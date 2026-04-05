#!/usr/bin/env node

import { symlinkSync, unlinkSync, rmSync, existsSync } from 'fs'
import { dirname, resolve } from 'path'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const root = resolve(__dirname, '..')
const bin = resolve(dirname(process.execPath), 'agency-web')
const target = resolve(__dirname, 'agency-web.mjs')

// Clear old build so next start rebuilds with new code
const dist = resolve(root, 'dist')
if (existsSync(dist)) {
  rmSync(dist, { recursive: true, force: true })
}

try { unlinkSync(bin) } catch {}

try { symlinkSync(target, bin) } catch {}

console.log(`
  agency-web is ready. Run with:

    npx agency-web              Start the web UI (default port 8280)
    npx agency-web --port 9000  Start on a custom port
    npx agency-web stop         Stop it
    npx agency-web status       Check if running
`)
