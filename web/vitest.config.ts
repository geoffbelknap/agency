import { defineConfig } from 'vitest/config';
import path from 'path';
import react from '@vitejs/plugin-react';

export default defineConfig({
  define: {
    __BUILD_ID__: JSON.stringify('test'),
    __BUILD_TIME__: JSON.stringify('test'),
  },
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    css: false,
    maxWorkers: 1,
    env: {
      VITE_API_BASE_URL: 'http://localhost:8200/api/v1',
    },
  },
});
