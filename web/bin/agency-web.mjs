#!/usr/bin/env node

import { execFileSync, spawn } from 'child_process'
import { existsSync, mkdirSync, readFileSync, unlinkSync, writeFileSync, openSync } from 'fs'
import { dirname, join, resolve } from 'path'
import { homedir, networkInterfaces } from 'os'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const root = resolve(__dirname, '..')
const dist = resolve(root, 'dist')
const vite = resolve(root, 'node_modules', '.bin', 'vite')
const stateDir = join(homedir(), '.agency')
const pidFile = join(stateDir, 'web.pid')
const logFile = join(stateDir, 'web.log')
const stateFile = join(stateDir, 'web.state.json')
const DEFAULT_PORT = 8280

// Parse --port from argv
function getPort() {
  const idx = process.argv.indexOf('--port')
  if (idx !== -1 && process.argv[idx + 1]) {
    const p = parseInt(process.argv[idx + 1], 10)
    if (p > 0 && p < 65536) return p
  }
  return DEFAULT_PORT
}

function getHost() {
  const idx = process.argv.indexOf('--host')
  if (idx === -1 || !process.argv[idx + 1]) return '127.0.0.1'
  const val = process.argv[idx + 1]
  // If it looks like an IP or 0.0.0.0/localhost, use directly
  if (/^[\d.:]+$/.test(val) || val === 'localhost') return val
  // Otherwise treat as interface name and resolve to its IPv4 address
  const ifaces = networkInterfaces()
  const iface = ifaces[val]
  if (iface) {
    const v4 = iface.find((a) => a.family === 'IPv4' && !a.internal)
    if (v4) return v4.address
    const any = iface.find((a) => a.family === 'IPv4')
    if (any) return any.address
  }
  console.error(`Unknown interface: ${val}`)
  console.error(`Available: ${Object.keys(ifaces).join(', ')}`)
  process.exit(1)
}

const port = getPort()
const host = getHost()

function baseURL() {
  const proto = existsSync(resolve(root, '.certs', 'localhost+2.pem')) ? 'https' : 'http'
  const display = host === '0.0.0.0' ? 'localhost' : host
  return `${proto}://${display}:${port}`
}

function readPid() {
  try {
    const pid = parseInt(readFileSync(pidFile, 'utf-8').trim(), 10)
    process.kill(pid, 0)
    return pid
  } catch {
    try { unlinkSync(pidFile) } catch {}
    try { unlinkSync(stateFile) } catch {}
    return null
  }
}

function savedURL() {
  try {
    const state = JSON.parse(readFileSync(stateFile, 'utf-8'))
    return state.url || baseURL()
  } catch {
    return baseURL()
  }
}

function ensureTLS() {
  const certsDir = resolve(root, '.certs')
  const certFile = resolve(certsDir, 'localhost+2.pem')
  if (existsSync(certFile)) return

  console.log('\n  Setting up TLS for agency-web...\n')
  mkdirSync(certsDir, { recursive: true })

  try {
    execFileSync('mkcert', ['-help'], { stdio: 'ignore' })
  } catch {
    console.log('  mkcert is required for HTTPS. Install it with:\n')
    console.log('    macOS:  brew install mkcert')
    console.log('    Linux:  sudo apt install mkcert  (or see https://github.com/FiloSottile/mkcert#installation)\n')
    console.log('  Then run agency-web again.\n')
    process.exit(1)
  }

  console.log('  Installing local CA (you may be prompted for your password)...')
  try {
    execFileSync('mkcert', ['-install'], { stdio: 'inherit' })
  } catch {
    console.log('\n  CA install failed — you can retry later with: mkcert -install')
    console.log('  Continuing without trusted CA (browser will show a warning).\n')
  }

  // Include the bound host IP in the cert if it's not localhost
  const certHosts = ['localhost', '127.0.0.1', '::1']
  if (host !== '0.0.0.0' && host !== '127.0.0.1' && host !== 'localhost' && !certHosts.includes(host)) {
    certHosts.push(host)
  }
  // Also include all IPv4 addresses so the cert works on any interface
  const ifaces = networkInterfaces()
  for (const addrs of Object.values(ifaces)) {
    for (const a of addrs) {
      if (a.family === 'IPv4' && !a.internal && !certHosts.includes(a.address)) {
        certHosts.push(a.address)
      }
    }
  }
  console.log('  Generating certificates for:', certHosts.join(', '))
  execFileSync('mkcert', [
    '-cert-file', 'localhost+2.pem',
    '-key-file', 'localhost+2-key.pem',
    ...certHosts,
  ], { cwd: certsDir, stdio: 'inherit' })
  console.log('')
}

function start() {
  const running = readPid()
  if (running) {
    console.log(`agency-web is already running at ${savedURL()} (pid ${running})`)
    process.exit(0)
  }

  ensureTLS()

  if (!existsSync(dist)) {
    console.log('Building...')
    execFileSync(vite, ['build'], { cwd: root, stdio: 'inherit' })
  }

  mkdirSync(stateDir, { recursive: true })
  const out = openSync(logFile, 'a')

  const child = spawn(vite, ['preview', '--host', host, '--port', String(port)], {
    cwd: root,
    detached: true,
    stdio: ['ignore', out, out],
  })

  const url = baseURL()
  writeFileSync(pidFile, String(child.pid))
  writeFileSync(stateFile, JSON.stringify({ url, host, port }))
  child.unref()

  console.log(`agency-web started at ${url} (pid ${child.pid})`)
}

function stop() {
  const pid = readPid()
  if (!pid) {
    console.log('agency-web is not running')
    return
  }
  process.kill(pid, 'SIGTERM')
  try { unlinkSync(pidFile) } catch {}
  console.log(`agency-web stopped (pid ${pid})`)
}

function status() {
  const pid = readPid()
  if (pid) {
    console.log(`agency-web is running at ${savedURL()} (pid ${pid})`)
  } else {
    console.log('agency-web is not running')
  }
}

// Find the command — skip flags like --host and --port
const command = (() => {
  const args = process.argv.slice(2)
  let skipNext = false
  for (const arg of args) {
    if (skipNext) { skipNext = false; continue }
    if (arg === '--host' || arg === '--port') { skipNext = true; continue }
    if (arg.startsWith('-')) continue
    return arg
  }
  return 'start'
})()

switch (command) {
  case 'start':
    start()
    break
  case 'stop':
    stop()
    break
  case 'restart':
    stop()
    start()
    break
  case 'status':
    status()
    break
  case 'build':
    execFileSync(vite, ['build'], { cwd: root, stdio: 'inherit' })
    break
  case 'dev':
    execFileSync(vite, [], { cwd: root, stdio: 'inherit' })
    break
  default:
    console.error(`Unknown command: ${command}`)
    console.error('Usage: agency-web [start|stop|restart|status|build|dev] [--port PORT] [--host HOST]')
    console.error(`Default: port ${DEFAULT_PORT}, host 127.0.0.1 (localhost only, use --host 0.0.0.0 for all interfaces)`)
    process.exit(1)
}
