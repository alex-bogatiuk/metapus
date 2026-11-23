package cryptoapp

import (
	"context"
	"errors"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

type AMLCheckpointResolver func(id.ID) (crypto.EmptyCheckpointReader, error)

type AMLPreflightService struct {
	repository crypto.AMLPreflightRepository
	resolve    AMLCheckpointResolver
	worker     string
}

func NewAMLPreflightService(repository crypto.AMLPreflightRepository, resolve AMLCheckpointResolver, worker string) *AMLPreflightService {
	return &AMLPreflightService{repository: repository, resolve: resolve, worker: worker}
}

func (s *AMLPreflightService) RunOnce(ctx context.Context) (bool, error) {
	job, claimed, err := s.repository.ClaimAMLPreflight(ctx, s.worker, time.Minute)
	if err != nil || !claimed {
		return claimed, err
	}
	rpcCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	reader, err := s.resolve(job.NetworkID)
	if err == nil && reader == nil {
		err = apperror.NewValidation("network does not support independent empty checkpoints")
	}
	var proof crypto.EmptyCheckpoint
	if err == nil {
		proof, err = reader.EmptyCheckpoint(rpcCtx, job.Asset, job.Address)
	}
	if err != nil {
		return true, errors.Join(err, s.repository.RetryAMLPreflight(ctx, job, "checkpoint_unavailable"))
	}
	if err = s.repository.SaveAMLPreflight(ctx, job, proof); err != nil {
		return true, errors.Join(err, s.repository.RetryAMLPreflight(ctx, job, "checkpoint_rejected"))
	}
	return true, nil
}
