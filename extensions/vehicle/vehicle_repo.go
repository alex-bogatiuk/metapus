package vehicle

import (
	"context"

	"github.com/Masterminds/squirrel"

	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

const vehicleTable = "cat_vehicles"

// VehicleRepo implements vehicle.Repository using BaseCatalogRepo.
// BaseCatalogRepo already provides GetByID, GetForUpdate, Create, Update,
// Delete, List, GetTree, etc. — we only add FindByPlateNumber.
type VehicleRepo struct {
	*catalog_repo.BaseCatalogRepo[*Vehicle]
}

// NewVehicleRepo creates a new vehicle repository.
func NewVehicleRepo() *VehicleRepo {
	return &VehicleRepo{
		BaseCatalogRepo: catalog_repo.NewBaseCatalogRepo[*Vehicle](
			vehicleTable,
			postgres.ExtractDBColumns[Vehicle](),
			func() *Vehicle { return &Vehicle{} },
			true, // hierarchical
		),
	}
}

// FindByPlateNumber retrieves vehicle by plate number.
func (r *VehicleRepo) FindByPlateNumber(ctx context.Context, plateNumber string) (*Vehicle, error) {
	q := r.Builder().
		Select(postgres.ExtractDBColumns[Vehicle]()...).
		From(vehicleTable).
		Where(squirrel.Eq{"plate_number": plateNumber}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Where("_deleted_at IS NULL").
		Limit(1)

	return r.FindOne(ctx, q)
}
