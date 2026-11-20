package evm

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/blockchain/rpcpolicy"
)

var (
	ErrAllRPCProvidersUnavailable = errors.New("all EVM RPC providers unavailable")
	ErrWrongChainID               = errors.New("EVM RPC chain ID mismatch")
	ErrRPCProviderDivergence      = errors.New("EVM RPC providers disagree")
)

type rpcEndpointState string

const (
	rpcStateClosed     rpcEndpointState = "closed"
	rpcStateOpen       rpcEndpointState = "open"
	rpcStateWrongChain rpcEndpointState = "wrong_chain"
)

type RPCPoolConfig struct {
	allowWrongChainEndpoints bool // Hot reload isolates an invalid new endpoint; it never contributes a vote.
	Reload                   func(context.Context) (RPCPoolConfig, string, error)
	InitialVersion           string
	NetworkCode              string
	ExpectedChainID          *big.Int
	Endpoints                []chainconfig.Endpoint
	FailureThreshold         int
	OpenDuration             time.Duration
	HTTPClient               *http.Client
	Settings                 chainconfig.EVMSettings
}

type rpcEndpoint struct {
	endpointID   id.ID
	providerID   string
	trustGroup   string
	quotaLimited bool
	priority     int
	client       *ethclient.Client
	raw          *rpc.Client
	httpClient   *http.Client

	mu                 sync.Mutex
	state              rpcEndpointState
	failures           int
	openUntil          time.Time
	lastError          string
	verified           bool
	operations         map[rpcOperation]*operationCircuit
	unsupportedMethods map[string]bool
	admission          *providerAdmission
	logBlockRange      uint64 // Learned from an explicit provider range-limit response.
	blockLaneWidth     int    // Catch-up workers this provider sustained in its last window.
}

type rpcGeneration struct {
	networkCode      string
	expectedChainID  *big.Int
	failureThreshold int
	openDuration     time.Duration
	endpoints        []*rpcEndpoint
	settings         chainconfig.EVMSettings
	readMu           sync.Mutex
	preferred        map[rpcOperation]*rpcEndpoint
	probeNext        map[rpcOperation]uint64
	closeOnce        sync.Once
}

