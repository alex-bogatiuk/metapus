/**
 * Advances a critically damped spring (damping ratio 1: no overshoot) by `dt` seconds using the
 * exact solution, so retargeting mid-flight keeps the current position and velocity instead of
 * restarting a curve. `response` is the spring's period in seconds, as in SwiftUI's spring(response:).
 */
export function stepSpring(position: number, velocity: number, target: number, dt: number, response: number) {
  const omega = (2 * Math.PI) / response
  const offset = position - target
  const decay = Math.exp(-omega * dt)
  const drift = velocity + omega * offset
  return { position: target + (offset + drift * dt) * decay, velocity: (velocity - omega * dt * drift) * decay }
}
