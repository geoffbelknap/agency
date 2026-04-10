import { defineConfig, Plugin } from 'vite'
import path from 'path'
import fs from 'fs'
import os from 'os'
import http from 'http'
import { execSync } from 'child_process'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'

/** Read the gateway address and token from AGENCY_HOME or ~/.agency/config.yaml. */
function readAgencyConfig() {
  try {
    const agencyHome = process.env.AGENCY_HOME || path.join(os.homedir(), '.agency')
    const configPath = path.join(agencyHome, 'config.yaml')
    const raw = fs.readFileSync(configPath, 'utf-8')
    const tokenMatch = raw.match(/^token:\s*(.+)$/m)
    const addrMatch = raw.match(/^gateway_addr:\s*(.+)$/m)
    const token = tokenMatch?.[1]?.trim().replace(/^["']|["']$/g, '') || ''
    let addr = addrMatch?.[1]?.trim().replace(/^["']|["']$/g, '') || '127.0.0.1:8200'
    // 0.0.0.0 is a listen address, not a valid connect target — use loopback instead
    addr = addr.replace(/^0\.0\.0\.0/, '127.0.0.1')
    return { token, addr }
  } catch {
    return { token: '', addr: '127.0.0.1:8200' }
  }
}

/**
 * Vite plugin that serves the gateway token at /__agency/config.
 * Both dev and preview modes proxy API/WS traffic, so gateway URL is
 * always empty (same-origin requests go through the proxy).
 */
/**
 * SPA fallback for Vite preview server. The dev server handles this
 * automatically, but preview does not — direct navigation to /setup
 * or any SPA route returns 404 without this.
 */
function spaFallback(): Plugin {
  return {
    name: 'spa-fallback',
    configurePreviewServer(server) {
      server.middlewares.use((req, _res, next) => {
        // If the request isn't for a file (no extension) and isn't an API/WS route, rewrite to /
        if (req.url && !req.url.startsWith('/api/') && !req.url.startsWith('/ws') &&
            !req.url.startsWith('/__agency/') && !req.url.startsWith('/assets/') &&
            !req.url.startsWith('/health') && !/\.\w+$/.test(req.url)) {
          req.url = '/index.html'
        }
        next()
      })
    },
  }
}

function agencyAutoConfig(): Plugin {
  const handler = (_req: any, res: any) => {
    // Token delivery is safe here — the preview server binds to localhost
    // and any remote access layer provides its own access control.
    const { token } = readAgencyConfig()
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify({ token, gateway: '' }))
  }
  return {
    name: 'agency-auto-config',
    configureServer(server) {
      server.middlewares.use('/__agency/config', handler)
    },
    configurePreviewServer(server) {
      server.middlewares.use('/__agency/config', handler)
    },
  }
}

/**
 * Vite plugin that proxies WebSocket upgrade requests on /ws to the gateway.
 * Vite's built-in preview proxy does not handle WebSocket upgrades, so we
 * listen for the 'upgrade' event on the HTTP server directly.
 */
function gatewayWsProxy(target: string): Plugin {
  function handleUpgrade(req: http.IncomingMessage, socket: import('net').Socket, head: Buffer) {
    if (!req.url?.startsWith('/ws')) return
    const url = new URL(target)
    // Inject auth header — browsers cannot set custom headers on WebSocket
    // upgrades, so the proxy must authenticate on the client's behalf.
    const { token } = readAgencyConfig()
    const headers: Record<string, string | string[] | undefined> = {
      ...req.headers,
      host: `${url.hostname}:${url.port}`,
    }
    if (token) headers['authorization'] = `Bearer ${token}`
    const proxyReq = http.request({
      hostname: url.hostname,
      port: url.port,
      path: req.url,
      method: req.method,
      headers,
    })
    proxyReq.on('upgrade', (_res, proxySocket, proxyHead) => {
      // Build the 101 response, forwarding relevant headers
      let reply = `HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n`
      for (const h of ['sec-websocket-accept', 'sec-websocket-extensions', 'sec-websocket-protocol']) {
        if (_res.headers[h]) reply += `${h}: ${_res.headers[h]}\r\n`
      }
      reply += `\r\n`
      socket.write(reply)
      if (proxyHead.length) socket.write(proxyHead)
      // Wire bidirectional streams with error handling
      proxySocket.on('error', () => socket.destroy())
      socket.on('error', () => proxySocket.destroy())
      proxySocket.pipe(socket)
      socket.pipe(proxySocket)
    })
    proxyReq.on('error', () => socket.destroy())
    if (head.length) proxyReq.write(head)
    proxyReq.end()
  }
  return {
    name: 'gateway-ws-proxy',
    configureServer(server) {
      server.httpServer?.on('upgrade', handleUpgrade)
    },
    configurePreviewServer(server) {
      server.httpServer?.on('upgrade', handleUpgrade)
    },
  }
}

const { addr: gatewayAddr } = readAgencyConfig()
const gatewayTarget = `http://${gatewayAddr}`

const gitCommit = (() => {
  try { return execSync('git rev-parse --short HEAD').toString().trim() } catch { return 'unknown' }
})()
const gitDirty = (() => {
  try { return execSync('git diff --quiet && git diff --cached --quiet || echo dirty').toString().trim() } catch { return '' }
})()
const buildId = gitDirty ? `${gitCommit}-${gitDirty}` : gitCommit

if (process.env.VITE_API_TOKEN && process.env.NODE_ENV === 'production') {
  console.warn('\n⚠️  WARNING: VITE_API_TOKEN is set. This token will be embedded in the production bundle.\n  Use the /__agency/config endpoint for runtime token delivery instead.\n')
}

export default defineConfig({
  define: {
    __BUILD_ID__: JSON.stringify(buildId),
    __BUILD_TIME__: JSON.stringify(new Date().toISOString()),
  },
  plugins: [
    // The React and Tailwind plugins are both required for Make, even if
    // Tailwind is not being actively used – do not remove them
    react(),
    tailwindcss(),
    agencyAutoConfig(),
    spaFallback(),
    gatewayWsProxy(gatewayTarget),
  ],
  resolve: {
    alias: {
      // Alias @ to the src directory
      '@': path.resolve(__dirname, './src'),
    },
  },

  // File types to support raw imports. Never add .css, .tsx, or .ts files to this.
  assetsInclude: ['**/*.svg', '**/*.csv'],

  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return
          if (id.includes('@mui') || id.includes('@emotion')) return 'vendor-mui'
          if (id.includes('recharts') || id.includes('d3-')) return 'vendor-charts'
          if (id.includes('react-dom') || id.includes('react-router') || id.includes('scheduler'))
            return 'vendor-react'
          if (id.includes('@radix-ui')) return 'vendor-radix'
          if (id.includes('lucide-react')) return 'vendor-icons'
          if (id.includes('motion') || id.includes('framer-motion')) return 'vendor-motion'
          if (id.includes('date-fns')) return 'vendor-date'
          if (id.includes('react-markdown') || id.includes('remark-') || id.includes('rehype-') || id.includes('unified') || id.includes('mdast') || id.includes('hast') || id.includes('micromark') || id.includes('unist'))
            return 'vendor-markdown'
        },
      },
    },
  },

  server: {
    https: fs.existsSync(path.resolve(__dirname, '.certs/localhost+2.pem'))
      ? {
          cert: fs.readFileSync(path.resolve(__dirname, '.certs/localhost+2.pem')),
          key: fs.readFileSync(path.resolve(__dirname, '.certs/localhost+2-key.pem')),
        }
      : undefined,
    proxy: {
      '/api/v1': {
        target: gatewayTarget,
        changeOrigin: true,
      },
    },
  },

  preview: {
    port: 8280,
    https: fs.existsSync(path.resolve(__dirname, '.certs/localhost+2.pem'))
      ? {
          cert: fs.readFileSync(path.resolve(__dirname, '.certs/localhost+2.pem')),
          key: fs.readFileSync(path.resolve(__dirname, '.certs/localhost+2-key.pem')),
        }
      : undefined,
    proxy: {
      '/api/v1': {
        target: gatewayTarget,
        changeOrigin: true,
      },
    },
  },
})