func newRPCGeneration(ctx context.Context, cfg RPCPoolConfig) (*rpcGeneration, error) {
	if strings.TrimSpace(cfg.NetworkCode) == "" || cfg.ExpectedChainID == nil || cfg.ExpectedChainID.Sign() <= 0 {
		return nil, errors.New("EVM RPC pool requires network code and positive chain ID")
	}
	if len(cfg.Endpoints) == 0 || cfg.FailureThreshold <= 0 || cfg.OpenDuration <= 0 {
		return nil, errors.New("EVM RPC pool requires endpoints and positive circuit settings")
	}
	cfg.Settings = cfg.Settings.Defaults()
	if err := cfg.Settings.Validate(); err != nil {
		return nil, err
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	pool := &rpcGeneration{
		networkCode: cfg.NetworkCode, expectedChainID: new(big.Int).Set(cfg.ExpectedChainID),
		failureThreshold: cfg.FailureThreshold, openDuration: cfg.OpenDuration,
		endpoints: make([]*rpcEndpoint, 0, len(cfg.Endpoints)),
		settings:  cfg.Settings, preferred: make(map[rpcOperation]*rpcEndpoint),
	}
	for _, endpoint := range cfg.Endpoints {
		isolatedClient := *httpClient
		endpointClient := &isolatedClient
		isolatedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		transport := httpClient.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		if native, ok := transport.(*http.Transport); ok {
			isolatedClient.Transport = native.Clone()
		}
		if endpoint.DisableKeepAlives {
			transport := httpClient.Transport
			if transport == nil {
				transport = http.DefaultTransport
			}
			baseTransport, ok := transport.(*http.Transport)
			if !ok {
				return nil, fmt.Errorf("EVM RPC provider %s cannot disable keep-alives with a custom HTTP transport", endpoint.ProviderID)
			}
			isolatedTransport := baseTransport.Clone()
			isolatedTransport.DisableKeepAlives = true
			isolatedClient := *httpClient
			isolatedClient.Transport = isolatedTransport
			endpointClient = &isolatedClient
		}
		base := endpointClient.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		if endpoint.WrapTransport != nil {
			base = endpoint.WrapTransport(base)
		}
		endpointClient.Transport = boundedRPCTransport{base: base, limit: cfg.Settings.MaxResponseBytes, network: cfg.NetworkCode, provider: endpoint.ProviderID}
		admission, err := acquireProviderAdmission(endpoint, cfg.Settings)
		if err != nil {
			pool.Close()
			return nil, err
		}
		raw, err := rpc.DialOptions(ctx, endpoint.URL, rpc.WithHTTPClient(endpointClient))
		if err != nil {
			releaseProviderAdmission(admission)
			pool.Close()
			return nil, fmt.Errorf("dial EVM RPC provider %s failed", endpoint.ProviderID)
		}
		pool.endpoints = append(pool.endpoints, &rpcEndpoint{
			endpointID: endpoint.EndpointID,
			providerID: endpoint.ProviderID, trustGroup: endpoint.VotingGroup(), priority: endpoint.Priority, quotaLimited: endpoint.MinRequestIntervalMS > 0,
			raw: raw, httpClient: endpointClient, client: ethclient.NewClient(raw), state: rpcStateClosed,
			operations: make(map[rpcOperation]*operationCircuit), unsupportedMethods: make(map[string]bool), admission: admission,
		})
	}
	sort.SliceStable(pool.endpoints, func(i, j int) bool { return pool.endpoints[i].priority < pool.endpoints[j].priority })
	pool.Probe(ctx)
	if pool.hasWrongChainEndpoint() && !cfg.allowWrongChainEndpoints {
		pool.Close()
		return nil, fmt.Errorf("%w for network %s", ErrWrongChainID, cfg.NetworkCode)
	}
	if !pool.hasUsableEndpoint() {
		pool.Close()
		return nil, fmt.Errorf("%w for network %s", ErrAllRPCProvidersUnavailable, cfg.NetworkCode)
	}
	return pool, nil
}

func (p *rpcGeneration) hasWrongChainEndpoint() bool {
	for _, endpoint := range p.endpoints {
		endpoint.mu.Lock()
		wrongChain := endpoint.state == rpcStateWrongChain
		endpoint.mu.Unlock()
		if wrongChain {
			return true
		}
	}
	return false
}

func (p *rpcGeneration) Close() {
	if p == nil {
		return
	}
	p.closeOnce.Do(func() {
		for _, endpoint := range p.endpoints {
			endpoint.raw.Close()
			if endpoint.httpClient != nil {
				endpoint.httpClient.CloseIdleConnections()
			}
			releaseProviderAdmission(endpoint.admission)
		}
	})
}

func (p *rpcGeneration) hasUsableEndpoint() bool {
	for _, endpoint := range p.endpoints {
		endpoint.mu.Lock()
		usable := endpoint.verified && endpoint.state != rpcStateWrongChain
		endpoint.mu.Unlock()
		if usable {
			return true
		}
	}
	return false
}

func (p *rpcGeneration) Probe(ctx context.Context) {
	for _, endpoint := range p.endpoints {
		probeCtx, cancel := context.WithTimeout(ctx, p.settings.AttemptTimeout())
		sequenceFits := endpoint.admission.CanCompleteSequence(probeCtx, 2)
		leave, err := endpoint.admission.Enter(probeCtx, false)
		if err != nil {
			cancel()
			continue
		}
		chainID, err := endpoint.client.ChainID(probeCtx)
		leave()
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				p.failure(endpoint, err)
			}
			continue
		}
		if chainID.Cmp(p.expectedChainID) != 0 {
			endpoint.mu.Lock()
			endpoint.state = rpcStateWrongChain
			endpoint.lastError = "chain ID mismatch"
			endpoint.mu.Unlock()
			continue
		}
		endpoint.mu.Lock()
		endpoint.verified = true
		endpoint.mu.Unlock()
		if !sequenceFits {
			// A low-quota route may prove identity without having capacity for
			// a second request. Leave head/full-block circuits unobserved here;
			// real reads still have to establish their own fresh evidence.
			p.success(endpoint)
			continue
		}
		if !p.beginRead(endpoint, opHeader, time.Now()) {
			continue
		}
		if _, err := endpointRead(ctx, p, endpoint, opHeader, func(attempt context.Context, client *ethclient.Client) (*types.Header, error) {
			return p.readHeader(attempt, client, nil)
		}); err != nil {
			if ctx.Err() == nil {
				p.failure(endpoint, err)
			}
			continue
		}
		p.success(endpoint)
	}
}

