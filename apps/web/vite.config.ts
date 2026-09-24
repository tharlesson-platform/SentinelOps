import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig(({ mode }) => ({
  base: loadEnv(mode, process.cwd(), 'VITE_').VITE_BASE_PATH || '/',
  plugins: [react()],
  server: { port: 3000, proxy: { '/api': 'http://api:8080', '/healthz': 'http://api:8080' } },
  preview: { port: 3000, host: '0.0.0.0' },
}))
