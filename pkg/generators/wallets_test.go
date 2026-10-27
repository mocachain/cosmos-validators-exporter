package generators

import (
	"main/pkg/config"
	"main/pkg/constants"
	"main/pkg/fetchers"
	statePkg "main/pkg/state"
	"main/pkg/types"
	"maps"
	"testing"

	"cosmossdk.io/math"

	"github.com/guregu/null/v5"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalletsGeneratorNoState(t *testing.T) {
	t.Parallel()

	state := statePkg.NewState()
	generator := NewWalletsGenerator([]*config.Chain{})
	results := generator.Generate(state)
	assert.Empty(t, results)
}

func TestWalletsGeneratorNotEmptyState(t *testing.T) {
	t.Parallel()

	delegation := types.WalletDelegation{
		Balance: types.ResponseAmount{Amount: math.LegacyNewDec(2000000), Denom: "uatom"},
	}
	delegation.Delegation.ValidatorAddress = "validator1"

	redelegation := types.WalletRedelegation{
		Entries: []types.BalanceEntry{{Balance: math.LegacyNewDec(300000)}},
	}
	redelegation.Redelegation.ValidatorSrcAddress = "validator1"
	redelegation.Redelegation.ValidatorDstAddress = "validator2"

	state := statePkg.NewState()
	state.Set(constants.FetcherNameWallets, fetchers.WalletsData{
		Wallets: map[string]map[string]*fetchers.WalletInfo{
			"chain": {
				"wallet": {
					Balances: []types.Amount{
						{Amount: 100000, Denom: "uatom"},
						{Amount: 200000, Denom: "ustake"},
						{Amount: 300000, Denom: "uignored"},
					},
					Delegations: []types.WalletDelegation{delegation},
					Unbondings: []types.WalletUnbonding{{
						ValidatorAddress: "validator2",
						Entries: []types.BalanceEntry{
							{Balance: math.LegacyNewDec(500000)},
							{Balance: math.LegacyNewDec(250000)},
						},
					}},
					Redelegations: []types.WalletRedelegation{redelegation},
					Rewards: []types.WalletReward{{
						ValidatorAddress: "validator1",
						Reward: []types.ResponseAmount{
							{Amount: math.LegacyNewDec(1234567), Denom: "uatom"},
							{Amount: math.LegacyNewDec(1), Denom: "uignored"},
						},
					}},
				},
			},
		},
	})

	chains := []*config.Chain{
		{
			Name:      "chain",
			BaseDenom: "uatom",
			Wallets: []config.Wallet{
				{Address: "wallet", Name: "uploader"},
				{Address: "not-fetched"},
			},
			Denoms: config.DenomInfos{
				{Denom: "uatom", DisplayDenom: "atom", DenomExponent: 6},
				{Denom: "uignored", Ignore: null.BoolFrom(true)},
			},
		},
		{
			Name:    "chain2",
			Wallets: []config.Wallet{{Address: "wallet"}},
		},
	}
	generator := NewWalletsGenerator(chains)
	results := generator.Generate(state)
	require.Len(t, results, 5)

	labels := func(extra prometheus.Labels) prometheus.Labels {
		all := prometheus.Labels{"chain_id": "chain", "address": "wallet", "name": "uploader", "denom": "atom"}
		maps.Copy(all, extra)

		return all
	}

	balance, ok := results[0].(*prometheus.GaugeVec)
	require.True(t, ok)
	assert.Equal(t, 2, testutil.CollectAndCount(balance))
	assert.InEpsilon(t, 0.1, testutil.ToFloat64(balance.With(labels(nil))), 0.01)
	assert.InEpsilon(t, float64(200000), testutil.ToFloat64(balance.With(labels(prometheus.Labels{
		"denom": "ustake",
	}))), 0.01)

	delegations, ok := results[1].(*prometheus.GaugeVec)
	require.True(t, ok)
	assert.Equal(t, 1, testutil.CollectAndCount(delegations))
	assert.InEpsilon(t, float64(2), testutil.ToFloat64(delegations.With(labels(prometheus.Labels{
		"delegated_to": "validator1",
	}))), 0.01)

	unbondings, ok := results[2].(*prometheus.GaugeVec)
	require.True(t, ok)
	assert.Equal(t, 1, testutil.CollectAndCount(unbondings))
	assert.InEpsilon(t, 0.75, testutil.ToFloat64(unbondings.With(labels(prometheus.Labels{
		"unbonded_from": "validator2",
	}))), 0.01)

	redelegations, ok := results[3].(*prometheus.GaugeVec)
	require.True(t, ok)
	assert.Equal(t, 1, testutil.CollectAndCount(redelegations))
	assert.InEpsilon(t, 0.3, testutil.ToFloat64(redelegations.With(labels(prometheus.Labels{
		"redelegated_from": "validator1",
		"redelegated_to":   "validator2",
	}))), 0.01)

	rewards, ok := results[4].(*prometheus.GaugeVec)
	require.True(t, ok)
	assert.Equal(t, 1, testutil.CollectAndCount(rewards))
	assert.InEpsilon(t, 1.234567, testutil.ToFloat64(rewards.With(labels(prometheus.Labels{
		"validator_address": "validator1",
	}))), 0.01)
}
