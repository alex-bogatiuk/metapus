import { test, expect } from "@playwright/test"
import { stepSpring } from "../../lib/spring"

const run = (from: number, to: number, seconds: number, velocity = 0, response = 0.42) => {
  const path: number[] = []
  let state = { position: from, velocity }
  for (let t = 0; t < seconds; t += 1 / 60) {
    state = stepSpring(state.position, state.velocity, to, 1 / 60, response)
    path.push(state.position)
  }
  return { path, state }
}

test("a critically damped spring settles on its target without overshoot", () => {
  const { path, state } = run(0, 300, 1)
  expect(Math.max(...path)).toBeLessThanOrEqual(300)
  expect(path.every((value, i) => i === 0 || value >= path[i - 1])).toBe(true)
  expect(state.position).toBeCloseTo(300, 0)
})

test("retargeting mid-flight keeps position and velocity, so the motion has no jump", () => {
  const first = run(0, 300, 0.15).state
  const after = stepSpring(first.position, first.velocity, 100, 1 / 60, 0.42)
  // One frame later the button is still near where it was and still moving the old way.
  expect(Math.abs(after.position - first.position)).toBeLessThan(15)
  expect(Math.sign(after.velocity)).toBe(Math.sign(first.velocity))
  expect(run(first.position, 100, 1.5, first.velocity).state.position).toBeCloseTo(100, 0)
})

test("the step is independent of frame rate", () => {
  const at60 = run(0, 300, 0.25)
  let coarse = { position: 0, velocity: 0 }
  for (let i = 0; i < 8; i++) coarse = stepSpring(coarse.position, coarse.velocity, 300, 1 / 30, 0.42)
  expect(coarse.position).toBeCloseTo(at60.state.position, 0)
})
