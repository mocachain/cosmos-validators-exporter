package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/guregu/null/v5"
)

type Chain struct {
	Name             string      `toml:"name"`
	LCDEndpoint      string      `toml:"lcd-endpoint"`
	BaseDenom        string      `toml:"base-denom"`
	Denoms           DenomInfos  `toml:"denoms"`
	BechWalletPrefix string      `toml:"bech-wallet-prefix"`
	Validators       []Validator `toml:"validators"`
	Wallets          []Wallet    `toml:"wallets"`
	Queries          Queries     `toml:"queries"`
	IsProvider       null.Bool   `toml:"is-provider"`

	ConsumerChains []*ConsumerChain `toml:"consumers"`
}

func (c *Chain) GetQueries() Queries {
	return c.Queries
}

func (c *Chain) GetHost() string {
	return c.LCDEndpoint
}

func (c *Chain) GetName() string {
	return c.Name
}

func (c *Chain) Validate() error {
	if c.Name == "" {
		return errors.New("empty chain name")
	}

	if c.LCDEndpoint == "" {
		return errors.New("no LCD endpoint provided")
	}

	if len(c.Validators) == 0 {
		return errors.New("no validators provided")
	}

	if c.BaseDenom == "" {
		return errors.New("base-denom is not set")
	}

	for index, validator := range c.Validators {
		err := validator.Validate()
		if err != nil {
			return fmt.Errorf("error in validator #%d: %s", index, err)
		}
	}

	// A wallet listed twice would export its series twice, under two names.
	addresses := map[string]bool{}

	for index, wallet := range c.Wallets {
		err := wallet.Validate()
		if err != nil {
			return fmt.Errorf("error in wallet #%d: %s", index, err)
		}

		address := strings.ToLower(wallet.Address)
		if addresses[address] {
			return fmt.Errorf("error in wallet #%d: %s is listed twice", index, wallet.Address)
		}

		addresses[address] = true
	}

	for index, denomInfo := range c.Denoms {
		err := denomInfo.Validate()
		if err != nil {
			return fmt.Errorf("error in denom #%d: %s", index, err)
		}
	}

	for index, chain := range c.ConsumerChains {
		err := chain.Validate()
		if err != nil {
			return fmt.Errorf("error in consumer chain #%d: %s", index, err)
		}
	}

	return nil
}

func (c *Chain) DisplayWarnings() []Warning {
	warnings := []Warning{}

	if c.BechWalletPrefix == "" {
		warnings = append(warnings, Warning{
			Message: "bech-wallet-prefix is not set, cannot query wallet balances.",
			Labels:  map[string]string{"chain": c.Name},
		})
	}

	for _, denom := range c.Denoms {
		warnings = append(warnings, denom.DisplayWarnings(c)...)
	}

	for _, validator := range c.Validators {
		warnings = append(warnings, validator.DisplayWarnings(c)...)
	}

	for _, consumer := range c.ConsumerChains {
		warnings = append(warnings, consumer.DisplayWarnings(c)...)
	}

	return warnings
}
