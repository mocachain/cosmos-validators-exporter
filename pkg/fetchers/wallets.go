package fetchers

import (
	"context"
	"main/pkg/config"
	"main/pkg/constants"
	"main/pkg/tendermint"
	"main/pkg/types"
	"sync"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// WalletsFetcher queries each tracked wallet's balance, delegations,
// unbondings, redelegations and rewards.
type WalletsFetcher struct {
	Logger zerolog.Logger
	Chains []*config.Chain
	RPCs   map[string]*tendermint.RPCWithConsumers
	Tracer trace.Tracer

	wg    sync.WaitGroup
	mutex sync.Mutex

	queryInfos []*types.QueryInfo
	allWallets map[string]map[string]*WalletInfo
}

type WalletInfo struct {
	Balances      []types.Amount
	Delegations   []types.WalletDelegation
	Unbondings    []types.WalletUnbonding
	Redelegations []types.WalletRedelegation
	Rewards       []types.WalletReward
}

type WalletsData struct {
	// Wallets maps a chain name, then a wallet address, to what was fetched.
	Wallets map[string]map[string]*WalletInfo
}

func NewWalletsFetcher(
	logger *zerolog.Logger,
	chains []*config.Chain,
	rpcs map[string]*tendermint.RPCWithConsumers,
	tracer trace.Tracer,
) *WalletsFetcher {
	return &WalletsFetcher{
		Logger: logger.With().Str("component", "wallets_fetcher").Logger(),
		Chains: chains,
		RPCs:   rpcs,
		Tracer: tracer,
	}
}

func (q *WalletsFetcher) Dependencies() []constants.FetcherName {
	return []constants.FetcherName{}
}

func (q *WalletsFetcher) Name() constants.FetcherName {
	return constants.FetcherNameWallets
}

func (q *WalletsFetcher) Fetch(
	ctx context.Context,
	data ...any,
) (any, []*types.QueryInfo) {
	q.queryInfos = []*types.QueryInfo{}
	q.allWallets = map[string]map[string]*WalletInfo{}

	for _, chain := range q.Chains {
		q.allWallets[chain.Name] = map[string]*WalletInfo{}

		if !chain.Queries.Enabled("wallets") {
			continue
		}

		rpc := q.RPCs[chain.Name].RPC

		for _, wallet := range chain.Wallets {
			q.wg.Add(1)

			go q.processWallet(ctx, chain.Name, wallet.Address, rpc)
		}
	}

	q.wg.Wait()

	return WalletsData{Wallets: q.allWallets}, q.queryInfos
}

func (q *WalletsFetcher) processWallet(
	ctx context.Context,
	chainName string,
	wallet string,
	rpc *tendermint.RPC,
) {
	defer q.wg.Done()

	info := &WalletInfo{}

	balances, query, err := rpc.GetWalletBalance(wallet, ctx)
	if q.record(chainName, wallet, "balance", query, err) {
		info.Balances = balances
	}

	delegations, query, err := rpc.GetWalletDelegations(wallet, ctx)
	if q.record(chainName, wallet, "delegations", query, err) {
		info.Delegations = delegations
	}

	unbondings, query, err := rpc.GetWalletUnbondings(wallet, ctx)
	if q.record(chainName, wallet, "unbondings", query, err) {
		info.Unbondings = unbondings
	}

	redelegations, query, err := rpc.GetWalletRedelegations(wallet, ctx)
	if q.record(chainName, wallet, "redelegations", query, err) {
		info.Redelegations = redelegations
	}

	rewards, query, err := rpc.GetWalletRewards(wallet, ctx)
	if q.record(chainName, wallet, "rewards", query, err) {
		info.Rewards = rewards
	}

	q.mutex.Lock()
	defer q.mutex.Unlock()

	q.allWallets[chainName][wallet] = info
}

// record keeps a query's info and reports whether the query succeeded.
func (q *WalletsFetcher) record(
	chainName string,
	wallet string,
	what string,
	query *types.QueryInfo,
	err error,
) bool {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if query != nil {
		q.queryInfos = append(q.queryInfos, query)
	}

	if err != nil {
		q.Logger.Error().
			Err(err).
			Str("chain", chainName).
			Str("wallet", wallet).
			Msg("Error querying for wallet " + what)

		return false
	}

	return true
}
