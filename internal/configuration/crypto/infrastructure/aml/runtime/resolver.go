// Package runtime resolves pinned AML accounts without RPC failover/retries.
package runtime

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/internal/configuration/crypto/infrastructure/aml/emulator"
	"metapus/internal/configuration/crypto/infrastructure/aml/getblocknet"
)

type Store interface {
	LoadAMLRuntimeConfig(context.Context, aml.Job) (aml.RuntimeConfig, error)
	AdmitAMLSubmit(context.Context, aml.Job) error
}

type Resolver struct {
	store Store
	http  *http.Client
}

func NewResolver(store Store, client *http.Client) *Resolver {
	if store == nil {
		panic("AML runtime store is required")
	}
	return &Resolver{store: store, http: client}
}

func (r *Resolver) ResolveAMLProvider(ctx context.Context, job aml.Job) (aml.Provider, error) {
	if job.Provider != getblocknet.ProviderName && job.Provider != aml.TestnetProvider {
		return nil, fmt.Errorf("unsupported AML provider")
	}
	cfg, err := r.store.LoadAMLRuntimeConfig(ctx, job)
	if err != nil {
		return nil, err
	}
	if (cfg.Provider != "" && cfg.Provider != job.Provider) || cfg.AccountID != job.ProviderAccountID || cfg.NetworkID != job.Request.NetworkID || cfg.TokenID != job.Request.TokenID || cfg.MappingVersion != job.MappingVersion {
		return nil, fmt.Errorf("AML runtime binding mismatch")
	}
	contractMatches := cfg.ContractAddress == job.Request.ContractAddress
	if cfg.Currency == "ETH" {
		contractMatches = strings.EqualFold(cfg.ContractAddress, job.Request.ContractAddress)
	}
	if !contractMatches {
		return nil, fmt.Errorf("AML asset mapping does not match request contract")
	}
	if job.Provider == aml.TestnetProvider {
		return emulator.New(job, cfg)
	}
	if cfg.ContractVersion == aml.TestnetContract {
		return nil, fmt.Errorf("synthetic AML mapping cannot be used for live paid requests")
	}
	client, err := getblocknet.New(getblocknet.Config{AccountID: cfg.AccountID, BearerToken: cfg.BearerToken, ContractVersion: cfg.ContractVersion,
		Mappings:        []getblocknet.AssetMapping{{NetworkID: cfg.NetworkID, TokenID: cfg.TokenID, ContractAddress: cfg.ContractAddress, Currency: cfg.Currency, ProviderTokenID: cfg.ProviderTokenID, Version: cfg.MappingVersion, Native: cfg.Native, CapabilityAccepted: true}},
		PendingStatuses: cfg.PendingStatuses, FailureStatuses: cfg.FailureStatuses}, r.http)
	if err != nil {
		return nil, err
	}
	return &admittedProvider{Provider: client, store: r.store, job: job}, nil
}

type admittedProvider struct {
	aml.Provider
	store Store
	job   aml.Job
}

func (p *admittedProvider) Submit(ctx context.Context, request aml.CheckRequest, requestID string) (aml.Ticket, error) {
	if p.job.Status != aml.JobSubmitting || request != p.job.Request || requestID != p.job.SubmitRequestID {
		return aml.Ticket{}, fmt.Errorf("AML submit does not match leased job")
	}
	if err := p.store.AdmitAMLSubmit(ctx, p.job); err != nil {
		return aml.Ticket{}, err
	}
	return p.Provider.Submit(ctx, request, requestID)
}
