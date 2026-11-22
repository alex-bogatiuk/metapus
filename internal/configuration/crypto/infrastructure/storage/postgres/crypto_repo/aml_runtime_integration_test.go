//go:build integration

package crypto_repo

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto/aml"
	amlruntime "metapus/internal/configuration/crypto/infrastructure/aml/runtime"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/secretbox"

	"github.com/stretchr/testify/require"
)

type amlRoundTripFunc func(*http.Request) (*http.Response, error)

func (f amlRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAMLRuntimePinsOldMappingAndPollsWithoutNewBudget(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, pool := amlTestDatabase(t)
	input, jobID := seedAMLJob(t, ctx, pool, 1)
	credentials := []byte(`{"keys":[{"name":"default","value":"synthetic-aml-token"}]}`)
	cipher, err := secretbox.Seal(credentials)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE cat_integration_accounts SET credentials_enc=$2,credentials_version=1 WHERE id=$1`, input.ProviderAccountID, cipher)
	require.NoError(t, err)
	submits, polls := 0, 0
	client := &http.Client{Transport: amlRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Nil(t, postgres.MustGetTxManager(req.Context()).GetTx(req.Context()), "paid and free HTTP must run after releasing SQL locks")
		require.Equal(t, "https://api.getblock.net/rpc/v1/request", req.URL.String())
		require.Equal(t, "Bearer synthetic-aml-token", req.Header.Get("Authorization"))
		var payload struct {
			ID     string         `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
		var result string
		if payload.Method == "checkup.checktx" {
			submits++
			require.Equal(t, "synthetic-provider-token", payload.Params["token_id"])
			result = `{"check":{"hash":"synthetic-hash"}}`
		} else {
			require.Equal(t, "checkup.getresult", payload.Method)
			polls++
			result = fmt.Sprintf(`{"check":{"hash":"synthetic-hash","currency":"TRX","address":%q,"transaction":%q,"token_id":"synthetic-provider-token","status":"SUCCESS","resultDate":%q,"report":{"riskscore":0.1,"signals":{"wallet":1,"sanctions":0}}}}`, input.Request.Address, input.Request.TxHash, time.Now().UTC().Format(time.RFC3339))
		}
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%q,"result":%s}`, payload.ID, result)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	repo := NewAMLRepository()
	service := cryptoapp.NewAMLJobService(repo, amlruntime.NewResolver(repo, client))
	claimed, err := service.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, 1, submits)
	// New requests use a new mapping version, while this ticket retains v1.
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_aml_provider_mappings(network_id,token_id,version,contract_version,contract_address,currency,provider_token_id,native,decimal_places,capability_accepted,acceptance_reference,currencylist_snapshot)
 VALUES($1,$2,'mapping-v2','synthetic-v2','test-contract','TRX','different-token',FALSE,6,TRUE,'synthetic fixture only','{"fixture":2}')`, input.Request.NetworkID, input.Request.TokenID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE sys_crypto_aml_account_policies SET allow_submissions=FALSE,version=version+1 WHERE account_id=$1`, input.ProviderAccountID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE reg_crypto_aml_jobs SET next_attempt_at=NOW()-interval '1 second' WHERE id=$1`, jobID)
	require.NoError(t, err)
	// Recreate the service to exercise restart and DB-only ticket recovery.
	service = cryptoapp.NewAMLJobService(repo, amlruntime.NewResolver(repo, client))
	claimed, err = service.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, 1, submits)
	require.Equal(t, 1, polls)
	var status, contract string
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM reg_crypto_aml_jobs WHERE id=$1`, jobID).Scan(&status))
	require.Equal(t, "succeeded", status)
	require.NoError(t, pool.QueryRow(ctx, `SELECT contract_version FROM reg_crypto_aml_reports WHERE subject_id=(SELECT subject_id FROM reg_crypto_aml_jobs WHERE id=$1)`, jobID).Scan(&contract))
	require.Equal(t, "synthetic-v1", contract)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_aml_paid_admissions WHERE account_id=$1`, input.ProviderAccountID).Scan(&count))
	require.Equal(t, 1, count)
	_, err = pool.Exec(ctx, `UPDATE sys_crypto_aml_provider_mappings SET provider_token_id='changed' WHERE network_id=$1`, input.Request.NetworkID)
	require.ErrorContains(t, err, "append-only")
	_, err = pool.Exec(ctx, `DELETE FROM reg_crypto_aml_paid_admissions WHERE job_id=$1`, jobID)
	require.ErrorContains(t, err, "append-only")
}

