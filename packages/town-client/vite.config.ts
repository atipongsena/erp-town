import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import { createDevProxy } from './dev-proxy.ts'

export default defineConfig({
  base: './',
  cacheDir: process.env.TOWN_DEV_CACHE_DIR ?? 'node_modules/.vite',
  plugins: [react()],
  server: { port: 5173, strictPort: true, proxy: createDevProxy(process.env) },
  build: {
    outDir: 'dist',
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            {
              name: 'graphics',
              maxSize: 450000,
              test: (id) =>
                id.includes('/three/') ||
                id.includes('/three-stdlib/') ||
                id.includes('/@react-three/'),
            },
          ],
        },
      },
    },
  },
})
