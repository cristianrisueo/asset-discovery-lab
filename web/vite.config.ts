import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

// Spec §14.3: the production build is served by nginx; `npm run dev` proxies
// /api to a local explorer.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
  },
});
