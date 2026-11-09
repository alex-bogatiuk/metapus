package doc_probe

import (
	"context"
	"metapus/internal/core/entity"
)

// DocProbe stores the business data. Add typed fields and pure validation here.
type DocProbe struct {
	entity.Document
}

func NewDocProbe() *DocProbe                { return &DocProbe{Document: entity.NewDocument()} }
func (e *DocProbe) GetDocumentType() string { return Definition.Name }

// Override CanPost and implement the required MovementSource interfaces when
// this document affects accounting. Generate movements without SQL or RPC.
func (e *DocProbe) CanPost(ctx context.Context) error {
	if err := e.Document.CanPost(ctx); err != nil {
		return err
	}
	return e.Validate(ctx)
}

func (e *DocProbe) Validate(ctx context.Context) error { return e.Document.Validate(ctx) }

func (DocProbe) EntityDefinition() entity.Definition { return Definition }
