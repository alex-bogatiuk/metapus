package contracts

import (
	"bytes"
	"fmt"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/contracts/spec"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// RouteContract binds a method and path to concrete wire types. Paths are server
// route templates, independent of URL parameters and of authorization policy.
type RouteContract = spec.RouteContract

// Routes lists the platform route contracts followed by configuration bundles.
func Routes(bundles ...spec.Bundle) []RouteContract {
	routes := []RouteContract{
		{Scope: "api", Method: "GET", Path: "/system/integrations/providers", Response: "IntegrationProvidersResponse"},
		{Scope: "api", Method: "GET", Path: "/system/integrations/pools/:id/status", Response: "IntegrationPoolStatusResponse"},
		{Scope: "api", Method: "POST", Path: "/system/integrations/pools/:id/command", Request: "IntegrationPoolCommandRequest", Response: "IntegrationPoolStatusResponse"},
		{Scope: "api", Method: "POST", Path: "/system/integrations/pools/:id/test", Response: "IntegrationConnectionTestResponse"},
	}
	return slices.Concat(routes, automationRoutes(), systemRoutes(), securityRoutes(), serviceOperationRoutes(), bundleRoutes(bundles))
}

// routeScopePattern names the API surface a route belongs to: "api" for the
// platform, others (for example "portal") for configuration surfaces.
var routeScopePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func GenerateRoutes(contracts []Contract, routes []RouteContract) ([]byte, error) {
	return generateRoutes(nil, Modules{}, contracts, routes)
}

// GenerateRoutesOn writes a configuration's route table. Its routes may use
// the configuration's own contracts and those of the base layer.
func GenerateRoutesOn(base *Layer, modules Modules, contracts []Contract, routes []RouteContract) ([]byte, error) {
	return generateRoutes(base, modules, contracts, routes)
}

// BundleRoutes lists only the routes the bundles declare, without the
// platform's own routes.
func BundleRoutes(bundles ...spec.Bundle) []RouteContract { return bundleRoutes(bundles) }

func generateRoutes(base *Layer, modules Modules, contracts []Contract, routes []RouteContract) ([]byte, error) {
	types := make(map[string]Direction, len(contracts))
	shapes := make(map[string]reflect.Type, len(contracts))
	for _, contract := range contracts {
		if base != nil {
			// The base layer declares this name; the configuration file does not.
			if _, declared := base.used[contract.Name]; declared {
				continue
			}
		}
		types[contract.Name] = contract.Direction
		shapes[contract.Name] = contract.Type
	}
	seen := make(map[string]bool, len(routes))
	var output bytes.Buffer
	var table bytes.Buffer
	for _, route := range routes {
		key := route.Scope + " " + route.Method + " " + route.Path
		if seen[key] {
			return nil, fmt.Errorf("duplicate HTTP route contract %s", key)
		}
		seen[key] = true
		if !routeScopePattern.MatchString(route.Scope) || !strings.HasPrefix(route.Path, "/") {
			return nil, fmt.Errorf("invalid HTTP route contract %s", key)
		}
		switch route.Method {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return nil, fmt.Errorf("invalid HTTP method %s", route.Method)
		}
		schema := func(name string, direction Direction) (string, error) {
			if name == "" {
				return "null", nil
			}
			if actual, ok := types[name]; ok && actual == direction {
				return "Wire." + name + "Schema", nil
			}
			if base != nil {
				if actual, ok := base.roots[name]; ok && actual == direction {
					return "Platform." + name + "Schema", nil
				}
			}
			return "", fmt.Errorf("route %s: missing %v contract %s", key, direction, name)
		}
		request, err := schema(route.Request, Input)
		if err != nil {
			return nil, err
		}
		response, err := schema(route.Response, Output)
		if err != nil {
			return nil, err
		}
		if len(route.ResponseVariants) != 0 {
			if route.Response != "" || len(route.ResponseVariants) < 2 {
				return nil, fmt.Errorf("route %s: response variants require two or more concrete alternatives", key)
			}
			alternatives := make([]string, 0, len(route.ResponseVariants))
			for _, name := range route.ResponseVariants {
				validator, err := schema(name, Output)
				if err != nil || name == "" {
					return nil, fmt.Errorf("route %s: invalid response alternative %q", key, name)
				}
				alternatives = append(alternatives, validator)
			}
			response = "z.union([" + strings.Join(alternatives, ", ") + "])"
		}
		fields, parts := "null", ""
		if route.Request != "" {
			requestType, own := shapes[route.Request]
			if !own && base != nil {
				requestType = base.shapes[route.Request]
			}
			names := requestFieldNames(requestType)
			if names != nil {
				fields = quotedList(names)
			}
			if rows := requestRowFields(requestType); len(rows) != 0 {
				entries := make([]string, 0, len(rows))
				for _, row := range rows {
					entries = append(entries, strconv.Quote(row.name)+": "+quotedList(row.fields))
				}
				parts = ", requestParts: { " + strings.Join(entries, ", ") + " }"
			}
		}
		fmt.Fprintf(&table, "  { scope: %s, method: %s, path: %s, request: %s, requestFields: %s%s, response: %s, optionalRequest: %t },\n", strconv.Quote(route.Scope), strconv.Quote(route.Method), strconv.Quote(route.Path), request, fields, parts, response, route.OptionalRequest)
	}
	output.WriteString("// Code generated by go run ./cmd/http-contracts. DO NOT EDIT.\n")
	if base == nil {
		output.WriteString("import { z } from \"zod\"\nimport * as Wire from \"./http-contract-schemas\"\nimport type { HTTPRouteContract } from \"@/lib/http-contract-types\"\n\n")
	} else {
		body := table.String()
		if strings.Contains(body, "z.union(") {
			output.WriteString("import { z } from \"zod\"\n")
		}
		if strings.Contains(body, "Wire.") {
			output.WriteString("import * as Wire from \"" + modules.Schemas + "\"\n")
		}
		if strings.Contains(body, "Platform.") {
			output.WriteString("import * as Platform from \"" + base.modules.Schemas + "\"\n")
		}
		output.WriteString("import type { HTTPRouteContract } from \"@/lib/http-contract-types\"\n\n")
	}
	output.WriteString("export const httpRoutes = [\n")
	output.Write(table.Bytes())
	output.WriteString("] as const satisfies readonly HTTPRouteContract[]\n")
	return output.Bytes(), nil
}

// Request DTOs own writable names. Form projection must not infer these from
// display metadata or copy server-owned properties from an entity response.
func quotedList(names []string) string {
	quoted := make([]string, len(names))
	for index, name := range names {
		quoted[index] = strconv.Quote(name)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

type requestRows struct {
	name   string
	fields []string
}

// requestRowFields lists, for each request field that is an array of objects
// (a document table part), the fields one element accepts, so a form can send
// rows without the response-only fields it displays.
func requestRowFields(t reflect.Type) []requestRows {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || hasCustomCodec(t) {
		return nil
	}
	var rows []requestRows
	for field := range t.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if !field.IsExported() || field.Anonymous || name == "" || name == "-" || field.Type.Kind() != reflect.Slice {
			continue
		}
		if names := requestFieldNames(field.Type.Elem()); len(names) != 0 {
			rows = append(rows, requestRows{name: name, fields: names})
		}
	}
	return rows
}

func requestFieldNames(t reflect.Type) []string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || hasCustomCodec(t) {
		return nil
	}
	names := []string{}
	for field := range t.Fields() {
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if field.Anonymous && name == "" {
			names = append(names, requestFieldNames(field.Type)...)
			continue
		}
		if name == "" {
			name = field.Name
		}
		names = append(names, name)
	}
	return names
}
