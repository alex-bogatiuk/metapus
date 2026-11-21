package crypto_repo

import (
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
)

func TestNetworkIncidentTextDistinguishesCatchupAndWholeRecovery(t *testing.T) {
	lag := int64(900)
	event := crypto.IngestionAlertTransition{Condition: crypto.IngestionCondition{Scope: "network/TRON-NILE", NetworkCode: "TRON-NILE", WatcherKey: "tron:solidified", Reason: "backlog", LagSeconds: &lag}}
	_, message := ingestionAlertText(event)
	require.Contains(t, message, "продолжается с отставанием на 900 с")
	event.Condition.Reason = "quorum_unavailable"
	_, message = ingestionAlertText(event)
	require.Contains(t, message, "двух независимых RPC")
	event.Recovered = true
	title, message := ingestionAlertText(event)
	require.Equal(t, "Обработка входящих оплат восстановлена", title)
	require.Contains(t, message, "Обязательные проверки")
}

func TestShadowQuorumNotificationIdentifiesOptionalStream(t *testing.T) {
	event := crypto.IngestionAlertTransition{Condition: crypto.IngestionCondition{NetworkCode: "TRON-NILE", WatcherKey: "tron:shadow", Reason: "quorum_unavailable"}}
	title, message := ingestionAlertText(event)
	require.Equal(t, "Проблема теневой проверки блоков", title)
	require.Contains(t, message, "двух независимых RPC")
	event.Recovered = true
	title, _ = ingestionAlertText(event)
	require.Equal(t, "Теневая проверка блоков восстановлена", title)
	event.Recovered = false
	event.Condition.WatcherKey = "native_trx"
	title, _ = ingestionAlertText(event)
	require.Equal(t, "Проблема обработки входящих оплат", title)
}

func TestShadowBacklogNotificationDistinguishesProgressFromStall(t *testing.T) {
	lag, age := int64(84), int64(4)
	event := crypto.IngestionAlertTransition{Condition: crypto.IngestionCondition{
		NetworkCode: "TRON-NILE", WatcherKey: "tron:shadow", Reason: "backlog", LagSeconds: &lag, ProgressAgeSeconds: &age,
	}}
	_, message := ingestionAlertText(event)
	require.Contains(t, message, "84 с")
	require.Contains(t, message, "Последний прогресс — 4 с назад")
	require.NotContains(t, message, "не продвигается")
	event.Condition.Reason = "checkpoint stalled"
	_, message = ingestionAlertText(event)
	require.Contains(t, message, "не продвигается более 60 секунд")
	require.NotContains(t, message, "Последний прогресс")
	event.Condition.Reason = "backlog"
	event.Condition.LagSeconds, event.Condition.ProgressAgeSeconds = nil, nil
	_, message = ingestionAlertText(event)
	require.Contains(t, message, "устойчиво отстаёт")
	event.Recovered = true
	_, message = ingestionAlertText(event)
	require.Contains(t, message, "снова проходит")
	require.NotContains(t, message, "отстаёт")
}

func TestShadowPauseNotificationDoesNotClaimRecovery(t *testing.T) {
	title, message := ingestionAlertText(crypto.IngestionAlertTransition{Resolution: "operator_paused", Condition: crypto.IngestionCondition{NetworkCode: "TRON-NILE", WatcherKey: "tron:shadow", Reason: "backlog"}})
	require.Contains(t, title, "приостановлена")
	require.Contains(t, message, "без подтверждения восстановления")
	require.NotContains(t, message, "снова проходит")
}
