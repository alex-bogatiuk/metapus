# HTTP contracts

Go transport DTOs are the source of TypeScript declarations, Zod schemas and the
route registry. Domain persistence and signed-protocol codecs are independent.

When adding or changing an endpoint:

1. Declare its request and response types. Move closed inline handler payloads to
   `v1/dto`; genuinely dynamic JSON remains `json.RawMessage`/`JsonValue`.
2. Add named `Request[T]`/`Response[T]` entries and bind the HTTP method, API scope
   and server route template to those names. Empty responses have no response
   schema. A union must list concrete response variants. Optional request bodies
   must be explicit (for example, refresh by httpOnly cookie).
3. Run `go run ./cmd/http-contracts`. Do not edit the generated files: the
   platform's three (`frontend/types/generated`, `frontend/lib/generated`) and
   each configuration's three under `frontend/configuration/<name>/`. A
   configuration's contracts belong to its entities or its `RouteContracts`
   (`configuration.OwnHTTPContractBundles`);
   its files import platform types instead of declaring them again.
4. Use the generated types in consumers. Add local schemas for frontend-owned
   state or domain refinements; validate before publishing to a store. Normalize
   omitted fields explicitly rather than asserting a different type.
5. Run `go run ./cmd/http-contracts -check`, the contract tests and frontend checks.
   Route registration tests should include the handler's actual Gin registration.

`apiFetch` and the clients from `createScopedClient` reject unregistered routes; a
configuration registers its generated route table with `registerHTTPRoutes`. They validate request JSON
before network access and response JSON before it reaches consumers. Binary and
SSE calls validate request DTOs too; stream events have their own runtime schema.
Literal route segments take precedence over parameters. Route authorization still
belongs to the backend application services.

AutoForm projects writable root fields from the concrete request DTO, then checks
the result. Display metadata cannot authorize additional fields or replace a DTO.

Presence checks use the active HTTP JSON facade, including `omitempty`, `omitzero`
and custom codecs. Required false/zero, omitted values and explicit null are
distinct. ERP monetary values are canonical int64 minor-unit strings over HTTP;
crypto amount strings retain their separate arbitrary-precision contract. Neither
contract permits a float conversion as a transport shortcut.