func (p *rpcGeneration) Status() []crypto.RuntimeEndpointStatus {
	result := make([]crypto.RuntimeEndpointStatus, 0, len(p.endpoints))
	for _, endpoint := range p.endpoints {
		endpoint.mu.Lock()
		status := crypto.RuntimeEndpointStatus{
			EndpointID: endpoint.endpointID,
			ProviderID: endpoint.providerID, Role: string(chainconfig.RoleRPC), State: string(endpoint.state),
			Failures: endpoint.failures, LastError: rpcpolicy.SafeReason(endpoint.lastError),
		}
		for _, op := range []rpcOperation{opHeader, opBlock, opLogs, opReceipt, opState} {
			if c := endpoint.operations[op]; c != nil {
				rpcpolicy.AddDiagnostic(&status, c.Diagnostic(string(op)))
			}
		}
		result = append(result, status)
		endpoint.mu.Unlock()
	}
	return result
}

func (p *rpcGeneration) available(endpoint *rpcEndpoint, now time.Time) bool {
	// A chain-ID mismatch is a terminal configuration failure for the whole
	// logical network. Continuing through another endpoint could hide a mixed
	// provider configuration and allow outbound execution under stale
	// readiness, so every data-plane operation fails closed until restart with
	// corrected topology.
	if p.hasWrongChainEndpoint() {
		return false
	}
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	if endpoint.state == rpcStateWrongChain {
		return false
	}
	if endpoint.state == rpcStateOpen && now.Before(endpoint.openUntil) {
		return false
	}
	return true
}

func (p *rpcGeneration) success(endpoint *rpcEndpoint) {
	endpoint.mu.Lock()
	if endpoint.state == rpcStateWrongChain {
		endpoint.mu.Unlock()
		return
	}
	endpoint.state, endpoint.failures, endpoint.lastError = rpcStateClosed, 0, ""
	endpoint.openUntil = time.Time{}
	endpoint.mu.Unlock()
}

func (p *rpcGeneration) failure(endpoint *rpcEndpoint, err error) {
	if errors.Is(err, ErrRPCAdmissionBusy) || errors.Is(err, context.Canceled) {
		return // Local capacity/caller cancellation does not prove an upstream failure.
	}
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	if endpoint.state == rpcStateWrongChain {
		return
	}
	endpoint.failures++
	endpoint.lastError = sanitizedRPCError(err)
	if endpoint.failures >= p.failureThreshold {
		endpoint.state = rpcStateOpen
		endpoint.openUntil = time.Now().UTC().Add(p.openDuration)
	}
}

func sanitizedRPCError(err error) string {
	if err == nil {
		return ""
	}
	return rpcErrorCategory(err)
}

func (p *rpcGeneration) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	return rpcRead(ctx, p, opHeader, "eth_getBlockByNumber", func(attempt context.Context, client *ethclient.Client) (*types.Header, error) {
		return p.readHeader(attempt, client, number)
	})
}

func (p *rpcGeneration) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	return rpcRead(ctx, p, opBlock, blockMethod, func(attempt context.Context, client *ethclient.Client) (*types.Block, error) {
		return p.fetchBlock(attempt, client, number)
	})
}

const blockMethod = "eth_getBlockByNumber"

// fetchBlock rejects a body that does not match its own header roots, so a
// provider failure is attributed to the endpoint that served it.
func (p *rpcGeneration) fetchBlock(ctx context.Context, client *ethclient.Client, number *big.Int) (*types.Block, error) {
	block, err := client.BlockByNumber(ctx, number)
	if err != nil {
		return nil, err
	}
	height := block.Number().Int64()
	if number != nil && number.Sign() >= 0 {
		height = number.Int64()
	}
	validator := Watcher{network: crypto.NetworkRef{NetworkIdentifier: p.expectedChainID.String()}}
	if err := validator.validateNativeBlock(block, height); err != nil {
		return nil, err
	}
	return block, nil
}

