package crypto_repo

import (
	"context"
	"fmt"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/notifications"
	"metapus/internal/infrastructure/storage/postgres"
)

// DeliverIngestionAlert reuses ERP notifications. Event IDs are saved by the
// monitor before delivery; an advisory lock and event attribute deduplicate
// replay after a crash, including notifications read/soft-deleted by a user.
func DeliverIngestionAlert(ctx context.Context, event crypto.IngestionAlertTransition) error {
	manager, err := tenant.GetTxManager(ctx)
	if err != nil {
		return err
	}
	return manager.RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "ingestion-alert/"+event.ID); err != nil {
			return err
		}
		rows, err := q.Query(ctx, `SELECT u.id FROM auth_users u
		 WHERE u.is_admin=TRUE AND u.is_active=TRUE AND u.deletion_mark=FALSE
		 AND NOT EXISTS (SELECT 1 FROM sys_notifications n WHERE n.user_id=u.id AND n.attributes->>'ingestionEventId'=$1)
		 ORDER BY u.id`, event.ID)
		if err != nil {
			return err
		}
		var users []id.ID
		for rows.Next() {
			var user id.ID
			if err := rows.Scan(&user); err != nil {
				rows.Close()
				return err
			}
			users = append(users, user)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		// A successfully delivered replay has no new recipients. Distinguish this
		// from a configuration without any enabled system administrator.
		if len(users) == 0 {
			var admins int
			if err := q.QueryRow(ctx, `SELECT count(*) FROM auth_users WHERE is_admin=TRUE AND is_active=TRUE AND deletion_mark=FALSE`).Scan(&admins); err != nil {
				return err
			}
			if admins == 0 {
				return fmt.Errorf("ingestion alert has no active system administrator")
			}
			return nil
		}
		title, message := ingestionAlertText(event)
		severity := notifications.Severity(event.Condition.Severity)
		if event.Recovered {
			severity = notifications.SeveritySuccess
		}
		if event.Resolution == "operator_paused" {
			severity = notifications.SeverityInfo
		}
		items := make([]*notifications.Notification, 0, len(users))
		for _, user := range users {
			attributes := map[string]any{"source": "crypto_ingestion", "ingestionEventId": event.ID, "scope": event.Condition.Scope, "networkCode": event.Condition.NetworkCode, "watcherKey": event.Condition.WatcherKey, "reason": event.Condition.Reason, "recovered": event.Recovered, "observedAt": event.At}
			if event.Condition.LagSeconds != nil {
				attributes["lagSeconds"] = *event.Condition.LagSeconds
			}
			if event.Condition.ProgressAgeSeconds != nil {
				attributes["progressAgeSeconds"] = *event.Condition.ProgressAgeSeconds
			}
			if event.Resolution != "" {
				attributes["resolution"] = event.Resolution
			}
			items = append(items, &notifications.Notification{UserID: user, Title: title, Message: message, Severity: severity,
				Attributes: attributes})
		}
		return postgres.NewNotificationRepo().CreateBatch(ctx, items)
	})
}

func ingestionAlertText(event crypto.IngestionAlertTransition) (string, string) {
	name := event.Condition.NetworkCode
	if name == "" {
		name = "криптопроцессинга"
	}
	if event.Condition.WatcherKey == "tron:shadow" {
		if event.Resolution == "operator_paused" {
			return "Теневая проверка блоков приостановлена", "Теневая проверка блоков " + name + " приостановлена администратором. Инцидент закрыт без подтверждения восстановления."
		}
		if event.Recovered {
			return "Теневая проверка блоков восстановлена", "Теневая проверка блоков " + name + " снова проходит."
		}
		reason := "Проверка выявила устойчивую проблему."
		switch event.Condition.Reason {
		case "quorum_unavailable":
			reason = "Не удалось получить согласованные данные от двух независимых RPC-провайдеров."
		case "provider_disagreement":
			reason = "Независимые RPC-провайдеры вернули разные данные блоков."
		case "backlog":
			reason = "Теневая проверка устойчиво отстаёт от сети."
			if event.Condition.LagSeconds != nil && *event.Condition.LagSeconds >= 0 {
				reason = fmt.Sprintf("Теневая проверка отстаёт от сети на %d с.", *event.Condition.LagSeconds)
			}
			if event.Condition.ProgressAgeSeconds != nil && *event.Condition.ProgressAgeSeconds >= 0 {
				reason += fmt.Sprintf(" Последний прогресс — %d с назад.", *event.Condition.ProgressAgeSeconds)
			}
		case "checkpoint stalled":
			reason = "Теневая проверка не продвигается более 60 секунд при наличии непрочитанных блоков."
		case "incomplete_receipts":
			reason = "RPC вернул неполные данные транзакций блока."
		}
		return "Проблема теневой проверки блоков", "Теневая проверка блоков " + name + ": " + reason + " Состояние рабочего приёма оплат проверяется отдельно."
	}
	if event.Recovered {
		if event.Condition.Scope == "network/"+event.Condition.NetworkCode {
			return "Обработка входящих оплат восстановлена", "Обязательные проверки обработки входящих оплат " + name + " снова в пределах допустимой задержки."
		}
		switch event.Condition.Reason {
		case "rpc_coverage", "stale head", "provider head behind checkpoint":
			return "Получение данных блоков восстановлено", "Актуальные данные блоков " + name + " снова доступны."
		case "needs_review", "inbox_stalled":
			return "Проблема очереди входящих событий устранена", "Проверка очереди входящих событий " + name + " снова проходит."
		case "read_failure":
			return "Проверки обработки оплат возобновлены", "Мониторинг снова может проверять обработку входящих оплат."
		default:
			return "Обработка блоков восстановлена", "Обработчик блоков " + name + " снова проходит проверку."
		}
	}
	message := "Проверки обработки оплат " + name + " выявили устойчивую проблему."
	switch event.Condition.Reason {
	case "rpc_coverage", "ingestion RPC coverage unavailable":
		message = "Не удаётся получить актуальные данные блоков " + name + ". Обнаружение новых оплат может задерживаться."
	case "quorum_unavailable":
		message = "Не удаётся получить согласованные данные блоков " + name + " от двух независимых RPC-провайдеров. Подтверждение новых оплат может задерживаться."
	case "backlog":
		message = "Обработка блоков " + name + " отстаёт от сети. Новые оплаты могут отображаться с задержкой."
		if event.Condition.LagSeconds != nil && *event.Condition.LagSeconds >= 0 {
			message = fmt.Sprintf("Обработка блоков %s продолжается с отставанием на %d с. Новые оплаты могут отображаться с задержкой.", name, *event.Condition.LagSeconds)
		}
	case "checkpoint stalled":
		message = "Обработка блоков " + name + " остановилась при наличии непрочитанной истории."
	case "watcher lease inactive", "missing checkpoint":
		message = "Обработчик входящих оплат " + name + " не активен."
	case "stale head", "provider head behind checkpoint":
		message = "RPC возвращает устаревшие данные блоков " + name + "."
	case "needs_review":
		message = "Для " + name + " есть входящие события, требующие проверки."
	case "inbox_stalled":
		message = "Входящие события " + name + " ожидают обработки дольше допустимого времени."
	case "read_failure":
		message = "Мониторинг не может проверить обработку входящих оплат. Требуется проверить доступность БД и конфигурацию."
	}
	return "Проблема обработки входящих оплат", message
}
