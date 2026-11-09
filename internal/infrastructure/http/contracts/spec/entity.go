// Package spec declares wire contracts without depending on the application
// registry or code generator. Entity modules can supply their own typed bundle.
package spec

import (
	"reflect"
	"strings"

	"metapus/internal/core/entity"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/metadata"
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
	if err := def.Validate(); err != nil {
		panic(err)
	}
	name := def.Name
	create, update := wireName[Create]("Create"+name+"Request"), wireName[Update]("Update"+name+"Request")
	read, list := wireName[Read](name+"Response"), wireName[List](name+"Response")
	cursor := strings.TrimSuffix(list, "Response") + "CursorResponse"
	tree := strings.TrimSuffix(list, "Response") + "TreeResponse"
	b := Bundle{Contracts: []Contract{Request[Create](create), Request[Update](update), Response[Read](read), Response[dto.TypedCursorListResponse[List]](cursor)}}
	if read != list || reflect.TypeFor[Read]() != reflect.TypeFor[List]() {
		b.Contracts = append(b.Contracts, Response[List](list))
	}
	base := "/" + string(def.Kind) + "/" + def.Route
	add := func(enabled bool, method, suffix, request, response string) {
		if enabled {
			b.Routes = append(b.Routes, RouteContract{Scope: "api", Method: method, Path: base + suffix, Request: request, Response: response})
		}
	}
	if def.Kind == entity.CatalogKind {
		caps = metadata.DocumentCapabilities{List: true, Read: true, Create: true, Update: true, Delete: true, SetDeletionMark: true, ExportList: true}
		b.Contracts = append(b.Contracts, Response[[]Read](tree))
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

func wireName[T any](fallback string) string {
	name := reflect.TypeFor[T]().Name()
	if name == "" || name == strings.TrimSuffix(fallback, "Response") {
		return fallback
	}
	return name
}

// AddRoute keeps custom workflow paths relative to the same entity identity.
func (b *Bundle) AddRoute(def entity.Definition, method, suffix, request, response string) {
	b.Routes = append(b.Routes, RouteContract{Scope: "api", Method: method, Path: "/" + string(def.Kind) + "/" + def.Route + suffix, Request: request, Response: response})
}