func (p *rpcGeneration) TransactionReceipt(ctx context.Context, hash [32]byte) (*types.Receipt, error) {
	return rpcRead(ctx, p, opReceipt, "eth_getTransactionReceipt", func(attempt context.Context, client *ethclient.Client) (*types.Receipt, error) {
		return client.TransactionReceipt(attempt, hash)
	})
}
func (p *rpcGeneration) TransactionByHash(ctx context.Context, hash [32]byte) (*types.Transaction, bool, error) {
	type result struct {
		tx      *types.Transaction
		pending bool
	}
	value, err := rpcRead(ctx, p, opReceipt, "eth_getTransactionByHash", func(attempt context.Context, client *ethclient.Client) (result, error) {
		tx, pending, err := client.TransactionByHash(attempt, hash)
		return result{tx, pending}, err
	})
	return value.tx, value.pending, err
}

func (p *rpcGeneration) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	return rpcReadEndpoint(ctx, p, opLogs, "eth_getLogs", func(attempt context.Context, endpoint *rpcEndpoint) ([]types.Log, error) {
		return filterEndpointLogs(attempt, endpoint, query)
	})
}

func (p *rpcGeneration) BalanceAt(ctx context.Context, address [20]byte) (*big.Int, error) {
	return rpcRead(ctx, p, opState, "eth_getBalance", func(attempt context.Context, client *ethclient.Client) (*big.Int, error) {
		return client.BalanceAt(attempt, address, nil)
	})
}

func (p *rpcGeneration) PendingNonceAt(ctx context.Context, address [20]byte) (uint64, error) {
	return rpcRead(ctx, p, opState, "eth_getTransactionCount", func(attempt context.Context, client *ethclient.Client) (uint64, error) {
		return client.PendingNonceAt(attempt, address)
	})
}

func (p *rpcGeneration) NonceAt(ctx context.Context, address [20]byte) (uint64, error) {
	return rpcRead(ctx, p, opState, "eth_getTransactionCount", func(attempt context.Context, client *ethclient.Client) (uint64, error) {
		return client.NonceAt(attempt, address, nil)
	})
}

func (p *rpcGeneration) ObserveNonceState(ctx context.Context, sourceAddress string) (ProviderNonceSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, p.settings.ReadBudget())
	defer cancel()
	normalized, err := NormalizeAddress(sourceAddress)
	if err != nil {
		return ProviderNonceSnapshot{}, err
	}
	required := min(p.independentProviders(), 2)
	latestValues, pendingValues := make(map[uint64]struct{}), make(map[uint64]struct{})
	observations := 0
	observedGroups := map[string]bool{}
	for endpoint := range votingEndpoints(p.endpoints) {
		if !p.beginRead(endpoint, opState, time.Now()) {
			continue
		}
		type values struct{ latest, pending uint64 }
		observation, readErr := endpointRead(ctx, p, endpoint, opState, func(attempt context.Context, client *ethclient.Client) (values, error) {
			// A provider with a stale head reports the nonce of an old block.
			// It is unavailable evidence, never a disagreement: counting it
			// would hold healthy sources permanently on a lagging RPC.
			if _, err := p.readHeader(attempt, client, nil); err != nil {
				return values{}, err
			}
			latest, err := client.NonceAt(attempt, common.HexToAddress(normalized), nil)
			if err != nil {
				return values{}, err
			}
			pending, err := client.PendingNonceAt(attempt, common.HexToAddress(normalized))
			return values{latest, pending}, err
		})
		if readErr != nil {
			continue
		}
		latest, pending := observation.latest, observation.pending

		latestValues[latest], pendingValues[pending] = struct{}{}, struct{}{}
		if !observedGroups[endpoint.trustGroup] {
			observedGroups[endpoint.trustGroup] = true
			observations++
		}
	}
	if required == 0 || observations < required {
		return ProviderNonceSnapshot{}, fmt.Errorf("only %d/%d required providers returned nonce state", observations, required)
	}
	result := ProviderNonceSnapshot{Observations: observations, Divergent: len(latestValues) != 1 || len(pendingValues) != 1}
	if result.Divergent {
		recordRPCDivergence(ctx, p.networkCode, "nonce_state")
	}
	for value := range latestValues {
		result.Latest = value
	}
	for value := range pendingValues {
		result.Pending = value
	}
	return result, nil
}

