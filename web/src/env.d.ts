/// <reference types="vite/client" />

declare global {
  interface ImportMetaEnv {
    /** Where the Go API lives. Defaults to /api, which Vite proxies in dev. */
    readonly VITE_API_BASE_URL?: string
  }
}

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    requiresAuth?: boolean
    guest?: boolean
    public?: boolean
  }
}

export {}
