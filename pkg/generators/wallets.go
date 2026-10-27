package generators

import (
	"main/pkg/config"
	"main/pkg/constants"
	fetchersPkg "main/pkg/fetchers"
	statePkg "main/pkg/state"
	"main/pkg/types"
	"maps"

	"github.com/prometheus/client_golang/prometheus"
)

type WalletsGenerator struct {
	Chains []*config.Chain
}

func NewWalletsGenerator(chains []*config.Chain) *WalletsGenerator {
	return &WalletsGenerator{Chains: chains}
}

func (g *WalletsGenerator) Generate(state *statePkg.State) []prometheus.Collector {
	data, ok := statePkg.StateGet[fetchersPkg.WalletsData](state, constants.FetcherNameWallets)
	if !ok {
		return []prometheus.Collector{}
	}

	newGauge := func(name, help string, labels ...string) *prometheus.GaugeVec {
		return prometheus.NewGaugeVec(
			prometheus.GaugeOpts{Name: constants.WalletMetricsPrefix + name, Help: help},
			append([]string{"chain_id", "address", "name", "denom"}, labels...),
		)
	}

	balanceGauge := newGauge("balance", "Wallet balance (in tokens)")
	delegationsGauge := newGauge("delegations", "Wallet delegations (in tokens)", "delegated_to")
	unbondingsGauge := newGauge("unbondings", "Wallet unbondings (in tokens)", "unbonded_from")
	redelegationsGauge := newGauge(
		"redelegations",
		"Wallet redelegations (in tokens)",
		"redelegated_from",
		"redelegated_to",
	)
	rewardsGauge := newGauge("rewards", "Wallet unclaimed rewards (in tokens)", "validator_address")

	for _, chain := range g.Chains {
		chainWallets, ok := data.Wallets[chain.Name]
		if !ok {
			continue
		}

		for _, wallet := range chain.Wallets {
			info, ok := chainWallets[wallet.Address]
			if !ok {
				continue
			}

			set := func(gauge *prometheus.GaugeVec, amount types.Amount, labels prometheus.Labels) {
				converted := chain.Denoms.Convert(&amount)
				if converted == nil {
					return
				}

				all := prometheus.Labels{
					"chain_id": chain.Name,
					"address":  wallet.Address,
					"name":     wallet.Name,
					"denom":    converted.Denom,
				}
				maps.Copy(all, labels)
				gauge.With(all).Set(converted.Amount)
			}

			for _, balance := range info.Balances {
				set(balanceGauge, balance, nil)
			}

			for _, delegation := range info.Delegations {
				set(delegationsGauge, delegation.Balance.ToAmount(), prometheus.Labels{
					"delegated_to": delegation.Delegation.ValidatorAddress,
				})
			}

			for _, unbonding := range info.Unbondings {
				set(unbondingsGauge, sumEntries(unbonding.Entries, chain.BaseDenom), prometheus.Labels{
					"unbonded_from": unbonding.ValidatorAddress,
				})
			}

			for _, redelegation := range info.Redelegations {
				set(redelegationsGauge, sumEntries(redelegation.Entries, chain.BaseDenom), prometheus.Labels{
					"redelegated_from": redelegation.Redelegation.ValidatorSrcAddress,
					"redelegated_to":   redelegation.Redelegation.ValidatorDstAddress,
				})
			}

			for _, reward := range info.Rewards {
				for _, amount := range reward.Reward {
					set(rewardsGauge, amount.ToAmount(), prometheus.Labels{
						"validator_address": reward.ValidatorAddress,
					})
				}
			}
		}
	}

	return []prometheus.Collector{
		balanceGauge,
		delegationsGauge,
		unbondingsGauge,
		redelegationsGauge,
		rewardsGauge,
	}
}

func sumEntries(entries []types.BalanceEntry, denom string) types.Amount {
	sum := types.Amount{Denom: denom}

	for _, entry := range entries {
		sum.Amount += entry.Balance.MustFloat64()
	}

	return sum
}