// CanonicalHeaderByNumber obtains at least two independent observations when
// configured. Bounded parallel reads keep a stalled provider from consuming
// the budget of the healthy providers. A disagreement is returned as an error: callers must
// retry rather than classify a financially relevant receipt as orphaned from a
// single stale RPC response. A one-provider Sepolia setup remains supported.
func (p *rpcGeneration) CanonicalHeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	ctx, cancel := context.WithTimeout(ctx, p.settings.ReadBudget())
	defer cancel()
	if number == nil || number.Sign() < 0 {
		return nil, errors.New("canonical EVM header requires a non-negative block number")
	}
	primary, fallback := []*rpcEndpoint{}, []*rpcEndpoint{}
	for endpoint := range votingEndpoints(p.endpoints) {
		if endpoint.quotaLimited {
			fallback = append(fallback, endpoint)
		} else {
			primary = append(primary, endpoint)
		}
	}
	required := min(p.independentProviders(), 2)
	observations := 0
	observedGroups := map[string]bool{}
	var canonical *types.Header
	// Preserve low-quota fallback capacity for reads that actually need it.
	// Every successful observation within each group must still agree.
	for _, group := range [][]*rpcEndpoint{primary, fallback} {
		if required > 0 && observations >= required {
			break
		}
		for _, observation := range p.canonicalHeaders(ctx, number, group) {
			header := observation.header
			if !observedGroups[observation.group] {
				observedGroups[observation.group] = true
				observations++
			}
			if canonical == nil {
				canonical = header
			} else if canonical.Hash() != header.Hash() {
				recordRPCDivergence(ctx, p.networkCode, "canonical_block")
				return nil, fmt.Errorf("%w on block %s", ErrRPCProviderDivergence, number.String())
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if required == 0 || observations < required {
		return nil, fmt.Errorf("only %d/%d required providers returned canonical block %s", observations, required, number.String())
	}
	return canonical, nil
}

func (p *rpcGeneration) canonicalHeaders(ctx context.Context, number *big.Int, endpoints []*rpcEndpoint) []headerObservation {
	jobs := make(chan *rpcEndpoint, len(endpoints))
	headers := make(chan headerObservation, len(endpoints))
	for _, endpoint := range endpoints {
		jobs <- endpoint
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(4, len(endpoints)) {
		workers.Go(func() {
			for endpoint := range jobs {
				if ctx.Err() != nil || !p.beginRead(endpoint, opHeader, time.Now()) {
					continue
				}
				header, err := endpointRead(ctx, p, endpoint, opHeader, func(attempt context.Context, client *ethclient.Client) (*types.Header, error) {
					return p.readHeader(attempt, client, number)
				})
				if err == nil {
					headers <- headerObservation{header: header, group: endpoint.trustGroup}
				}
			}
		})
	}
	workers.Wait()
	close(headers)
	result := make([]headerObservation, 0, len(endpoints))
	for header := range headers {
		result = append(result, header)
	}
	return result
}

func (p *rpcGeneration) EstimateGas(ctx context.Context, message ethereum.CallMsg) (uint64, error) {
	return rpcRead(ctx, p, opState, "eth_estimateGas", func(attempt context.Context, client *ethclient.Client) (uint64, error) {
		return client.EstimateGas(attempt, message)
	})
}

func (p *rpcGeneration) FeeHistory(ctx context.Context, blockCount uint64, rewardPercentiles []float64) (*ethereum.FeeHistory, error) {
	return rpcRead(ctx, p, opState, "eth_feeHistory", func(attempt context.Context, client *ethclient.Client) (*ethereum.FeeHistory, error) {
		return client.FeeHistory(attempt, blockCount, nil, rewardPercentiles)
	})
}

func (p *rpcGeneration) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return rpcRead(ctx, p, opState, "eth_maxPriorityFeePerGas", func(attempt context.Context, client *ethclient.Client) (*big.Int, error) {
		return client.SuggestGasTipCap(attempt)
	})
}

func (p *rpcGeneration) CallContract(ctx context.Context, message ethereum.CallMsg) ([]byte, error) {
	return rpcRead(ctx, p, opState, "eth_call", func(attempt context.Context, client *ethclient.Client) ([]byte, error) {
		return client.CallContract(attempt, message, nil)
	})
}

func (p *rpcGeneration) CodeAt(ctx context.Context, address [20]byte) ([]byte, error) {
	return rpcRead(ctx, p, opState, "eth_getCode", func(attempt context.Context, client *ethclient.Client) ([]byte, error) {
		return client.CodeAt(attempt, address, nil)
	})
}

func (p *rpcGeneration) SendTransaction(ctx context.Context, tx *types.Transaction) (string, error) {
	var joined []error
	for _, endpoint := range p.endpoints {
		if !p.available(endpoint, time.Now().UTC()) {
			continue
		}
		err := endpoint.client.SendTransaction(ctx, tx)
		if err == nil || strings.Contains(strings.ToLower(err.Error()), "already known") || strings.Contains(strings.ToLower(err.Error()), "known transaction") {
			p.success(endpoint)
			return endpoint.providerID, err
		}
		p.failure(endpoint, err)
		joined = append(joined, fmt.Errorf("provider %s: %w", endpoint.providerID, err))
	}
	return "", errors.Join(ErrAllRPCProvidersUnavailable, errors.Join(joined...))
}

func (p *rpcGeneration) assessReplacement(ctx context.Context, source [20]byte, nonce uint64, hashes [][32]byte) crypto.ReplacementAssessment {
	required := min(p.independentProviders(), 2)
	if required == 0 {
		return crypto.ReplacementAssessment{Disposition: crypto.ReplacementUncertain, Reason: "no chain-verified RPC providers"}
	}
	observations := 0
	observedGroups := map[string]bool{}
	pendingNonces := make(map[uint64]struct{})
	mined := make(map[string]struct{})
	for endpoint := range votingEndpoints(p.endpoints) {
		if !p.available(endpoint, time.Now().UTC()) {
			continue
		}
		valid := true
		for _, hash := range hashes {
			receipt, err := endpoint.client.TransactionReceipt(ctx, hash)
			switch {
			case err == nil:
				mined[receipt.TxHash.Hex()] = struct{}{}
			case errors.Is(err, ethereum.NotFound):
			default:
				valid = false
				p.failure(endpoint, err)
			}
			if !valid {
				break
			}
		}
		if !valid {
			continue
		}
		pending, err := endpoint.client.PendingNonceAt(ctx, source)
		if err != nil {
			p.failure(endpoint, err)
			continue
		}
		p.success(endpoint)
		pendingNonces[pending] = struct{}{}
		if !observedGroups[endpoint.trustGroup] {
			observedGroups[endpoint.trustGroup] = true
			observations++
		}
	}
	result := crypto.ReplacementAssessment{Disposition: crypto.ReplacementUncertain, Observations: observations}
	if observations < required {
		result.Reason = fmt.Sprintf("only %d/%d required providers produced a complete observation", observations, required)
		return result
	}
	if len(mined) > 1 {
		result.Reason = "providers report different mined replacement generations"
		return result
	}
	for hash := range mined {
		result.Disposition, result.MinedTxHash = crypto.ReplacementMined, hash
		return result
	}
	if len(pendingNonces) != 1 {
		result.Reason = "RPC providers disagree on pending nonce"
		return result
	}
	for pending := range pendingNonces {
		if pending > nonce {
			result.Disposition = crypto.ReplacementNonceConsumed
			result.Reason = fmt.Sprintf("pending nonce advanced to %d without a known transaction receipt", pending)
			return result
		}
	}
	result.Disposition = crypto.ReplacementStuck
	result.Reason = "known generations have no receipt and pending nonce has not advanced"
	return result
}

func finalizedBlockNumber() *big.Int { return big.NewInt(int64(rpc.FinalizedBlockNumber)) }

type headerObservation struct {
	header *types.Header
	group  string
}

func (p *rpcGeneration) independentProviders() int {
	groups := map[string]bool{}
	for e := range votingEndpoints(p.endpoints) {
		groups[e.trustGroup] = true
	}
	return len(groups)
}

// votingEndpoints retains read priority while excluding opaque upstreams from proofs.
func votingEndpoints(endpoints []*rpcEndpoint) iter.Seq[*rpcEndpoint] {
	return func(yield func(*rpcEndpoint) bool) {
		for _, endpoint := range endpoints {
			if endpoint.trustGroup != "" && !yield(endpoint) {
				return
			}
		}
	}
}
