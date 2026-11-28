import { z } from "zod"
import { httpRoutes } from "@/lib/generated/http-contract-routes"
import type { HTTPRouteContract } from "@/lib/http-contract-types"
import type { JsonObject, JsonValue } from "@/lib/json-value"

type IndexedRoute = { contract: HTTPRouteContract; segments: string[] }

// Literal segments precede parameters regardless of declaration order.
function indexRoutes(contracts: readonly HTTPRouteContract[]): IndexedRoute[] {
  const parameters = (path: string) => path.split("/").filter(part => part.startsWith(":")).length
  return [...contracts].sort((left, right) => parameters(left.path) - parameters(right.path))
    .map(contract => ({ contract, segments: contract.path.split("/") }))
}

const registered = new Set<readonly HTTPRouteContract[]>([httpRoutes])
let routes = indexRoutes(httpRoutes)

const routeKey = (contract: HTTPRouteContract) => `${contract.scope} ${contract.method} ${contract.path}`

/**
 * Adds a configuration's generated route table (its own
 * configuration/<name>/lib/generated/http-contract-routes.ts). Registering the
 * same table again is a no-op; a route both tables declare is an error. A
 * route marked extends (an add-on's fields of a catalog of its base) is
 * merged into the registered route instead, which must exist: the base
 * registers before its add-on.
 */
export function registerHTTPRoutes(table: readonly HTTPRouteContract[]): void {
  if (registered.has(table)) return
  const known = new Map(routes.map(({ contract }) => [routeKey(contract), contract]))
  const duplicate = table.find(contract => !contract.extends && known.has(routeKey(contract)))
  if (duplicate) throw new Error(`HTTP contract ${duplicate.scope} ${duplicate.method} ${duplicate.path} is already registered`)
  const missing = table.find(contract => contract.extends && !known.has(routeKey(contract)))
  if (missing) throw new Error(`HTTP contract ${missing.scope} ${missing.method} ${missing.path} extends a route that is not registered`)
  registered.add(table)
  for (const extension of table.filter(contract => contract.extends)) {
    const key = routeKey(extension)
    known.set(key, extendContract(known.get(key)!, extension))
  }
  routes = indexRoutes([...known.values(), ...table.filter(contract => !contract.extends)])
}

/**
 * The route with an add-on's fields: the request takes the base's fields and
 * the add-on's, each part checked by its own schema; the response is both.
 */
function extendContract(base: HTTPRouteContract, extension: HTTPRouteContract): HTTPRouteContract {
  let request = base.request
  let requestFields = base.requestFields
  if (base.request && extension.request) {
    const own = new Set(extension.requestFields ?? [])
    const baseRequest = base.request
    const extensionRequest = extension.request
    request = z.record(z.string(), z.unknown()).superRefine((value, ctx) => {
      const parts: [z.ZodType, Record<string, unknown>][] = [
        [baseRequest, Object.fromEntries(Object.entries(value).filter(([name]) => !own.has(name)))],
        [extensionRequest, Object.fromEntries(Object.entries(value).filter(([name]) => own.has(name)))],
      ]
      for (const [schema, part] of parts) {
        const result = schema.safeParse(part)
        if (!result.success) {
          for (const issue of result.error.issues) ctx.addIssue({ code: "custom", message: issue.message, path: issue.path })
        }
      }
    })
    requestFields = [...(base.requestFields ?? []), ...(extension.requestFields ?? [])]
  }
  const response = base.response && extension.response ? z.intersection(base.response, extension.response) : base.response
  return { ...base, request, requestFields, response }
}

export function findHTTPContract(scope: string, method: string, path: string): HTTPRouteContract | undefined {
  const segments = path.split("?")[0].replace(/\/$/, "").split("/")
  return routes.find(({ contract, segments: expected }) => contract.scope === scope && contract.method === method &&
    expected.length === segments.length && expected.every((part, index) => part.startsWith(":") ? segments[index].length > 0 : part === segments[index]))?.contract
}

export function requireHTTPContract(scope: string, method: string, path: string): HTTPRouteContract {
  const contract = findHTTPContract(scope, method, path)
  if (!contract) throw new Error(`No HTTP contract for ${scope} ${method} ${path.split("?")[0]}`)
  return contract
}

export function validateHTTPBody(contract: HTTPRouteContract, body: RequestInit["body"]): void {
  if (contract.request) {
    if (contract.optionalRequest && body == null) return
    if (typeof body !== "string") throw new Error(`JSON body required for ${contract.method} ${contract.path}`)
    contract.request.parse(JSON.parse(body))
  } else if (body != null) {
    throw new Error(`Body is not supported for ${contract.method} ${contract.path}`)
  }
}

export function projectFormRequest(method: string, path: string, draft: JsonObject): JsonObject {
  const contract = findHTTPContract("api", method, path)
  if (!contract?.request || !contract.requestFields) throw new Error(`No form request contract for ${method} ${path}`)
  const payload = Object.fromEntries(contract.requestFields
    .filter(name => Object.prototype.hasOwnProperty.call(draft, name))
    .map(name => [name, projectRows(contract.requestParts?.[name], draft[name])]))
  contract.request.parse(payload)
  return payload
}

// A table part's rows carry response-only values (lineNo, reference displays,
// read-only columns); the request takes only the fields its line contract declares.
function projectRows(fields: readonly string[] | undefined, value: JsonValue): JsonValue {
  if (!fields || !Array.isArray(value)) return value
  return value.map(row => row !== null && typeof row === "object" && !Array.isArray(row)
    ? Object.fromEntries(fields.filter(name => row[name] !== undefined && row[name] !== null).map(name => [name, row[name]]))
    : row)
}

/** Fields the form request of method/path accepts; null when no contract is known. */
export function formRequestFieldNames(method: string, path: string): ReadonlySet<string> | null {
  const fields = findHTTPContract("api", method, path)?.requestFields
  return fields ? new Set(fields) : null
}

/**
 * Line fields the form request of method/path accepts for each table part;
 * a part without them is not writable through that request.
 */
export function formRequestParts(method: string, path: string): Readonly<Record<string, readonly string[]>> {
  return findHTTPContract("api", method, path)?.requestParts ?? {}
}
