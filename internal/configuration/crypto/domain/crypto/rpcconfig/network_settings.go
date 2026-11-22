package rpcconfig

import "metapus/pkg/sdk/apperror"

// RPCNetworkSettings keeps read limits and notification policy in one versioned
// administrative command. Zero numeric values select the documented defaults.
type RPCNetworkSettings struct {
	EVM    *EVMSettings           `json:"evm,omitempty"`
	TRON   *TRONSettings          `json:"tron,omitempty"`
	Alerts IngestionAlertSettings `json:"alerts"`
}

type IngestionAlertSettings struct {
	MaxLagSeconds       int64 `json:"maxLagSeconds"`
	FireAfterSeconds    int   `json:"fireAfterSeconds"`
	RecoveryLagSeconds  int64 `json:"recoveryLagSeconds"`
	RecoverAfterSeconds int   `json:"recoverAfterSeconds"`
}

func (s IngestionAlertSettings) Defaults() IngestionAlertSettings {
	if s.MaxLagSeconds == 0 {
		s.MaxLagSeconds = 60
	}
	if s.FireAfterSeconds == 0 {
		s.FireAfterSeconds = 60
	}
	if s.RecoveryLagSeconds == 0 {
		s.RecoveryLagSeconds = 30
	}
	if s.RecoverAfterSeconds == 0 {
		s.RecoverAfterSeconds = 120
	}
	return s
}

func (s IngestionAlertSettings) Validate() error {
	s = s.Defaults()
	if s.MaxLagSeconds < 10 || s.MaxLagSeconds > 3600 || s.RecoveryLagSeconds < 1 || s.RecoveryLagSeconds >= s.MaxLagSeconds {
		return apperror.NewValidation("Порог восстановления должен быть меньше допустимого отставания (10–3600 с)")
	}
	if s.FireAfterSeconds < 30 || s.FireAfterSeconds > 3600 || s.RecoverAfterSeconds < 30 || s.RecoverAfterSeconds > 3600 {
		return apperror.NewValidation("Длительность проблемы и восстановления должна быть от 30 до 3600 с")
	}
	return nil
}

func (s RPCNetworkSettings) Defaults(family string) RPCNetworkSettings {
	s.Alerts = s.Alerts.Defaults()
	switch family {
	case "evm":
		v := EVMSettings{}
		if s.EVM != nil {
			v = *s.EVM
		}
		v = v.Defaults()
		s.EVM, s.TRON = &v, nil
	case "tron":
		v := TRONSettings{}
		if s.TRON != nil {
			v = *s.TRON
		}
		v = v.Defaults()
		s.TRON, s.EVM = &v, nil
	}
	return s
}

func (s RPCNetworkSettings) Validate(family string) error {
	if err := s.Alerts.Validate(); err != nil {
		return err
	}
	var attempt, budget int
	switch family {
	case "evm":
		if s.TRON != nil {
			return apperror.NewValidation("Настройки TRON неприменимы к EVM")
		}
		e := s.Defaults(family).EVM
		if err := e.Validate(); err != nil {
			return apperror.NewValidation("Проверьте таймауты, лимиты запросов и памяти EVM")
		}
		if e.LogsTimeoutMS > e.ReadBudgetMS {
			return apperror.NewValidation("Таймаут поиска событий не должен превышать бюджет операции")
		}
		attempt, budget = e.AttemptTimeoutMS, e.ReadBudgetMS
	case "tron":
		if s.EVM != nil {
			return apperror.NewValidation("Настройки EVM неприменимы к TRON")
		}
		t := s.Defaults(family).TRON
		if err := t.Validate(); err != nil {
			return apperror.NewValidation("Проверьте таймауты и лимиты запросов TRON")
		}
		attempt, budget = t.AttemptTimeoutMS, t.ReadBudgetMS
	default:
		return apperror.NewValidation("Настройки RPC для этого семейства не поддерживаются")
	}
	if budget < 2*attempt {
		return apperror.NewValidation("Бюджет операции должен вмещать как минимум две попытки RPC")
	}
	return nil
}

func (s RPCNetworkSettings) Equal(other RPCNetworkSettings) bool {
	return s.Alerts == other.Alerts && equalRPCSettings(s.EVM, other.EVM) && equalRPCSettings(s.TRON, other.TRON)
}

func equalRPCSettings[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
