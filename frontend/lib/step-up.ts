import { onScopeReset } from "@/lib/auth-scope"

export const STEP_UP_EVENT = "metapus:step-up-required"

export interface StepUpEventDetail {
  actionCode: string
  resolve: () => void
  reject: (error: Error) => void
}

export function requestStepUp(actionCode: string): Promise<void> {
  if (typeof window === "undefined") {
    return Promise.reject(new Error("step-up is only available in the browser"))
  }

  return new Promise<void>((resolve, reject) => {
    const dispose = onScopeReset(() => { dispose(); reject(new DOMException("Session scope changed", "AbortError")) })
    window.dispatchEvent(new CustomEvent<StepUpEventDetail>(STEP_UP_EVENT, {
      detail: { actionCode, resolve: () => { dispose(); resolve() }, reject: (error) => { dispose(); reject(error) } },
    }))
  })
}
