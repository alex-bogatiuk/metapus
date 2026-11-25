package integrations

import (
	"encoding/hex"
	"strings"
	"unicode"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
)

type CredentialKeyFormat string

const (
	CredentialHex32   CredentialKeyFormat = "hex32"
	CredentialURLSafe CredentialKeyFormat = "url_safe"
	CredentialBearer  CredentialKeyFormat = "bearer"
)

func (f CredentialKeyFormat) Valid(value string) bool {
	switch f {
	case CredentialHex32:
		_, err := hex.DecodeString(value)
		return len(value) == 32 && err == nil
	case CredentialURLSafe:
		if value == "" {
			return false
		}
		for _, ch := range value {
			switch {
			case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '-', ch == '_':
			default:
				return false
			}
		}
		return true
	case CredentialBearer:
		return value != "" && len(value) <= 4096 && !strings.Contains(value, "://") && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) == -1
	case "":
		return true
	default:
		return false
	}
}

func (p CredentialProfile) Validate(c Credentials) error {
	if err := c.Validate(); err != nil {
		return err
	}
	// Empty replacement payloads retain the existing account credentials.
	if len(c.Keys) == 0 && c.APISecret == "" && c.Username == "" {
		return nil
	}
	if p.SingleKey && len(c.Keys) != 1 || p.KeyOnly && (c.APISecret != "" || c.Username != "") {
		return apperror.NewValidation("Набор реквизитов не соответствует профилю провайдера")
	}
	if p.RequireFields {
		for _, field := range p.Fields {
			if _, ok := c.Key(field.Name); !ok && !field.Optional {
				return apperror.NewValidation("Укажите все обязательные ключи провайдера").WithDetail("field", field.Name)
			}
		}
	}
	for _, key := range c.Keys {
		if !p.KeyFormat.Valid(key.Value) {
			return apperror.NewValidation("Введите ключ допустимого формата без URL и служебных символов").WithDetail("field", key.Name)
		}
	}
	return nil
}
