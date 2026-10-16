import { defineConfig, type ProxyOptions } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// The browser calls the API under /api (see src/api/client.js). In dev and
// preview, Vite forwards those calls to the Go server and strips the prefix,
// so the Go API needs no CORS setup and SPA routes never clash with API paths.
const apiTarget = process.env.API_URL ?? 'http://localhost:8080'

const proxy: Record<string, ProxyOptions> = {
  '/api': {
    target: apiTarget,
    changeOrigin: true,
    rewrite: (path) => path.replace(/^\/api/, ''),
  },
}

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  server: { port: 5173, proxy },
  preview: { port: 4173, proxy },
})
