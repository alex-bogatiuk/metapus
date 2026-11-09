package authqr

import (
	"encoding/base64"
	"github.com/skip2/go-qrcode"
)

type Encoder struct{}

func (Encoder) EncodeDataURL(uri string) (string, error) {
	png, err := qrcode.Encode(uri, qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}
