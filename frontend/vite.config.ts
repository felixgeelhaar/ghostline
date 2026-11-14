import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import { clientIpAwareProxy } from './proxyClientIp'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '.', '')
  return {
    plugins: [vue()],
    server: {
      host: '0.0.0.0',
      proxy: {
        '/auth': clientIpAwareProxy(env.GHOSTLINE_API_URL || 'http://localhost:8080'),
        '/api': clientIpAwareProxy(env.GHOSTLINE_API_URL || 'http://localhost:8080'),
      },
    },
  }
})
