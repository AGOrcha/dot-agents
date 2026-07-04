/// <reference types="vitest" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
    // Exclude Playwright e2e specs — those run under their own `playwright test`
    // runner, not Vitest. Without this exclusion Vitest picks them up and fails
    // because Playwright's test() is not compatible with Vitest's test context.
    exclude: ['**/node_modules/**', '**/e2e/**'],
    coverage: {
      provider: 'v8',
      reporter: ['lcov'],
      reportsDirectory: 'coverage',
    },
  },
})
