import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      // forward API requests to the backend running in Docker on localhost:3003
      '^/api': {
        target: 'http://localhost:3003',
        changeOrigin: true,
        secure: false,
        rewrite: (path) => path,
      },
    },
  },
})
