package config

import "errors"

// Wallet is an account tracked on its own: its balance, delegations,
// unbondings, redelegations and rewards.
type Wallet struct {
	Address string `toml:"address"`
	Name    string `toml:"name"`
}

func (w *Wallet) Validate() error {
	if w.Address == "" {
		return errors.New("empty wallet address")
	}

	return nil
}
