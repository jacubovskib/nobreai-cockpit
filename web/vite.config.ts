/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Em desenvolvimento o /api vai para o nobred; changeOrigin reescreve o Host
// para 127.0.0.1:7420, que é o único que o daemon aceita.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:7420',
        changeOrigin: true,
        ws: true,
        // O daemon confere o Origin do WebSocket; só em dev reescrevemos o do Vite.
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        configure: (proxy: any) => {
          proxy.on('proxyReqWs', (proxyReq: { setHeader(k: string, v: string): void }) => proxyReq.setHeader('Origin', 'http://127.0.0.1:7420'))
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
    globals: true,
  },
})
