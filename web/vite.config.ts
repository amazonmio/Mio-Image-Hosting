import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

export default defineConfig({
  plugins: [vue(), Components({ resolvers: [ElementPlusResolver()] })],
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: false },
      '/i/': { target: 'http://127.0.0.1:8080', changeOrigin: false },
      '/download/': { target: 'http://127.0.0.1:8080', changeOrigin: false },
      '/branding': { target: 'http://127.0.0.1:8080', changeOrigin: false },
      '/favicon.ico': { target: 'http://127.0.0.1:8080', changeOrigin: false },
    },
  },
  test: { environment: 'node', include: ['src/**/*.test.ts'] },
})
