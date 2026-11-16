import { httpRoutes } from "@/lib/generated/http-contract-routes"
import type { HTTPRouteContract } from "@/lib/http-contract-types"
import type { JsonObject } from "@/lib/json-value"

// Literal segments precede parameters regardless of declaration order.
const routes = [...httpRoutes].sort((left, right) => {
  const parameters = (path: string) => path.split("/").filter(part => part.startsWith(":")).length
  return parameters(left.path) - parameters(right.path)
}).map(contract => ({ contract, segments: contract.path.split("/") }))

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
    .map(name => [name, draft[name]]))
  contract.request.parse(payload)
  return payload
}
