package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// WSTicketStore manages short-lived, single-use tickets for WebSocket authentication.
// This replaces passing JWT tokens in URL query parameters, which leak into
// server access logs, browser history, and Referer headers.
//
// Flow:
//  1. Client calls POST /auth/ws-ticket (authenticated via JWT header) → receives ticket
//  2. Client opens WebSocket with ?ticket=<ticket> (no JWT in URL)
//  3. Server validates and consumes the ticket (single-use)
//
// Tickets expire after 30 seconds to limit the attack window.
type WSTicketStore struct {
	mu      sync.Mutex
	tickets map[string]*wsTicket
	stopCh  chan struct{}
}

type wsTicket struct {
	UserID   string
	TenantID string
	ExpiresAt time.Time
}

// NewWSTicketStore creates a new ticket store with background cleanup.
func NewWSTicketStore() *WSTicketStore {
	s := &WSTicketStore{
		tickets: make(map[string]*wsTicket),
		stopCh:  make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

// Stop terminates the background cleanup goroutine.
func (s *WSTicketStore) Stop() {
	close(s.stopCh)
}

// IssueTicket creates a single-use ticket valid for 30 seconds.
func (s *WSTicketStore) IssueTicket(userID, tenantID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	ticket := hex.EncodeToString(raw)

	s.mu.Lock()
	s.tickets[ticket] = &wsTicket{
		UserID:    userID,
		TenantID:  tenantID,
		ExpiresAt: time.Now().Add(30 * time.Second),
	}
	s.mu.Unlock()

	return ticket, nil
}

// ValidateTicket validates and consumes a ticket. Returns userID and tenantID.
// The ticket is deleted after validation (single-use).
func (s *WSTicketStore) ValidateTicket(ticket string) (userID, tenantID string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, exists := s.tickets[ticket]
	if !exists {
		return "", "", false
	}

	// Always delete — single use
	delete(s.tickets, ticket)

	if time.Now().After(t.ExpiresAt) {
		return "", "", false
	}

	return t.UserID, t.TenantID, true
}

func (s *WSTicketStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.evictExpired()
		}
	}
}

func (s *WSTicketStore) evictExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for k, t := range s.tickets {
		if now.After(t.ExpiresAt) {
			delete(s.tickets, k)
		}
	}
}
