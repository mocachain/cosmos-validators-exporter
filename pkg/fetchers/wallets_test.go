package fetchers

import (
	"context"
	"errors"
	"fmt"
	"main/assets"
	"main/pkg/config"
	"main/pkg/constants"
	"main/pkg/logger"
	"main/pkg/tendermint"
	"main/pkg/tracing"
	"testing"

	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testWallet = "0x1000000000000000000000000000000000000001"

func newTestWalletsFetcher(queries config.Queries, wallets ...config.Wallet) *WalletsFetcher {
	chains := []*config.Chain{{
		Name:        "chain",
		LCDEndpoint: "https://lcd.example",
		BaseDenom:   "amoca",
		Wallets:     wallets,
		Queries:     queries,
	}}
	rpcs := map[string]*tendermint.RPCWithConsumers{
		"chain": tendermint.RPCWithConsumersFromChain(
			chains[0],
			10,
			*logger.GetNopLogger(),
			tracing.InitNoopTracer(),
		),
	}

	return NewWalletsFetcher(logger.GetNopLogger(), chains, rpcs, tracing.InitNoopTracer())
}

// registerWalletResponders answers testWallet's five queries with the
// responder that respond returns for each fixture.
func registerWalletResponders(respond func(fixture string) httpmock.Responder) {
	for path, fixture := range map[string]string{
		"/cosmos/bank/v1beta1/balances/%s":                            "balances.json",
		"/cosmos/staking/v1beta1/delegations/%s":                      "wallet-delegations.json",
		"/cosmos/staking/v1beta1/delegators/%s/unbonding_delegations": "wallet-unbondings.json",
		"/cosmos/staking/v1beta1/delegators/%s/redelegations":         "wallet-redelegations.json",
		"/cosmos/distribution/v1beta1/delegators/%s/rewards":          "wallet-rewards.json",
	} {
		httpmock.RegisterResponder("GET", "https://lcd.example"+fmt.Sprintf(path, testWallet), respond(fixture))
	}
}

func TestWalletsFetcherBase(t *testing.T) {
	t.Parallel()

	fetcher := newTestWalletsFetcher(nil)

	assert.NotNil(t, fetcher)
	assert.Equal(t, constants.FetcherNameWallets, fetcher.Name())
	assert.Empty(t, fetcher.Dependencies())
}

func TestWalletsFetcherNoWallets(t *testing.T) {
	t.Parallel()

	data, queries := newTestWalletsFetcher(nil).Fetch(context.Background())
	assert.Empty(t, queries)

	walletsData, ok := data.(WalletsData)
	require.True(t, ok)
	assert.Empty(t, walletsData.Wallets["chain"])
}

func TestWalletsFetcherQueryDisabled(t *testing.T) {
	t.Parallel()

	fetcher := newTestWalletsFetcher(
		map[string]bool{"wallets": false},
		config.Wallet{Address: testWallet},
	)
	data, queries := fetcher.Fetch(context.Background())
	assert.Empty(t, queries)

	walletsData, ok := data.(WalletsData)
	require.True(t, ok)
	assert.Empty(t, walletsData.Wallets["chain"])
}

//nolint:paralleltest // disabled due to httpmock usage
func TestWalletsFetcherQueryError(t *testing.T) {
	httpmock.Activate()

	defer httpmock.DeactivateAndReset()

	registerWalletResponders(func(string) httpmock.Responder {
		return httpmock.NewErrorResponder(errors.New("error"))
	})

	data, queries := newTestWalletsFetcher(nil, config.Wallet{Address: testWallet}).
		Fetch(context.Background())
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

	registerWalletResponders(func(string) httpmock.Responder {
		return httpmock.NewBytesResponder(200, assets.GetBytesOrPanic("error.json"))
	})

	data, queries := newTestWalletsFetcher(nil, config.Wallet{Address: testWallet}).
		Fetch(context.Background())
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

	registerWalletResponders(func(fixture string) httpmock.Responder {
		return httpmock.NewBytesResponder(200, assets.GetBytesOrPanic(fixture))
	})

	data, queries := newTestWalletsFetcher(nil, config.Wallet{Address: testWallet, Name: "uploader"}).
		Fetch(context.Background())
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
