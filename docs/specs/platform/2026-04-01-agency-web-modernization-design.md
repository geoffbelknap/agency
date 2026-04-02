# Agency Web Modernization

**Status:** Complete (2026-04-01)

Phased upgrade of agency-web to modern React, Node, and dependency versions.
All three phases implemented: React 19, Vite 8, Node 22 pin, all deps current.

## Current State

- React 18.3.1, no React 19 features
- Vite 6.4.1, @vitejs/plugin-react 4.7.0
- Node 25 locally, node:22-alpine in Dockerfile, no version pin
- ~25 Radix UI packages on older minor versions
- Several major-version-behind deps (recharts 2, date-fns 3, lucide-react 0.x, vite 6)

## Phase 1: React 19 + Radix + Node Pin

**Goal:** Establish the React 19 foundation that all other upgrades depend on.

### React 19

- Upgrade `react` and `react-dom` from 18.3.1 to 19.x (latest stable)
- Update `peerDependencies` and `peerDependenciesMeta` in package.json
- Audit for `forwardRef` usage — React 19 passes `ref` as a regular prop, so `forwardRef` wrappers in our code should be unwound
- Audit for deprecated APIs: `defaultProps` on function components, string refs, legacy context
- Update `@testing-library/react` to latest for React 19 compat
- Update `react-day-picker`, `react-hook-form`, `react-dnd` only if they require React 19-compatible versions to function (otherwise defer to Phase 3)

### Radix UI

- Update all ~25 `@radix-ui/react-*` packages to latest versions
- These are minor/patch bumps and already declare React 19 peer support
- No API changes expected; verify build + test pass

### Node Pin

- Add to package.json: `"engines": { "node": ">=22" }`
- Create `.node-version` file containing `22`
- Dockerfile already uses `node:22-alpine` — no change needed

### Validation

- `npm install` succeeds without peer dep warnings
- `npm run build` produces working output
- `npm test` passes
- Manual smoke test: dev server starts, pages render

## Phase 2: Vite 8 + Plugin React v6

**Goal:** Upgrade the build toolchain.

- Upgrade `vite` from 6.x to 8.x
- Upgrade `@vitejs/plugin-react` from 4.x to 6.x
- Upgrade `@tailwindcss/vite` to 4.2.x for Vite 8 compatibility
- Upgrade `tailwindcss` to 4.2.x to match
- Audit `vite.config.ts` for deprecated config options (Environment API changes, plugin API changes)
- Remove the `pnpm.overrides` for vite if no longer needed
- Verify dev server HMR, production build, and proxy config all work

### Validation

- `npm run dev` starts with HMR working
- `npm run build` succeeds
- `npm test` passes
- Proxy to gateway at localhost:8200 still works

## Phase 3: Remaining Dependencies

**Goal:** Bring all remaining outdated deps to current versions.

### Major version bumps (breaking changes likely)

| Package | From | To | Migration notes |
|---------|------|----|-----------------|
| recharts | 2.x | 3.x | New component APIs, audit all chart usage |
| date-fns | 3.x | 4.x | ESM-only, some renamed functions |
| lucide-react | 0.x | 1.x | Icon import paths may change |
| react-day-picker | 8.x | 9.x | New API, update calendar components |
| react-resizable-panels | 2.x | 4.x | API changes in panel components |

### Minor/patch bumps (low risk)

| Package | From | To |
|---------|------|----|
| motion | 12.23 | 12.38 |
| sonner | 2.0.3 | 2.0.7 |
| tailwind-merge | 3.2 | 3.5 |
| tw-animate-css | 1.3.8 | 1.4.0 |
| react-hook-form | 7.55 | 7.72 |
| react-router | 7.13.0 | 7.13.2 |
| @mui/material | 7.3.5 | 7.3.9 |
| @mui/icons-material | 7.3.5 | 7.3.9 |
| embla-carousel-react | 8.6.0 | latest |
| msw | 2.12.12 | 2.12.14 |
| jsdom | 29.0.0 | 29.0.1 |
| vitest | 4.1.0 | 4.1.2 |

### Validation

- `npm install` clean
- `npm run build` succeeds
- `npm test` passes
- Visual check of charts, date pickers, icons, panels

## Out of Scope

- No architectural changes (routing, state management, component structure)
- No new features
- No MUI-to-Radix migration (separate effort)
- No React Router v7 framework mode migration
