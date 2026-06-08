package utils

import (
	"bytes"
	"encoding/hex"
	"main/pkg/constants"
	"net/http"
	"strconv"
	"strings"

	"github.com/tnakagawa/goref/bech32m"
)

func BoolToFloat64(b bool) float64 {
	if b {
		return 1
	}

	return 0
}

func StrToFloat64(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(err)
	}

	return f
}

func ChangeBech32Prefix(source, newPrefix string) (string, error) {
	// For EVM-style Cosmos chains the validator operator, wallet account, and
	// consensus identifier are all the same 20-byte 0x-prefixed hex string —
	// there is no separate bech32 prefix to swap. Return the source unchanged
	// so downstream lookups (wallet balance, rewards, signing info) receive a
	// usable address.
	if isEVMAddress(source) {
		return source, nil
	}

	_, bytes, _, err := bech32m.Decode(source)
	if err != nil {
		return "", err
	}

	return bech32m.Encode(newPrefix, bytes, bech32m.Bech32), nil
}

func Filter[T any](slice []T, f func(T) bool) []T {
	var n []T

	for _, e := range slice {
		if f(e) {
			n = append(n, e)
		}
	}

	return n
}

func Map[T any, V any](slice []T, f func(T) V) []V {
	n := make([]V, len(slice))

	for index, e := range slice {
		n[index] = f(e)
	}

	return n
}

func Find[T any](slice []T, predicate func(T) bool) (*T, bool) {
	for _, elt := range slice {
		if predicate(elt) {
			return &elt, true
		}
	}

	return nil, false
}

func FindIndex[T any](slice []T, predicate func(T) bool) (int, bool) {
	for index, elt := range slice {
		if predicate(elt) {
			return index, true
		}
	}

	return 0, false
}

// isEVMAddress reports whether s looks like a 0x-prefixed 20-byte hex string
// (an Ethereum-style account address). EVM-based Cosmos chains such as Moca,
// Evmos, and Berachain expose validator operator addresses in this form
// instead of the cosmosvaloper1... bech32 form, so bech32m.Decode can never
// succeed on them — they contain characters outside the bech32 alphabet
// regardless of case, and EIP-55 checksumming requires mixed case.
func isEVMAddress(s string) bool {
	if len(s) != 42 || !strings.HasPrefix(s, "0x") {
		return false
	}

	_, err := hex.DecodeString(s[2:])

	return err == nil
}

func CompareTwoBech32(first, second string) (bool, error) {
	if isEVMAddress(first) && isEVMAddress(second) {
		return strings.EqualFold(first, second), nil
	}

	_, firstBytes, _, err := bech32m.Decode(first)
	if err != nil {
		return false, err
	}

	_, secondBytes, _, err := bech32m.Decode(second)
	if err != nil {
		return false, err
	}

	return bytes.Equal(firstBytes, secondBytes), nil
}

func GetBlockHeightFromHeader(header http.Header) (int64, error) {
	valueStr := header.Get(constants.HeaderBlockHeight)
	if valueStr == "" {
		return 0, nil
	}

	value, err := strconv.ParseInt(valueStr, 10, 64)
	if err != nil {
		return 0, err
	}

	return value, nil
}
