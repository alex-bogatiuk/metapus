import { test, expect } from "@playwright/test"
import { createElement, lazy } from "react"
import { renderToPipeableStream } from "react-dom/server"
import { Writable } from "node:stream"
import { Wallet } from "lucide-react"
import { defineWidget } from "../../lib/widget-registry"
import type { WidgetRenderProps } from "../../types/dashboard"
import type { JsonObject } from "../../lib/json-value"

test("widget registry validates configs before rendering and before publishing changes", async () => {
  let received: WidgetRenderProps<"attention-counter"> | undefined
  const definition = defineWidget({
    type: "attention-counter", label: "KPI", description: "", icon: Wallet, allowedSizes: ["2x1"], defaultSize: "2x1",
    category: "kpi", requiredPermission: null, defaultConfig: { metric: "withdrawals" },
    component: lazy(async () => ({ default: (props: WidgetRenderProps<"attention-counter">) => {
      received = props
      return createElement("span", null, props.config.metric)
    } })),
  })
  const changes: JsonObject[] = []
  for (const [config, metric] of [[{ metric: "inbound" }, "inbound"], [{ metric: 12 }, "withdrawals"]] as const) {
    const element = createElement(definition.component, {
      placement: { instanceId: "test", widgetType: "attention-counter", w: 2, h: 1, config: {}, x: 0, y: 0 },
      config, isEditMode: false, onConfigChange: next => changes.push(next),
    })
    const html = await new Promise<string>((resolve, reject) => {
      let output = ""
      const destination = new Writable({ write(chunk: Buffer, _encoding, done) { output += chunk.toString(); done() } })
      destination.on("finish", () => resolve(output))
      destination.on("error", reject)
      const stream = renderToPipeableStream(element, { onAllReady() { stream.pipe(destination) }, onError: reject })
    })
    expect(html).toContain(metric)
    expect(received?.config.metric).toBe(metric)
    received?.onConfigChange({ metric: "outflows" })
  }
  expect(changes).toEqual([{ metric: "outflows" }, { metric: "outflows" }])
})
