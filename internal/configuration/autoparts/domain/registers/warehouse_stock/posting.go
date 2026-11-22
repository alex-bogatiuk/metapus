package warehouse_stock

import (
	"context"
	"fmt"

	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/posting"
)

// MovementSource is implemented by documents that move warehouse stock.
type MovementSource interface {
	GenerateWarehouseStockMovements(ctx context.Context) ([]Movement, error)
}

// Visitor collects the register's movements from posted documents.
type Visitor struct{}

func (Visitor) Name() string { return Name }

func (Visitor) CollectMovements(ctx context.Context, doc posting.Postable, set *posting.MovementSet) error {
	source, ok := doc.(MovementSource)
	if !ok {
		return nil
	}
	movements, err := source.GenerateWarehouseStockMovements(ctx)
	if err != nil {
		return fmt.Errorf("generate warehouse stock movements: %w", err)
	}
	if len(movements) > 0 {
		set.SetExtension(Name, movements)
	}
	return nil
}

// Recorder writes the collected movements through the register service.
type Recorder struct{ service *Service }

// NewRecorder creates the register's posting recorder.
func NewRecorder(service *Service) *Recorder { return &Recorder{service: service} }

func (r *Recorder) Name() string { return Name }

func (r *Recorder) RecordFromSet(ctx context.Context, set *posting.MovementSet) error {
	raw, ok := set.GetExtension(Name)
	if !ok {
		return nil
	}
	movements, ok := raw.([]Movement)
	if !ok {
		return fmt.Errorf("warehouse stock movements have type %T", raw)
	}
	return r.service.RecordMovements(ctx, movements)
}

func (r *Recorder) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return r.service.ReverseMovements(ctx, recorderID, beforeVersion)
}

var (
	_ posting.RegisterVisitor  = Visitor{}
	_ posting.RegisterRecorder = (*Recorder)(nil)
)
