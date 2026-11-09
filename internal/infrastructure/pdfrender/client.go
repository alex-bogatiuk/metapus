// Package pdfrender implements the internal HTML-to-PDF capability.
package pdfrender

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/json"
	"metapus/internal/domain/printing"
)

const MaxHTML = 4 << 20
const MaxPDF = 16 << 20
const Timeout = 30 * time.Second

type Request struct {
	HTML    string              `json:"html"`
	Options printing.PDFOptions `json:"options"`
}

type Client struct {
	base  string
	http  *http.Client
	slots chan struct{}
}

func NewClient(endpoint string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid PDF renderer endpoint")
	}
	return &Client{base: strings.TrimRight(endpoint, "/"), slots: make(chan struct{}, 2), http: &http.Client{
		Timeout: Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func Unavailable(err error) error {
	return &apperror.AppError{Code: "PDF_RENDERER_UNAVAILABLE", Message: "PDF renderer is unavailable", HTTPStatus: http.StatusServiceUnavailable, Err: err}
}

func (c *Client) Render(ctx context.Context, html []byte, options printing.PDFOptions) ([]byte, error) {
	if len(html) == 0 || len(html) > MaxHTML {
		return nil, apperror.NewValidation("print HTML exceeds the supported size")
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, Unavailable(ctx.Err())
	}
	body, err := json.Marshal(Request{HTML: string(html), Options: options})
	if err != nil {
		return nil, Unavailable(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/render", bytes.NewReader(body))
	if err != nil {
		return nil, Unavailable(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, Unavailable(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/pdf" {
		return nil, Unavailable(fmt.Errorf("renderer response status %d", res.StatusCode))
	}
	pdf, err := io.ReadAll(io.LimitReader(res.Body, MaxPDF+1))
	if err != nil {
		return nil, Unavailable(err)
	}
	if len(pdf) > MaxPDF || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		return nil, Unavailable(fmt.Errorf("invalid renderer output"))
	}
	return pdf, nil
}

func (c *Client) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health/ready", nil)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("renderer is not ready")
	}
	return nil
}
