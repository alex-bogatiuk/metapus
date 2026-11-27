package v1

import (
	"slices"
	"sort"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/domain/auth"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
	"github.com/alex-bogatiuk/metapus/internal/platform"
)

// permissionActionLabels names the standard actions in the role editor.
var permissionActionLabels = map[string]string{
	"read":   "чтение",
	"create": "создание",
	"update": "изменение",
	"delete": "удаление",
	"post":   "проведение",
	"unpost": "отмена проведения",
}

// Permissions lists every permission the registered entities and reports
// require: the standard actions of each catalog (read, create, update,
// delete), of each document as far as its capabilities expose them (read,
// create, update, delete, post, unpost), the permissions of declared document
// actions and of status transitions, and the read permission of each dataset.
//
// The list is derived from the same registry the router mounts, so adding an
// entity to the registry is enough for its permissions to exist: migrations
// sync it into auth_permissions and grant the new codes to the admin role (see
// migration.WithPermissions). Codes are "<kind>:<entity key>:<action>", the
// resource is the entity key, so the role editor labels it from metadata.
// Permissions checked by hand-written routes outside the standard surface
// (route modules, subresources) are not derivable: the platform's system
// administration lists its own (auth.SystemPermissions), the others stay in
// migrations.
func (r *FactoryRegistry) Permissions() []auth.Permission {
	set := permissionSet{}
	for _, factory := range r.catalogs {
		label := permissionEntityLabel(factory)
		for _, action := range []string{"read", "create", "update", "delete"} {
			set.add(factory.Permission()+":"+action, label, "")
		}
	}
	for _, factory := range r.documents {
		caps := metadata.DocumentCapabilities{List: true, Read: true, Create: true, Update: true, Delete: true, SetDeletionMark: true, Post: true, Unpost: true}
		if provider, ok := factory.(DocumentCapabilitiesProvider); ok {
			caps = provider.DocumentCapabilities()
		}
		label := permissionEntityLabel(factory)
		for _, action := range documentPermissionActions(caps) {
			set.add(factory.Permission()+":"+action, label, "")
		}
		set.addActions(factory, label)
		set.addStatusTransitions(factory.EntityDefinition(), label)
	}
	for _, factory := range r.customDocuments {
		label := permissionEntityLabel(factory)
		for _, action := range documentPermissionActions(documentCapabilitiesForCustomRegistration(factory)) {
			set.add(factory.Permission()+":"+action, label, "")
		}
		set.addActions(factory, label)
		set.addStatusTransitions(factory.EntityDefinition(), label)
	}
	for _, ds := range r.datasets {
		if ds.Permission != "" {
			set.add(ds.Permission, ds.Name, "")
		}
	}
	return set.list()
}

// documentPermissionActions mirrors RegisterDocumentRoutes: the actions its
// mounted routes require for the given capabilities.
func documentPermissionActions(caps metadata.DocumentCapabilities) []string {
	var actions []string
	add := func(enabled bool, action string) {
		if enabled && !slices.Contains(actions, action) {
			actions = append(actions, action)
		}
	}
	add(caps.List || caps.Read || caps.PrintForms || caps.RelatedDocuments || caps.Movements || caps.ExportList, "read")
	add(caps.Create || caps.Copy, "create")
	add(caps.Update, "update")
	add(caps.Delete || caps.SetDeletionMark, "delete")
	add(caps.Post || caps.Repost || caps.BatchAction || caps.BatchActionByFilter, "post")
	add(caps.Unpost, "unpost")
	return actions
}

func permissionEntityLabel(factory entityIdentity) string {
	if presentable, ok := factory.(platform.Presentable); ok {
		if plural := presentable.EntityPresentation().Plural; plural != "" {
			return plural
		}
	}
	if labeled, ok := factory.(platform.Labeled); ok {
		if label := labeled.EntityLabel(); label != "" {
			return label
		}
	}
	return factory.EntityDefinition().Name
}

type permissionSet map[string]auth.Permission

func (s permissionSet) addActions(factory documentMetadataRegistration, label string) {
	for _, action := range documentActions(factory) {
		if action.Permission != "" {
			s.add(action.Permission, label, strings.ToLower(action.Label))
		}
	}
}

// addStatusTransitions adds the permissions the transitions of a document's
// status model need (entity.StatusTransition.Permission).
func (s permissionSet) addStatusTransitions(def entity.Definition, label string) {
	for _, t := range def.Statuses.Permissions() {
		actionLabel := t.Label
		if actionLabel == "" {
			actionLabel = t.Permission
		}
		s.add(domain.StatusPermission(def, t.Permission), label, strings.ToLower(actionLabel))
	}
}

// add records a code once; the first label wins. A standard action keeps its
// standard name even when a custom document action shares its permission.
func (s permissionSet) add(code, label, actionLabel string) {
	if _, ok := s[code]; ok {
		return
	}
	resource, action := PermissionResourceAction(code)
	if standard, ok := permissionActionLabels[action]; ok {
		actionLabel = standard
	}
	if actionLabel == "" {
		actionLabel = action
	}
	s[code] = auth.Permission{Code: code, Name: label + ": " + actionLabel, Resource: resource, Action: action}
}

func (s permissionSet) list() []auth.Permission {
	result := make([]auth.Permission, 0, len(s))
	for _, permission := range s {
		result = append(result, permission)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Code < result[j].Code })
	return result
}

// PermissionResourceAction splits a permission code into the resource the
// role editor groups it under and its action. "catalog:currency:read" is
// ("currency", "read"), "crypto_receipt:approve" is ("crypto_receipt",
// "approve"); a report permission is grouped as "report_<key>", e.g.
// "report:document-journal:read" is ("report_document_journal", "read").
func PermissionResourceAction(code string) (resource, action string) {
	parts := strings.Split(code, ":")
	if len(parts) < 2 {
		return code, ""
	}
	action = parts[len(parts)-1]
	resource = parts[len(parts)-2]
	if len(parts) >= 3 && parts[0] == "report" {
		resource = "report_" + resource
	}
	return strings.ReplaceAll(resource, "-", "_"), action
}
