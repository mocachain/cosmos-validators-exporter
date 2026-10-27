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

// Fetch keeps its state local, so scrapes that overlap do not share it.
func (q *WalletsFetcher) Fetch(
	ctx context.Context,
	data ...any,
) (any, []*types.QueryInfo) {
	var (
		wg    sync.WaitGroup
		mutex sync.Mutex
	)

	queryInfos := []*types.QueryInfo{}
	allWallets := make(map[string]map[string]*WalletInfo, len(q.Chains))

	for _, chain := range q.Chains {
		allWallets[chain.Name] = map[string]*WalletInfo{}
	}

	for _, chain := range q.Chains {
		if !chain.Queries.Enabled("wallets") {
			continue
		}

		rpc := q.RPCs[chain.Name].RPC

		for _, wallet := range chain.Wallets {
			info := &WalletInfo{}
			allWallets[chain.Name][wallet.Address] = info

			// record keeps a query's info and reports whether it succeeded.
			record := func(what string) func(*types.QueryInfo, error) bool {
				return func(query *types.QueryInfo, err error) bool {
					mutex.Lock()
					defer mutex.Unlock()

					if query != nil {
						queryInfos = append(queryInfos, query)
					}

					if err != nil {
						q.Logger.Error().
							Err(err).
							Str("chain", chain.Name).
							Str("wallet", wallet.Address).
							Msg("Error querying for wallet " + what)

						return false
					}

					return true
				}
			}

			runQuery(ctx, &wg, wallet.Address, rpc.GetWalletBalances, record("balance"), func(balances []types.Amount) {
				// The bank API leaves out zero coins. Report 0 of the base
				// denom, so an empty wallet still shows up.
				if len(balances) == 0 {
					balances = []types.Amount{{Denom: chain.BaseDenom}}
				}

				info.Balances = balances
			})
			runQuery(ctx, &wg, wallet.Address, rpc.GetWalletDelegations, record("delegations"), func(delegations []types.WalletDelegation) {
				info.Delegations = delegations
			})
			runQuery(ctx, &wg, wallet.Address, rpc.GetWalletUnbondings, record("unbondings"), func(unbondings []types.WalletUnbonding) {
				info.Unbondings = unbondings
			})
			runQuery(ctx, &wg, wallet.Address, rpc.GetWalletRedelegations, record("redelegations"), func(redelegations []types.WalletRedelegation) {
				info.Redelegations = redelegations
			})
			runQuery(ctx, &wg, wallet.Address, rpc.GetWalletRewards, record("rewards"), func(rewards []types.WalletReward) {
				info.Rewards = rewards
			})
		}
	}

	wg.Wait()

	return WalletsData{Wallets: allWallets}, queryInfos
}

// runQuery runs one of a wallet's queries in its own goroutine and hands the
// result to store if the query succeeds.
func runQuery[T any](
	ctx context.Context,
	wg *sync.WaitGroup,
	wallet string,
	query func(string, context.Context) (T, *types.QueryInfo, error),
	record func(*types.QueryInfo, error) bool,
	store func(T),
) {
	wg.Go(func() {
		result, info, err := query(wallet, ctx)
		if record(info, err) {
			store(result)
		}
	})
}
