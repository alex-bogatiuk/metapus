// Package emulator provides stateless, reproducible testnet AML responses.
// It has no HTTP client, provider credentials or paid-admission dependency.
package emulator

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
)

type Provider struct {
	job    aml.Job
	cfg    aml.RuntimeConfig
	digest [32]byte
	now    func() time.Time
}

func New(job aml.Job, cfg aml.RuntimeConfig) (*Provider, error) {
	if id.IsNil(job.ID) || job.Generation <= 0 || cfg.Emulator == nil || cfg.Emulator.Validate(cfg.NetworkFamily, cfg.NetworkIdentifier) != nil || cfg.ContractVersion != aml.TestnetContract || job.Provider != aml.TestnetProvider || cfg.AccountID != job.ProviderAccountID || cfg.NetworkID != job.Request.NetworkID || cfg.TokenID != job.Request.TokenID || cfg.MappingVersion != job.MappingVersion || job.RequestDigest != job.Request.Digest(job.MappingVersion) || job.Request.Validate() != nil {
		return nil, fmt.Errorf("invalid pinned testnet AML context")
	}
	copyConfig := *cfg.Emulator
	cfg.Emulator = &copyConfig
	mac := hmac.New(sha256.New, []byte(cfg.Emulator.Seed))
	_, _ = fmt.Fprintf(mac, "aml-testnet.v1|%s|%s|%d|%s|%s", job.ProviderAccountID, job.ID, job.Generation, job.SubmitRequestID, job.RequestDigest)
	p := &Provider{job: job, cfg: cfg, now: time.Now}
	copy(p.digest[:], mac.Sum(nil))
	return p, nil
}

func (p *Provider) Name() string      { return aml.TestnetProvider }
func (p *Provider) AccountID() id.ID  { return p.cfg.AccountID }
func (p *Provider) checkHash() string { return "emulated-" + hex.EncodeToString(p.digest[:]) }

func (p *Provider) Submit(ctx context.Context, r aml.CheckRequest, requestID string) (aml.Ticket, error) {
	if err := ctx.Err(); err != nil {
		return aml.Ticket{}, err
	}
	if p.job.Status != aml.JobSubmitting || r != p.job.Request || requestID == "" || requestID != p.job.SubmitRequestID {
		return aml.Ticket{}, fmt.Errorf("emulated submit differs from leased job")
	}
	return aml.Ticket{Provider: p.Name(), ProviderAccountID: p.AccountID(), CheckHash: p.checkHash(), SubmitRequestID: requestID, RequestDigest: p.job.RequestDigest, MappingVersion: p.job.MappingVersion, Request: r, SubmittedAt: p.now().UTC()}, nil
}

func (p *Provider) GetResult(ctx context.Context, ticket aml.Ticket, requestID string) (aml.CheckResult, error) {
	if err := ctx.Err(); err != nil {
		return aml.CheckResult{}, err
	}
	if p.job.Status != aml.JobPolling || requestID == "" || requestID != p.job.PollRequestID || p.job.ValidateTicket(ticket) != nil || ticket.CheckHash != p.checkHash() {
		return aml.CheckResult{}, fmt.Errorf("emulated poll differs from pinned ticket")
	}
	delay := binary.BigEndian.Uint64(p.digest[16:24]) % uint64(p.cfg.Emulator.MaxDelaySeconds+1)
	completed := ticket.SubmittedAt.Add(time.Duration(delay) * time.Second)
	now := p.now().UTC()
	if now.Before(completed) {
		return aml.CheckResult{State: aml.CheckPending, ProviderStatus: "TESTNET_PENDING"}, nil
	}
	score := aml.ScoreBP(binary.BigEndian.Uint64(p.digest[:8]) % 10001)
	unknown := binary.BigEndian.Uint64(p.digest[8:16])%10000 < uint64(p.cfg.Emulator.UnknownRateBP)
	fraction := fmt.Sprintf("%d.%04d", int(score)/10000, int(score)%10000)
	coverage := !unknown && (ticket.Request.Kind == aml.AddressSubject || (ticket.Request.MatchingEffects == 1 && ticket.Request.EventKind != "internal_value"))
	// Retain an explicit synthetic marker around a provider-shaped response.
	raw, err := json.MarshalDeterministic(struct {
		Emulated   bool      `json:"emulated"`
		Provider   string    `json:"provider"`
		NetworkID  id.ID     `json:"networkId"`
		CheckHash  string    `json:"checkHash"`
		Status     string    `json:"status"`
		ResultDate time.Time `json:"result_date"`
		Coverage   bool      `json:"coverageAccepted"`
		Report     struct {
			RiskScore json.RawMessage            `json:"riskscore"`
			Signals   map[string]json.RawMessage `json:"signals"`
		} `json:"report"`
	}{Emulated: true, Provider: p.Name(), NetworkID: p.cfg.NetworkID, CheckHash: ticket.CheckHash, Status: "SUCCESS", ResultDate: completed, Coverage: coverage, Report: struct {
		RiskScore json.RawMessage            `json:"riskscore"`
		Signals   map[string]json.RawMessage `json:"signals"`
	}{json.RawMessage(fraction), map[string]json.RawMessage{"wallet": json.RawMessage(fraction), "sanctions": json.RawMessage("0")}}})
	if err != nil {
		return aml.CheckResult{}, err
	}
	// A deterministic UUIDv7 binds a report to its completion time and ticket.
	var reportID id.ID
	copy(reportID[:], p.digest[:16])
	millis := uint64(completed.UnixMilli())
	for i := 5; i >= 0; i-- {
		reportID[i] = byte(millis)
		millis >>= 8
	}
	reportID[6] = (reportID[6] & 0x0f) | 0x70
	reportID[8] = (reportID[8] & 0x3f) | 0x80
	return aml.CheckResult{State: aml.CheckSucceeded, ProviderStatus: "TESTNET_SUCCESS", Report: &aml.Report{ID: reportID, Provider: p.Name(), ProviderAccountID: p.AccountID(), CheckHash: ticket.CheckHash, RequestDigest: ticket.RequestDigest, MappingVersion: ticket.MappingVersion, ContractVersion: aml.TestnetContract, AdapterVersion: aml.TestnetAdapter, Score: &score, CoverageAccepted: coverage, Signals: []aml.Signal{{Name: "wallet", Value: fraction}, {Name: "sanctions", Value: "0"}}, CompletedAt: completed, ReceivedAt: now, RawResponse: raw}}, nil
}
