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

/**
 * Adds a configuration's generated route table (its own
 * configuration/<name>/lib/generated/http-contract-routes.ts). Registering the
 * same table again is a no-op; a route both tables declare is an error.
 */
export function registerHTTPRoutes(table: readonly HTTPRouteContract[]): void {
  if (registered.has(table)) return
  const known = new Set(routes.map(({ contract }) => `${contract.scope} ${contract.method} ${contract.path}`))
  const duplicate = table.find(contract => known.has(`${contract.scope} ${contract.method} ${contract.path}`))
  if (duplicate) throw new Error(`HTTP contract ${duplicate.scope} ${duplicate.method} ${duplicate.path} is already registered`)
  registered.add(table)
  routes = indexRoutes([...routes.map(({ contract }) => contract), ...table])
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
