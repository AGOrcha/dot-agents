import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import {
  createSseObservabilityTransport,
  createWebSocketObservabilityTransport,
} from './api/observabilityTransport'
import { useLiveUpdates } from './hooks/useLiveUpdates'
import './index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Stale time: 30s (store LRU TTL is 30s — stay in sync with backend cache).
      staleTime: 30_000,
      // Retry once on failure; dashboard data is best-effort.
      retry: 1,
    },
  },
})
const observabilityTransport =
  import.meta.env.VITE_OBSERVABILITY_TRANSPORT === 'websocket'
    ? createWebSocketObservabilityTransport()
    : createSseObservabilityTransport()

function Dashboard() {
  useLiveUpdates(observabilityTransport)
  return (
    <BrowserRouter>
      <App />
    </BrowserRouter>
  )
}


const root = document.getElementById('root')
if (!root) throw new Error('Root element #root not found')

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <Dashboard />
    </QueryClientProvider>
  </StrictMode>,
)
