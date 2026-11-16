package postgres

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"metapus/internal/application/readaccess"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/infrastructure/readpolicy"
	"metapus/internal/metadata"
)

type authorizedReadAdapter struct{ registry *metadata.Registry }

func NewAuthorizedReadService(reg *metadata.Registry, policies *security.PolicyEngine) *readaccess.Service {
	adapter := &authorizedReadAdapter{reg}
	return readaccess.New(adapter, adapter, policies)
}

// userPresentation is an explicit auth projection; credentials are never selected.
type userPresentation struct {
	ID        id.ID   `db:"id" json:"id"`
	FirstName *string `db:"first_name" json:"firstName"`
	LastName  *string `db:"last_name" json:"lastName"`
	Email     string  `db:"email" json:"email"`
}

func (u *userPresentation) GetID() id.ID { return u.ID }
func (a *authorizedReadAdapter) model(key string) (metadata.EntityDef, bool) {
	if key == "users" || key == "user" {
		return metadata.EntityDef{Key: "user", TableName: "auth_users", ReadModel: userPresentation{}, Fields: []metadata.FieldDef{
			{Name: "id", DBColumn: "id"}, {Name: "firstName", DBColumn: "first_name"},
			{Name: "lastName", DBColumn: "last_name"}, {Name: "email", DBColumn: "email"},
		}}, true
	}
	return a.registry.Get(key)
}
func (a *authorizedReadAdapter) Definition(key string) (readaccess.Definition, bool) {
	def, ok := a.model(key)
	if !ok {
		for _, candidate := range a.registry.List() {
			if candidate.TableName == key {
				def, ok = candidate, true
				break
			}
		}
	}
	if !ok || def.ReadModel == nil || def.TableName == "" {
		return readaccess.Definition{}, false
	}
	d := readaccess.Definition{Key: def.Key, Permission: def.ReadPermission, Table: def.TableName, Dimensions: def.RLSDimensions, MovementSources: def.MovementSources}
	t := reflect.TypeOf(def.ReadModel)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	d.SoftDelete = slices.Contains(extractColumnsFromType(t), "_deleted_at")
	if def.Key == "user" {
		d.RequiredRoles = []string{"admin"}
	}
	for _, f := range def.Fields {
		if f.DBColumn != "" {
			d.Fields = append(d.Fields, readaccess.Field{Name: f.Name, Column: f.DBColumn, WriteOnly: f.WriteOnly})
		}
	}
	return d, true
}

func (a *authorizedReadAdapter) Read(ctx context.Context, d readaccess.Definition, request readaccess.Query) ([]readaccess.Record, error) {
	def, ok := a.model(d.Key)
	if !ok {
		return nil, fmt.Errorf("read entity is no longer registered")
	}
	modelType := reflect.TypeOf(def.ReadModel)
	for modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
	}
	columns := extractColumnsFromType(modelType)
	q := squirrel.Select(columns...).From(d.Table).OrderBy("id ASC")
	if slices.Contains(columns, "_deleted_at") {
		q = q.Where(squirrel.Eq{"_deleted_at": nil})
	}
	if d.Key == "user" {
		q = q.Where(squirrel.Eq{"deletion_mark": false})
	}
	for _, condition := range readpolicy.ApplyConditions(request.Scope, d.Key, d.Dimensions) {
		q = q.Where(condition)
	}
	if len(request.IDs) > 0 {
		q = q.Where(squirrel.Eq{"id": request.IDs})
	}
	if !id.IsNil(request.After) {
		q = q.Where(squirrel.Gt{"id": request.After})
	}
	if len(request.SearchColumns) > 0 {
		pattern := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(request.Search) + "%"
		conditions := squirrel.Or{}
		for _, column := range request.SearchColumns {
			conditions = append(conditions, squirrel.ILike{column: pattern})
		}
		q = q.Where(conditions).Where(squirrel.Eq{"deletion_mark": false})
	}
	if request.Limit > 0 {
		q = q.Limit(request.Limit)
	}
	sql, args, err := q.PlaceholderFormat(squirrel.Dollar).ToSql()
	if err != nil {
		return nil, err
	}
	rows, err := MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	destination := reflect.New(reflect.SliceOf(reflect.PointerTo(modelType)))
	if err := pgxscan.ScanAll(destination.Interface(), rows); err != nil {
		return nil, err
	}
	values := destination.Elem()
	result := make([]readaccess.Record, 0, values.Len())
	for i := 0; i < values.Len(); i++ {
		entity := values.Index(i).Interface()
		identified, ok := entity.(interface{ GetID() id.ID })
		if !ok {
			return nil, fmt.Errorf("read model has no identity")
		}
		dbFields := StructToMap(entity)
		fields := make(map[string]any, len(d.Fields))
		dimensions := make(map[string]string, len(d.Dimensions))
		for _, field := range d.Fields {
			fields[field.Name] = readFieldValue(dbFields[field.Column])
		}
		for dimension, column := range d.Dimensions {
			value := readFieldValue(dbFields[column])
			if value == nil {
				dimensions[dimension] = ""
			} else {
				dimensions[dimension] = fmt.Sprint(value)
			}
		}
		result = append(result, readaccess.Record{ID: identified.GetID(), Entity: entity, Fields: fields, Dimensions: dimensions})
	}
	return result, nil
}

func readFieldValue(value any) any {
	if value == nil {
		return nil
	}
	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	return v.Interface()
}
