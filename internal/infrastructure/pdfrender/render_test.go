package pdfrender

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/domain/printing"
	"github.com/stretchr/testify/require"
)

type renderFunc func(context.Context, []byte, printing.PDFOptions) ([]byte, error)

func (f renderFunc) Render(ctx context.Context, html []byte, options printing.PDFOptions) ([]byte, error) {
	return f(ctx, html, options)
}

func TestPrepareHTMLRemovesActiveContentAndEnforcesResourcePolicy(t *testing.T) {
	input := `<html><head><meta http-equiv="refresh" content="0;url=http://127.0.0.1"><base href="http://internal/"><style>body{font-family:serif;background:url(http://example.test)}</style></head><body onload="alert(1)"><script>location='http://internal/'</script><iframe src="file:///etc/passwd"></iframe><svg><foreignObject><script>x()</script></foreignObject></svg><img src="http://internal/pixel"><img src="data:image/png;base64,aGVsbG8="><a href="javascript:x()">link</a><table><tr><td>Проверка кириллицы</td></tr></table></body></html>`
	output, err := prepareHTML([]byte(input), printing.PDFOptions{PaperSize: "A5", Landscape: true})
	require.NoError(t, err)
	s := string(output)
	for _, forbidden := range []string{"<script", "<iframe", "<svg", "<base", "onload=", `http-equiv="refresh"`, `src="http`, `href=`, "file:///etc/passwd"} {
		require.NotContains(t, s, forbidden)
	}
	require.Contains(t, html.UnescapeString(s), contentPolicy)
	require.Less(t, strings.Index(s, "Content-Security-Policy"), strings.Index(s, "<style"))
	require.Contains(t, s, "Проверка кириллицы")
	require.Contains(t, s, "size: A5 landscape")
	require.Contains(t, s, "data:image/png;base64,")
	_, err = prepareHTML([]byte("hello"), printing.PDFOptions{PaperSize: "A4; @import url(http://internal)"})
	require.Error(t, err)
}

func TestInternalEndpointRejectsURLAndClientPreservesContext(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	handler := NewServer(renderFunc(func(ctx context.Context, source []byte, options printing.PDFOptions) ([]byte, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}))
	handler.SetReady(true)
	server := httptest.NewServer(handler)
	defer server.Close()
	for _, body := range []string{`{"url":"http://internal"}`, `{"html":"hello","options":{},"url":"file:///etc/passwd"}`, `{"html":"hello","options":{"paperSize":"invalid"}}`} {
		res, err := http.Post(server.URL+"/render", "application/json", strings.NewReader(body))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
		require.NoError(t, res.Body.Close())
	}
	client, err := NewClient(server.URL)
	require.NoError(t, err)
	require.NoError(t, client.Ready(context.Background()))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := client.Render(ctx, []byte("<p>Печать</p>"), printing.PDFOptions{})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("render did not start")
	}
	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
		require.Equal(t, http.StatusServiceUnavailable, apperror.GetHTTPStatus(err))
	case <-time.After(5 * time.Second):
		t.Fatal("client ignored cancellation")
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("server render ignored cancellation")
	}
}

func TestClientAcceptsOnlyBoundedPDFAndNeverFollowsRedirects(t *testing.T) {
	for _, scenario := range []struct {
		name              string
		status            int
		contentType, body string
		ok                bool
	}{
		{"pdf", 200, "application/pdf", "%PDF-1.7\nexact", true},
		{"html", 200, "text/html", "<p>error</p>", false},
		{"invalid", 200, "application/pdf", "not a PDF", false},
		{"redirect", 302, "application/pdf", "", false},
		{"failure", 503, "application/pdf", "", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "http://127.0.0.1:1/")
				w.Header().Set("Content-Type", scenario.contentType)
				w.WriteHeader(scenario.status)
				_, _ = w.Write([]byte(scenario.body))
			}))
			defer server.Close()
			client, err := NewClient(server.URL)
			require.NoError(t, err)
			_, err = client.Render(context.Background(), []byte("print"), printing.PDFOptions{})
			if scenario.ok {
				require.NoError(t, err)
			} else {
				var appErr *apperror.AppError
				require.True(t, errors.As(err, &appErr))
				require.Equal(t, "PDF_RENDERER_UNAVAILABLE", appErr.Code)
			}
		})
	}
}

func TestServerRejectsOverloadBeforeReadingBody(t *testing.T) {
	started, release := make(chan struct{}, 2), make(chan struct{})
	handler := NewServer(renderFunc(func(ctx context.Context, source []byte, options printing.PDFOptions) ([]byte, error) {
		started <- struct{}{}
		<-release
		return []byte("%PDF-1.7"), nil
	}))
	handler.SetReady(true)
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/render", strings.NewReader(`{"html":"print"}`)))
		}()
	}
	defer func() { close(release); <-done; <-done }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("render did not start")
		}
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/render", nil)
	request.Body = unreadableBody{t}
	handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

type unreadableBody struct{ t *testing.T }

func (b unreadableBody) Read([]byte) (int, error) {
	b.t.Error("overloaded renderer read request body")
	return 0, errors.New("unexpected read")
}
func (b unreadableBody) Close() error { return nil }
