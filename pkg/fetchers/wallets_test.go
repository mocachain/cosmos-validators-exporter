package fetchers

import (
	"context"
	"errors"
	"main/assets"
	"main/pkg/config"
	"main/pkg/constants"
	"main/pkg/logger"
	"main/pkg/tendermint"
	"main/pkg/tracing"
	"main/pkg/types"
	"testing"

	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testWallet = "0x1000000000000000000000000000000000000001"

func newTestWalletsFetcher(chains ...*config.Chain) *WalletsFetcher {
	rpcs := make(map[string]*tendermint.RPCWithConsumers, len(chains))
	for _, chain := range chains {
		rpcs[chain.Name] = tendermint.RPCWithConsumersFromChain(
			chain,
			10,
			*logger.GetNopLogger(),
			tracing.InitNoopTracer(),
		)
	}

	return NewWalletsFetcher(logger.GetNopLogger(), chains, rpcs, tracing.InitNoopTracer())
}

// newTestWalletsChain returns a chain whose LCD is https://<name>.example.
func newTestWalletsChain(name string, queries config.Queries, wallets ...string) *config.Chain {
	chain := &config.Chain{
		Name:        name,
		LCDEndpoint: "https://" + name + ".example",
		BaseDenom:   "amoca",
		Queries:     queries,
	}

	for _, wallet := range wallets {
		chain.Wallets = append(chain.Wallets, config.Wallet{Address: wallet})
	}

	return chain
}

// registerWalletResponders answers a wallet's five queries on chain's LCD
// with the responder that respond returns for each query's fixture.
func registerWalletResponders(
	chain *config.Chain,
	wallet string,
	respond func(fixture string) httpmock.Responder,
) {
	host := chain.LCDEndpoint
	for url, fixture := range map[string]string{
		host + "/cosmos/bank/v1beta1/balances/" + wallet:                                 "balances.json",
		host + "/cosmos/staking/v1beta1/delegations/" + wallet:                           "wallet-delegations.json",
		host + "/cosmos/staking/v1beta1/delegators/" + wallet + "/unbonding_delegations": "wallet-unbondings.json",
		host + "/cosmos/staking/v1beta1/delegators/" + wallet + "/redelegations":         "wallet-redelegations.json",
		host + "/cosmos/distribution/v1beta1/delegators/" + wallet + "/rewards":          "wallet-rewards.json",
	} {
		httpmock.RegisterResponder("GET", url, respond(fixture))
	}
}

func fixtureResponder(fixture string) httpmock.Responder {
	return httpmock.NewBytesResponder(200, assets.GetBytesOrPanic(fixture))
}

func TestWalletsFetcherBase(t *testing.T) {
	t.Parallel()

	fetcher := newTestWalletsFetcher(newTestWalletsChain("chain", nil))

	assert.NotNil(t, fetcher)
	assert.Equal(t, constants.FetcherNameWallets, fetcher.Name())
	assert.Empty(t, fetcher.Dependencies())
}

func TestWalletsFetcherNoWallets(t *testing.T) {
	t.Parallel()

	data, queries := newTestWalletsFetcher(newTestWalletsChain("chain", nil)).Fetch(context.Background())
	assert.Empty(t, queries)

	walletsData, ok := data.(WalletsData)
	require.True(t, ok)
	assert.Empty(t, walletsData.Wallets["chain"])
}

func TestWalletsFetcherQueryDisabled(t *testing.T) {
	t.Parallel()

	chain := newTestWalletsChain("chain", map[string]bool{"wallets": false}, testWallet)
	data, queries := newTestWalletsFetcher(chain).Fetch(context.Background())
	assert.Empty(t, queries)

	walletsData, ok := data.(WalletsData)
	require.True(t, ok)
	assert.Empty(t, walletsData.Wallets["chain"])
}

//nolint:paralleltest // disabled due to httpmock usage
func TestWalletsFetcherQueryError(t *testing.T) {
	httpmock.Activate()

	defer httpmock.DeactivateAndReset()

	chain := newTestWalletsChain("chain", nil, testWallet)
	registerWalletResponders(chain, testWallet, func(string) httpmock.Responder {
		return httpmock.NewErrorResponder(errors.New("error"))
	})

	data, queries := newTestWalletsFetcher(chain).Fetch(context.Background())
	assert.Len(t, queries, 5)

	for _, query := range queries {
		assert.False(t, query.Success)
	}

	walletsData, ok := data.(WalletsData)
	require.True(t, ok)
	assert.Equal(t, &WalletInfo{}, walletsData.Wallets["chain"][testWallet])
}

//nolint:paralleltest // disabled due to httpmock usage
func TestWalletsFetcherNodeError(t *testing.T) {
	httpmock.Activate()

	defer httpmock.DeactivateAndReset()

	chain := newTestWalletsChain("chain", nil, testWallet)
	registerWalletResponders(chain, testWallet, func(string) httpmock.Responder {
		return fixtureResponder("error.json")
	})

	data, queries := newTestWalletsFetcher(chain).Fetch(context.Background())
	assert.Len(t, queries, 5)

	for _, query := range queries {
		assert.False(t, query.Success)
	}

	walletsData, ok := data.(WalletsData)
	require.True(t, ok)
	assert.Equal(t, &WalletInfo{}, walletsData.Wallets["chain"][testWallet])
}

//nolint:paralleltest // disabled due to httpmock usage
func TestWalletsFetcherQuerySuccess(t *testing.T) {
	httpmock.Activate()

	defer httpmock.DeactivateAndReset()

	// The balance query covers validator wallets only. Tracked wallets still
	// get their balances.
	chain := newTestWalletsChain("chain", map[string]bool{"balance": false}, testWallet)
	registerWalletResponders(chain, testWallet, fixtureResponder)

	data, queries := newTestWalletsFetcher(chain).Fetch(context.Background())
	assert.Len(t, queries, 5)

	for _, query := range queries {
		assert.True(t, query.Success)
	}

	walletsData, ok := data.(WalletsData)
	require.True(t, ok)

	info := walletsData.Wallets["chain"][testWallet]
	require.NotNil(t, info)

	require.Len(t, info.Balances, 1)
	assert.Equal(t, "uatom", info.Balances[0].Denom)
	assert.InDelta(t, 596250, info.Balances[0].Amount, 0.01)

	require.Len(t, info.Delegations, 1)
	assert.Equal(t, "0xA000000000000000000000000000000000000001", info.Delegations[0].Delegation.ValidatorAddress)
	assert.Equal(t, "amoca", info.Delegations[0].Balance.Denom)

	require.Len(t, info.Unbondings, 1)
	assert.Equal(t, "0xA000000000000000000000000000000000000001", info.Unbondings[0].ValidatorAddress)
	assert.Len(t, info.Unbondings[0].Entries, 2)

	require.Len(t, info.Redelegations, 1)
	assert.Equal(t, "0xA000000000000000000000000000000000000001", info.Redelegations[0].Redelegation.ValidatorSrcAddress)
	assert.Equal(t, "0xB000000000000000000000000000000000000002", info.Redelegations[0].Redelegation.ValidatorDstAddress)
	assert.Len(t, info.Redelegations[0].Entries, 1)

	require.Len(t, info.Rewards, 1)
	assert.Equal(t, "0xA000000000000000000000000000000000000001", info.Rewards[0].ValidatorAddress)
	require.Len(t, info.Rewards[0].Reward, 1)
	assert.Equal(t, "amoca", info.Rewards[0].Reward[0].Denom)
}

//nolint:paralleltest // disabled due to httpmock usage
func TestWalletsFetcherPartialData(t *testing.T) {
	httpmock.Activate()

	defer httpmock.DeactivateAndReset()

	// The wallet holds no coins, and its rewards query fails.
	chain := newTestWalletsChain("chain", nil, testWallet)
	registerWalletResponders(chain, testWallet, func(fixture string) httpmock.Responder {
		switch fixture {
		case "balances.json":
			return httpmock.NewStringResponder(200, `{"balances":[],"pagination":{"next_key":null,"total":"0"}}`)
		case "wallet-rewards.json":
			return httpmock.NewErrorResponder(errors.New("error"))
		default:
			return fixtureResponder(fixture)
		}
	})

	data, queries := newTestWalletsFetcher(chain).Fetch(context.Background())
	assert.Len(t, queries, 5)

	succeeded := 0

	for _, query := range queries {
		if query.Success {
			succeeded++
		}
	}

	assert.Equal(t, 4, succeeded)

	walletsData, ok := data.(WalletsData)
	require.True(t, ok)

	info := walletsData.Wallets["chain"][testWallet]
	require.NotNil(t, info)
	assert.Equal(t, []types.Amount{{Amount: 0, Denom: "amoca"}}, info.Balances)
	assert.Len(t, info.Delegations, 1)
	assert.Len(t, info.Unbondings, 1)
	assert.Len(t, info.Redelegations, 1)
	assert.Nil(t, info.Rewards)
}

//nolint:paralleltest // disabled due to httpmock usage
func TestWalletsFetcherMultipleChains(t *testing.T) {
	httpmock.Activate()

	defer httpmock.DeactivateAndReset()

	// The first two chains' workers run while the third chain is still being
	// set up. go test -race fails this test if that set-up races them. The
	// third chain tracks no wallets: its own workers' locking would hide the
	// race from the detector.
	otherWallet := "0x2000000000000000000000000000000000000002"
	chains := []*config.Chain{
		newTestWalletsChain("chain1", nil, testWallet, otherWallet),
		newTestWalletsChain("chain2", nil, testWallet, otherWallet),
		newTestWalletsChain("chain3", nil),
	}

	for _, chain := range chains {
		for _, wallet := range chain.Wallets {
			registerWalletResponders(chain, wallet.Address, fixtureResponder)
		}
	}

	data, queries := newTestWalletsFetcher(chains...).Fetch(context.Background())
	assert.Len(t, queries, 20)

	for _, query := range queries {
		assert.True(t, query.Success)
	}

	walletsData, ok := data.(WalletsData)
	require.True(t, ok)

	for _, chain := range []string{"chain1", "chain2"} {
		for _, wallet := range []string{testWallet, otherWallet} {
			info := walletsData.Wallets[chain][wallet]
			require.NotNil(t, info, "%s %s", chain, wallet)
			assert.Len(t, info.Balances, 1)
			assert.Len(t, info.Rewards, 1)
		}
	}

	assert.Empty(t, walletsData.Wallets["chain3"])
}
