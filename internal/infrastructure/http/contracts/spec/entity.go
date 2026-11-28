// Package spec declares wire contracts without depending on the application
// registry or code generator. Entity modules can supply their own typed bundle.
package spec

import (
	"reflect"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/dto"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

type Direction uint8

const (
	Input Direction = iota
	Output
)

type Contract struct {
	Name      string
	Type      reflect.Type
	Direction Direction
}

func Request[T any](name string) Contract  { return Contract{name, reflect.TypeFor[T](), Input} }
func Response[T any](name string) Contract { return Contract{name, reflect.TypeFor[T](), Output} }

type RouteContract struct {
	Scope, Method, Path string
	Request, Response   string
	OptionalRequest     bool
	ResponseVariants    []string
	// Extends marks an add-on's part of a route of a lower layer (an
	// extension of a catalog): its request and response contracts are the
	// fields the add-on adds, which the frontend merges into the route's.
	Extends bool
}

type Bundle struct {
	Contracts []Contract
	Routes    []RouteContract
}
type Provider interface{ HTTPContracts() Bundle }

// Entity derives concrete DTO names and route contracts from one definition.
// Document capabilities must match those used to mount the actual handler.
func Entity[Create, Update, Read any](def entity.Definition, caps metadata.DocumentCapabilities) Bundle {
	return EntityWithDetail[Create, Update, Read, Read](def, caps)
}

// EntityWithDetail supports an explicit detail projection without changing the
// list DTO or granting additional mutation capabilities.
func EntityWithDetail[Create, Update, List, Read any](def entity.Definition, caps metadata.DocumentCapabilities) Bundle {
	return entityBundle(def, caps, reflect.TypeFor[Create](), reflect.TypeFor[Update](), reflect.TypeFor[List](), reflect.TypeFor[Read](),
		reflect.TypeFor[dto.TypedCursorListResponse[List]]())
}

// EntityOf is Entity for wire types built at run time, e.g. derived from the
// model by a declarative registration. They get the standard names
// Create<Name>Request, Update<Name>Request and <Name>Response; readName
// replaces the response name for an entity whose standard name is taken.
func EntityOf(def entity.Definition, caps metadata.DocumentCapabilities, create, update, read reflect.Type, readName string) Bundle {
	return entityBundleNamed(def, caps, create, update, read, read, cursorOf(read), readName)
}

// cursorOf mirrors dto.TypedCursorListResponse for an item type that only
// exists at run time.
func cursorOf(item reflect.Type) reflect.Type {
	template := reflect.TypeFor[dto.TypedCursorListResponse[struct{}]]()
	fields := make([]reflect.StructField, template.NumField())
	for i := range fields {
		fields[i] = template.Field(i)
		if fields[i].Name == "Items" {
			fields[i].Type = reflect.SliceOf(item)
		}
	}
	return reflect.StructOf(fields)
}

func entityBundle(def entity.Definition, caps metadata.DocumentCapabilities, createType, updateType, listType, readType, cursorType reflect.Type) Bundle {
	return entityBundleNamed(def, caps, createType, updateType, listType, readType, cursorType, "")
}

func entityBundleNamed(def entity.Definition, caps metadata.DocumentCapabilities, createType, updateType, listType, readType, cursorType reflect.Type, readName string) Bundle {
	if err := def.Validate(); err != nil {
		panic(err)
	}
	name := def.Name
	create, update := wireNameOf(createType, "Create"+name+"Request"), wireNameOf(updateType, "Update"+name+"Request")
	read, list := wireNameOf(readType, name+"Response"), wireNameOf(listType, name+"Response")
	if readName != "" {
		read, list = readName, readName
	}
	cursor := strings.TrimSuffix(list, "Response") + "CursorResponse"
	tree := strings.TrimSuffix(list, "Response") + "TreeResponse"
	b := Bundle{Contracts: []Contract{{create, createType, Input}, {update, updateType, Input}, {read, readType, Output}, {cursor, cursorType, Output}}}
	if read != list || readType != listType {
		b.Contracts = append(b.Contracts, Contract{list, listType, Output})
	}
	base := "/" + string(def.Kind) + "/" + def.Route
	add := func(enabled bool, method, suffix, request, response string) {
		if enabled {
			b.Routes = append(b.Routes, RouteContract{Scope: "api", Method: method, Path: base + suffix, Request: request, Response: response})
		}
	}
	if def.Kind == entity.CatalogKind {
		caps = metadata.DocumentCapabilities{List: true, Read: true, Create: true, Update: true, Delete: true, SetDeletionMark: true, ExportList: true}
		b.Contracts = append(b.Contracts, Contract{tree, reflect.SliceOf(readType), Output})
		add(true, "GET", "/tree", "", tree)
	}
	add(caps.List, "GET", "", "", cursor)
	add(caps.Read, "GET", "/:id", "", read)
	add(caps.Create, "POST", "", create, read)
	add(caps.Update, "PUT", "/:id", update, read)
	add(caps.Delete, "DELETE", "/:id", "", "")
	mark := read
	if def.Kind == entity.CatalogKind {
		mark = "SuccessResponse"
	}
	add(caps.SetDeletionMark, "POST", "/:id/deletion-mark", "SetDeletionMarkRequest", mark)
	add(caps.ExportList, "POST", "/export-list", "ExportListRequest", "")
	add(caps.Post, "POST", "/:id/post", "", read)
	add(caps.Unpost, "POST", "/:id/unpost", "", read)
	add(caps.Repost, "PUT", "/:id/repost", update, read)
	add(caps.Movements, "GET", "/:id/movements", "", "DocumentMovementsResponse")
	add(caps.RelatedDocuments, "GET", "/:id/related-documents", "", "RelatedDocumentsResponse")
	add(caps.PrintForms, "GET", "/print-forms", "", "PrintFormsResponse")
	add(caps.PrintForms, "GET", "/:id/print", "", "")
	add(caps.BatchAction, "POST", "/batch-action", "BatchActionRequest", "BatchActionResponse")
	add(caps.BatchActionByFilter, "POST", "/batch-action-by-filter", "BatchActionByFilterRequest", "BatchActionResponse")
	return b
}

func wireNameOf(t reflect.Type, fallback string) string {
	name := t.Name()
	if name == "" || name == strings.TrimSuffix(fallback, "Response") {
		return fallback
	}
	return name
}

// AddRoute keeps custom workflow paths relative to the same entity identity.
func (b *Bundle) AddRoute(def entity.Definition, method, suffix, request, response string) {
	b.Routes = append(b.Routes, RouteContract{Scope: "api", Method: method, Path: "/" + string(def.Kind) + "/" + def.Route + suffix, Request: request, Response: response})
}
