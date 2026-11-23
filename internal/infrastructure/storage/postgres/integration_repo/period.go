package integration_repo

import (
	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	"time"
)

func quotaPeriod(definition integrations.ProviderDefinition, a *integrations.Account, now time.Time) (time.Time, time.Time, error) {
	loc, err := time.LoadLocation(a.ResetTimezone)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return a.Period(definition, now, loc)
}
