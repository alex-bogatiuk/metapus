package evm

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/ethereum/go-ethereum/common"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/fbsobreira/gotron-sdk/pkg/keys/hd"
)

type WalletData struct {
	Address    string
	PrivateKey string
	PublicKey  string
}

func NormalizeAddress(value string) (string, error) {
	if value != strings.TrimSpace(value) || !common.IsHexAddress(value) {
		return "", fmt.Errorf("invalid EVM address")
	}
	address := common.HexToAddress(value)
	if address == (common.Address{}) {
		return "", fmt.Errorf("zero EVM address is forbidden")
	}
	checksummed := address.Hex()
	body := strings.TrimPrefix(value, "0x")
	if body != strings.ToLower(body) && body != strings.ToUpper(body) && value != checksummed {
		return "", fmt.Errorf("invalid EIP-55 checksum")
	}
	return checksummed, nil
}

// ValidateHDPath restricts derivation to m/44'/60'/{account}'/0/{index}.
func ValidateHDPath(path string) error {
	normalized := strings.TrimSpace(path)
	if normalized != path || !strings.HasPrefix(normalized, "m/") {
		return fmt.Errorf("derivation path must be canonical and start with m/")
	}
	params, err := hd.NewParamsFromPath(normalized)
	if err != nil {
		return err
	}
	if params.Purpose != 44 || params.CoinType != 60 || params.Change {
		return fmt.Errorf("derivation path must match m/44'/60'/{account}'/0/{index}")
	}
	if normalized != "m/"+params.String() {
		return fmt.Errorf("derivation path is not canonical")
	}
	return nil
}

func DeriveWalletFromSeed(seed []byte, derivationPath string) (*WalletData, error) {
	if err := ValidateHDPath(derivationPath); err != nil {
		return nil, err
	}
	if len(seed) != 64 {
		return nil, fmt.Errorf("EVM HD seed must be exactly 64 bytes")
	}
	master, chainCode := hd.ComputeMastersFromSeed(seed, []byte("Bitcoin seed"))
	derived, err := hd.DerivePrivateKeyForPath(btcec.S256(), master, chainCode, derivationPath)
	if err != nil {
		return nil, err
	}
	privateKey, err := gethcrypto.ToECDSA(derived[:])
	if err != nil {
		return nil, fmt.Errorf("create derived EVM key: %w", err)
	}
	return &WalletData{
		Address:    gethcrypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		PrivateKey: hex.EncodeToString(gethcrypto.FromECDSA(privateKey)),
		PublicKey:  hex.EncodeToString(gethcrypto.FromECDSAPub(&privateKey.PublicKey)),
	}, nil
}
