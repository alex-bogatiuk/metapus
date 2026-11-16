import { expect, test } from "@playwright/test"
import { moveRPCConnection, rpcConfigurationChanges, rpcCoverage, rpcReasonLabel, type RPCConnection } from "../../configuration/crypto/lib/integrations/rpc-configuration"

const connection = (id: string, role: string, priority: number, group = id): RPCConnection => ({ id, version: 1, name: id, role, providerId: id, trustGroup: group, priority, minRequestIntervalMs: 0, isActive: true })

test("RPC ordering stays inside a role and preserves inactive records", () => {
  const first = connection("a", "rpc", 10)
  const second = { ...connection("b", "rpc", 20), isActive: false }
  const tron = connection("c", "solidity", 7)
  const moved = moveRPCConnection([first, second, tron], "b", -1)
  expect(moved.filter(row => row.role === "rpc").map(row => row.id)).toEqual(["b", "a"])
  expect(moved.find(row => row.id === "b")?.isActive).toBe(false)
  expect(moved.find(row => row.id === "c")).toEqual(tron)
  expect(rpcConfigurationChanges([first, second, tron], moved)).toHaveLength(2)
})

test("two accounts of one operator do not provide two independent votes", () => {
  const rows = [connection("tenderly-a", "rpc", 10, "tenderly"), connection("tenderly-b", "rpc", 20, "tenderly"), connection("other", "rpc", 30)]
  const requirement = [{ role: "rpc", requiredProviders: 2, configuredProviders: 2 }]
  expect(rpcCoverage(rows, requirement)[0].configuredProviders).toBe(2)
  expect(rpcCoverage(rows.map(row => ({ ...row, isActive: row.id !== "other" })), requirement)[0].configuredProviders).toBe(1)
})

test("diagnostics display only descriptions of known categories", () => {
  expect(rpcReasonLabel("archive_unavailable")).toContain("Архивные")
  expect(rpcReasonLabel("https://provider.example/private-token")).toBe("Причина не определена")
})
