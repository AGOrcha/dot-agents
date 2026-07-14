import '@testing-library/jest-dom'

// Recharts' ResponsiveContainer needs ResizeObserver, which jsdom lacks (real
// browsers provide it). Stub it once here so any chart mounts under the test
// renderer without every suite re-declaring the shim. jsdom has no layout
// engine, so every observer callback is a no-op.
class ResizeObserverStub {
  observe(): void {
    // no-op: nothing to measure without a layout engine
  }

  unobserve(): void {
    // no-op: no observations are ever scheduled
  }

  disconnect(): void {
    // no-op: nothing to tear down
  }
}
globalThis.ResizeObserver ??= ResizeObserverStub as unknown as typeof ResizeObserver
