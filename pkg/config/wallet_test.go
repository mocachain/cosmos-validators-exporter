package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWalletValidateNoAddress(t *testing.T) {
	t.Parallel()

	wallet := Wallet{Name: "wallet"}
	require.Error(t, wallet.Validate())
}

func TestWalletValidateValid(t *testing.T) {
	t.Parallel()

	wallet := Wallet{Address: "wallet"}
	require.NoError(t, wallet.Validate())
}
