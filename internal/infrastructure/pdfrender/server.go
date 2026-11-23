package pdfrender

import (
	"io"
	"net/http"
	"sync/atomic"

	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/domain/printing"
)

type Server struct {
	renderer printing.PDFRenderer
	ready    atomic.Bool
	slots    chan struct{}
}

func NewServer(renderer printing.PDFRenderer) *Server {
	return &Server{renderer: renderer, slots: make(chan struct{}, 2)}
}
func (s *Server) SetReady(ready bool) { s.ready.Store(ready) }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health/live" && r.Method == http.MethodGet {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.URL.Path == "/health/ready" && r.Method == http.MethodGet {
		if !s.ready.Load() {
			http.Error(w, "renderer unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.URL.Path != "/render" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if !s.ready.Load() {
		http.Error(w, "renderer unavailable", http.StatusServiceUnavailable)
		return
	}
	// JSON escaping can expand HTML by up to six bytes per input byte.
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		http.Error(w, "renderer busy", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 6*MaxHTML+1024)
	defer func() { _ = r.Body.Close() }()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	var request Request
	if err := json.UnmarshalStrictWithOptions(body, &request); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if _, err := prepareHTML([]byte(request.HTML), request.Options); err != nil {
		http.Error(w, "invalid print content", http.StatusBadRequest)
		return
	}
	pdf, err := s.renderer.Render(r.Context(), []byte(request.HTML), request.Options)
	if err != nil {
		http.Error(w, "renderer unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(pdf)
}
