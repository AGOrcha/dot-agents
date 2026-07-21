/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_OBSERVABILITY_TRANSPORT?: 'sse' | 'websocket'
}
