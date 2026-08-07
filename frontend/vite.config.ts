/// <reference types="vitest" />
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import { TanStackRouterVite } from '@tanstack/router-plugin/vite'
import path from 'node:path'

export default defineConfig({
  plugins: [
    TanStackRouterVite(),
    react(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    host: '0.0.0.0',
    proxy: {
      // Proxies /api requests to the backend. The browser makes same-origin
      // requests only, which is what lets the session cookie work without a
      // cross-site exemption and without the frontend learning the API's origin.
      '/api': {
        target: process.env.VITE_API_PROXY_TARGET || 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },

  test: {
    // jsdom rather than node: the code under test reaches for fetch, Response,
    // and DOM APIs, and msw's browser-shaped interception needs a window.
    environment: 'jsdom',

    // Pinned so the origin a relative fetch resolves against is a stated fact
    // rather than jsdom's default. api/client.ts requests '/api/...', and the
    // msw handlers have to name the same origin to match — leaving it implicit
    // means a change of default breaks every handler with a confusing
    // "no matching request handler".
    environmentOptions: {
      jsdom: { url: 'http://localhost' },
    },

    // Describe/it/expect without an import in every file, matching how the
    // testing-library examples in the ecosystem are written.
    globals: true,

    setupFiles: ['./src/test/setup.ts'],

    // The generated route tree is machine-written and carries @ts-nocheck;
    // there is nothing in it a test could meaningfully assert.
    exclude: ['node_modules', 'dist', 'src/routeTree.gen.ts'],
  },
})
