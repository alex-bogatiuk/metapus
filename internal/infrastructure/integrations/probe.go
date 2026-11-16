package integrations

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/integrations/profiles"
)

// ConnectionProber checks that a pool answers through its real binding.
// Providers whose profile has a Probe are checked directly. For the others
// the configuration supplies the request (EndpointProbe, which also rejects
// providers it does not check) and, for a target bound to an endpoint, the
// transport (EndpointTransport).
type ConnectionProber struct {
	Accounts          model.RuntimeRepository
	Profiles          *profiles.Registry
	Base              http.RoundTripper
	EndpointProbe     func(model.ConnectionTarget) (profiles.Probe, error)
	EndpointTransport func(id.ID, string) func(http.RoundTripper) http.RoundTripper
}

func (p ConnectionProber) Probe(ctx context.Context, target model.ConnectionTarget) (bool, error) {
	profile, ok := p.Profiles.Lookup(target.Provider)
	if !ok {
		return false, apperror.NewValidation("Проверка этого типа выполняется его подсистемой")
	}
	var probe profiles.Probe
	switch {
	case profile.Probe != nil:
		probe = *profile.Probe
	case p.EndpointProbe != nil:
		var err error
		if probe, err = p.EndpointProbe(target); err != nil {
			return false, err
		}
	default:
		return false, apperror.NewValidation("Проверка этого типа выполняется его подсистемой")
	}
	base := p.Base
	if base == nil {
		base = http.DefaultTransport
	}
	var transport http.RoundTripper
	if !id.IsNil(target.EndpointID) {
		if p.EndpointTransport == nil {
			return false, apperror.NewInternal(errors.New("RPC endpoint transport factory is not configured"))
		}
		transport = p.EndpointTransport(target.EndpointID, target.URL)(base)
	} else {
		transport = &Transport{Base: base, Repository: p.Accounts, PoolID: target.PoolID, Provider: target.Provider, Profiles: p.Profiles}
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, probe.Method, probe.URL, strings.NewReader(probe.Body))
	if err != nil {
		return false, apperror.NewValidation("Некорректные параметры проверки подключения")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return false, nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	return err == nil && len(body) <= 1<<20 && resp.StatusCode == http.StatusOK && probe.Valid(body), nil
}
