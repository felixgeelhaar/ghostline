import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import { setClientIpHeader } from './proxyClientIp'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '.', '')
  return {
    plugins: [vue()],
    server: {
      host: '0.0.0.0',
      proxy: {
        '/api': {
          target: env.GHOSTLINE_API_URL || 'http://localhost:8080',
          configure: (proxy) => { proxy.on('proxyReq', setClientIpHeader) },
        },
      },
    },
  }
})