func TestAMLRuntimeLostSubmitACKNeverRepeatsPaidHTTP(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, pool := amlTestDatabase(t)
	input, jobID := seedAMLJob(t, ctx, pool, 1)
	cipher, err := secretbox.Seal([]byte(`{"keys":[{"name":"default","value":"synthetic-token"}]}`))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE cat_integration_accounts SET credentials_enc=$2 WHERE id=$1`, input.ProviderAccountID, cipher)
	require.NoError(t, err)
	calls := 0
	client := &http.Client{Transport: amlRoundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("synthetic lost ACK") })}
	repo := NewAMLRepository()
	service := cryptoapp.NewAMLJobService(repo, amlruntime.NewResolver(repo, client))
	claimed, err := service.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = service.RunOnce(ctx)
	require.NoError(t, err)
	require.False(t, claimed)
	require.Equal(t, 1, calls)
	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM reg_crypto_aml_jobs WHERE id=$1`, jobID).Scan(&status))
	require.Equal(t, string(aml.JobSubmissionUnknown), status)
	_, err = pool.Exec(ctx, `DELETE FROM reg_crypto_aml_package_credits WHERE account_id=$1`, input.ProviderAccountID)
	require.ErrorContains(t, err, "append-only")
}

func TestAMLRuntimeBudgetAdmissionIsSerializedAndNeverBorrowed(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	input, _ := seedAMLJob(t, ctx, pool, 1)
	repo := NewAMLRepository()
	second := input
	second.Request.TxHash += "-second"
	_, err := repo.EnqueueAMLCheck(ctx, second)
	require.NoError(t, err)
	jobs := make([]aml.Job, 2)
	for index := range jobs {
		var ok bool
		jobs[index], ok, err = repo.ClaimAMLJob(ctx, time.Now(), time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, job := range jobs {
		wg.Go(func() { results <- repo.AdmitAMLSubmit(ctx, job) })
	}
	wg.Wait()
	close(results)
	allowed := 0
	for err := range results {
		if err == nil {
			allowed++
		}
	}
	require.Equal(t, 1, allowed)
	require.Error(t, repo.AdmitAMLSubmit(ctx, jobs[0]))
	require.Error(t, repo.AdmitAMLSubmit(ctx, jobs[1]))
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_aml_paid_admissions`).Scan(&count))
	require.Equal(t, 1, count)
	// A separate account's balance does not unlock this one.
	other, _ := seedAMLJob(t, ctx, pool, 100)
	changedAccount := input
	changedAccount.ProviderAccountID = other.ProviderAccountID
	_, err = repo.EnqueueAMLCheck(ctx, changedAccount)
	require.ErrorContains(t, err, "already pinned")
	_, err = pool.Exec(ctx, `UPDATE sys_crypto_aml_account_policies SET allow_submissions=FALSE,version=version+1 WHERE account_id=$1`, other.ProviderAccountID)
	require.NoError(t, err)
	_, err = repo.EnqueueAMLCheck(ctx, EnqueueAMLInput{WalletID: input.WalletID, Provider: input.Provider, ProviderAccountID: input.ProviderAccountID, MappingVersion: input.MappingVersion, Request: func() aml.CheckRequest { r := input.Request; r.TxHash += "-third"; return r }(), DeadlineAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	_, ok, err := repo.ClaimAMLJob(ctx, time.Now(), time.Minute)
	require.NoError(t, err)
	require.False(t, ok)
}
