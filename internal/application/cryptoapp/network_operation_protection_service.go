package cryptoapp

import (
	"context"
	"fmt"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tx"
	"metapus/internal/domain/documents/crypto_network_operation"
	"metapus/internal/domain/eventlog"
)

type CryptoNetworkOperationEditor interface {
	AllowEditing(context.Context, id.ID) (*crypto_network_operation.CryptoNetworkOperation, error)
	DenyEditing(context.Context, id.ID) (*crypto_network_operation.CryptoNetworkOperation, error)
}

// NetworkOperationProtectionService owns the atomic protection transition and
// its security audit event. HTTP is only responsible for authorization and DTOs.
type NetworkOperationProtectionService struct {
	txm    tx.Manager
	editor CryptoNetworkOperationEditor
	events eventlog.Writer
}

func NewNetworkOperationProtectionService(txm tx.Manager, editor CryptoNetworkOperationEditor, events eventlog.Writer) *NetworkOperationProtectionService {
	if txm == nil || editor == nil {
		panic("network operation protection requires transaction manager and editor")
	}
	return &NetworkOperationProtectionService{txm: txm, editor: editor, events: events}
}

func (s *NetworkOperationProtectionService) SetEditingAllowed(ctx context.Context, documentID id.ID, allowed bool) (*crypto_network_operation.CryptoNetworkOperation, error) {
	if id.IsNil(documentID) {
		return nil, fmt.Errorf("network operation document id is required")
	}
	var document *crypto_network_operation.CryptoNetworkOperation
	err := s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		var err error
		if allowed {
			document, err = s.editor.AllowEditing(txCtx, documentID)
		} else {
			document, err = s.editor.DenyEditing(txCtx, documentID)
		}
		if err != nil {
			return err
		}
		return s.writeEvent(txCtx, document, allowed)
	})
	return document, err
}

func (s *NetworkOperationProtectionService) writeEvent(ctx context.Context, document *crypto_network_operation.CryptoNetworkOperation, allowed bool) error {
	if s.events == nil {
		return nil
	}
	command := "deny_editing"
	message := "Editing of the blockchain operation was denied"
	if allowed {
		command = "allow_editing"
		message = "Editing of the blockchain operation was allowed"
	}
	event := eventlog.Event{
		Category: eventlog.CategoryData, Severity: eventlog.SeverityWarning,
		EventType: eventlog.EventDocumentUpdate, Source: "api",
		EntityType: crypto_network_operation.DocumentType, EntityID: &document.ID,
		EntityNumber: document.Number, Message: message, Details: map[string]any{"command": command},
	}
	if user := appctx.GetUser(ctx); user != nil {
		event.UserID, event.SessionID = user.UserID, user.SessionID
	}
	return s.events.Write(ctx, event)
}
