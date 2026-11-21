package evm

import (
	"errors"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

var ErrInvalidAddress = errors.New("invalid EVM address")

// NormalizeAddress accepts canonical lower/upper hexadecimal input or a valid
// mixed-case EIP-55 checksum and always returns EIP-55 form.
func NormalizeAddress(value string) (string, error) {
	if value != strings.TrimSpace(value) || len(value) != 42 || !strings.HasPrefix(value, "0x") || !common.IsHexAddress(value) {
		return "", ErrInvalidAddress
	}
	address := common.HexToAddress(value)
	if address == (common.Address{}) {
		return "", ErrInvalidAddress
	}
	hexPart := value[2:]
	if hexPart != strings.ToLower(hexPart) && hexPart != strings.ToUpper(hexPart) && value != address.Hex() {
		return "", ErrInvalidAddress
	}
	return address.Hex(), nil
}

func ValidateAddress(value string) error {
	_, err := NormalizeAddress(value)
	return err
}
