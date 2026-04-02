# Agency Web Modernization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Complete (2026-04-01) — all 13 tasks implemented across 3 phases.

**Goal:** Upgrade agency-web to React 19, pin Node 22, upgrade Vite to 8, and bring all dependencies to current versions — in three phases.

**Architecture:** Phased upgrade — React 19 foundation first, then build toolchain (Vite 8), then remaining deps. Each phase produces a working, testable build before the next begins.

**Tech Stack:** React 19, Vite 8, Node 22 LTS, Tailwind CSS 4.2, Radix UI (latest), Recharts 3, lucide-react 1.x

---

## Phase 1: React 19 + Radix + Node Pin

### Task 1: Pin Node version

**Files:**
- Create: `.node-version`
- Modify: `package.json`

- [ ] **Step 1: Create `.node-version` file**

```
22
```

- [ ] **Step 2: Add engines field to `package.json`**

Add after the `"peerDependenciesMeta"` block:

```json
"engines": {
  "node": ">=22"
},
```

- [ ] **Step 3: Commit**

```bash
git add .node-version package.json
git commit -m "build: pin node >=22 with .node-version and engines field"
```

---

### Task 2: Upgrade React 18 to 19

**Files:**
- Modify: `package.json`

- [ ] **Step 1: Install React 19 and update peer deps**

```bash
npm install react@latest react-dom@latest
```

- [ ] **Step 2: Update `peerDependencies` in `package.json`**

Change:

```json
"peerDependencies": {
  "react": "18.3.1",
  "react-dom": "18.3.1"
},
```

To:

```json
"peerDependencies": {
  "react": "19.2.4",
  "react-dom": "19.2.4"
},
```

(Use whatever version `npm install` resolved — check `node_modules/react/package.json` for the exact version.)

- [ ] **Step 3: Verify installation**

```bash
node -e "console.log(require('./node_modules/react/package.json').version)"
```

Expected: `19.x.x`

- [ ] **Step 4: Run build to check for React 19 compatibility errors**

```bash
npm run build 2>&1 | head -50
```

Note any errors — they will be addressed in the next tasks.

- [ ] **Step 5: Run tests**

```bash
npm test 2>&1 | tail -30
```

Note any failures for debugging in subsequent tasks.

- [ ] **Step 6: Commit**

```bash
git add package.json package-lock.json
git commit -m "feat: upgrade react and react-dom from 18 to 19"
```

---

### Task 3: Remove `forwardRef` from UI components (React 19 migration)

React 19 passes `ref` as a regular prop, making `forwardRef` unnecessary. Convert all 5 UI components.

**Files:**
- Modify: `src/app/components/ui/button.tsx`
- Modify: `src/app/components/ui/input.tsx`
- Modify: `src/app/components/ui/scroll-area.tsx`
- Modify: `src/app/components/ui/dialog.tsx`
- Modify: `src/app/components/ui/alert-dialog.tsx`

- [ ] **Step 1: Convert `button.tsx`**

Replace:

```tsx
const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";

    return (
      <Comp
        ref={ref}
        data-slot="button"
        className={cn(buttonVariants({ variant, size, className }))}
        {...props}
      />
    );
  },
);

Button.displayName = "Button";
```

With:

```tsx
function Button({
  className,
  variant,
  size,
  asChild = false,
  ref,
  ...props
}: ButtonProps & { ref?: React.Ref<HTMLButtonElement> }) {
  const Comp = asChild ? Slot : "button";

  return (
    <Comp
      ref={ref}
      data-slot="button"
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  );
}
```

- [ ] **Step 2: Convert `input.tsx`**

Replace:

```tsx
const Input = React.forwardRef<HTMLInputElement, React.ComponentProps<"input">>(
  ({ className, type, ...props }, ref) => {
    return (
      <input
        type={type}
        ref={ref}
        data-slot="input"
        className={cn(
          "file:text-foreground placeholder:text-muted-foreground selection:bg-primary selection:text-primary-foreground dark:bg-input/30 border-input flex h-9 w-full min-w-0 rounded-md border px-3 py-1 text-base bg-input-background transition-[color,box-shadow] outline-none file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
          "focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]",
          "aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive",
          className,
        )}
        {...props}
      />
    );
  },
);

Input.displayName = "Input";
```

With:

```tsx
function Input({
  className,
  type,
  ref,
  ...props
}: React.ComponentProps<"input"> & { ref?: React.Ref<HTMLInputElement> }) {
  return (
    <input
      type={type}
      ref={ref}
      data-slot="input"
      className={cn(
        "file:text-foreground placeholder:text-muted-foreground selection:bg-primary selection:text-primary-foreground dark:bg-input/30 border-input flex h-9 w-full min-w-0 rounded-md border px-3 py-1 text-base bg-input-background transition-[color,box-shadow] outline-none file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
        "focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]",
        "aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive",
        className,
      )}
      {...props}
    />
  );
}
```

- [ ] **Step 3: Convert `scroll-area.tsx`**

Replace:

```tsx
const ScrollArea = React.forwardRef<
  React.ElementRef<typeof ScrollAreaPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof ScrollAreaPrimitive.Root>
>(({ className, children, ...props }, ref) => {
  return (
    <ScrollAreaPrimitive.Root
      ref={ref}
      data-slot="scroll-area"
      className={cn("relative", className)}
      {...props}
    >
      <ScrollAreaPrimitive.Viewport
        data-slot="scroll-area-viewport"
        className="focus-visible:ring-ring/50 size-full rounded-[inherit] transition-[color,box-shadow] outline-none focus-visible:ring-[3px] focus-visible:outline-1"
      >
        {children}
      </ScrollAreaPrimitive.Viewport>
      <ScrollBar />
      <ScrollAreaPrimitive.Corner />
    </ScrollAreaPrimitive.Root>
  );
});
ScrollArea.displayName = 'ScrollArea';
```

With:

```tsx
function ScrollArea({
  className,
  children,
  ref,
  ...props
}: React.ComponentPropsWithoutRef<typeof ScrollAreaPrimitive.Root> & {
  ref?: React.Ref<React.ElementRef<typeof ScrollAreaPrimitive.Root>>;
}) {
  return (
    <ScrollAreaPrimitive.Root
      ref={ref}
      data-slot="scroll-area"
      className={cn("relative", className)}
      {...props}
    >
      <ScrollAreaPrimitive.Viewport
        data-slot="scroll-area-viewport"
        className="focus-visible:ring-ring/50 size-full rounded-[inherit] transition-[color,box-shadow] outline-none focus-visible:ring-[3px] focus-visible:outline-1"
      >
        {children}
      </ScrollAreaPrimitive.Viewport>
      <ScrollBar />
      <ScrollAreaPrimitive.Corner />
    </ScrollAreaPrimitive.Root>
  );
}
```

- [ ] **Step 4: Convert `dialog.tsx` — DialogOverlay and DialogContent**

Replace `DialogOverlay`:

```tsx
const DialogOverlay = React.forwardRef<
  React.ComponentRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    data-slot="dialog-overlay"
    className={cn(
      "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 fixed inset-0 z-50 bg-black/50",
      className,
    )}
    {...props}
  />
));
DialogOverlay.displayName = "DialogOverlay";
```

With:

```tsx
function DialogOverlay({
  className,
  ref,
  ...props
}: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay> & {
  ref?: React.Ref<React.ComponentRef<typeof DialogPrimitive.Overlay>>;
}) {
  return (
    <DialogPrimitive.Overlay
      ref={ref}
      data-slot="dialog-overlay"
      className={cn(
        "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 fixed inset-0 z-50 bg-black/50",
        className,
      )}
      {...props}
    />
  );
}
```

Replace `DialogContent`:

```tsx
const DialogContent = React.forwardRef<
  React.ComponentRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content>
>(({ className, children, ...props }, ref) => (
  <DialogPortal data-slot="dialog-portal">
    <DialogOverlay />
    <DialogPrimitive.Content
      ref={ref}
      data-slot="dialog-content"
      className={cn(
        "bg-background data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 fixed top-[50%] left-[50%] z-50 grid w-full max-w-[calc(100%-2rem)] translate-x-[-50%] translate-y-[-50%] gap-4 rounded-lg border p-6 shadow-lg duration-200 sm:max-w-lg",
        className,
      )}
      {...props}
    >
      {children}
      <DialogPrimitive.Close className="ring-offset-background focus:ring-ring data-[state=open]:bg-accent data-[state=open]:text-muted-foreground absolute top-4 right-4 rounded-xs opacity-70 transition-opacity hover:opacity-100 focus:ring-2 focus:ring-offset-2 focus:outline-hidden disabled:pointer-events-none [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4">
        <XIcon />
        <span className="sr-only">Close</span>
      </DialogPrimitive.Close>
    </DialogPrimitive.Content>
  </DialogPortal>
));
DialogContent.displayName = "DialogContent";
```

With:

```tsx
function DialogContent({
  className,
  children,
  ref,
  ...props
}: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & {
  ref?: React.Ref<React.ComponentRef<typeof DialogPrimitive.Content>>;
}) {
  return (
    <DialogPortal data-slot="dialog-portal">
      <DialogOverlay />
      <DialogPrimitive.Content
        ref={ref}
        data-slot="dialog-content"
        className={cn(
          "bg-background data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 fixed top-[50%] left-[50%] z-50 grid w-full max-w-[calc(100%-2rem)] translate-x-[-50%] translate-y-[-50%] gap-4 rounded-lg border p-6 shadow-lg duration-200 sm:max-w-lg",
          className,
        )}
        {...props}
      >
        {children}
        <DialogPrimitive.Close className="ring-offset-background focus:ring-ring data-[state=open]:bg-accent data-[state=open]:text-muted-foreground absolute top-4 right-4 rounded-xs opacity-70 transition-opacity hover:opacity-100 focus:ring-2 focus:ring-offset-2 focus:outline-hidden disabled:pointer-events-none [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4">
          <XIcon />
          <span className="sr-only">Close</span>
        </DialogPrimitive.Close>
      </DialogPrimitive.Content>
    </DialogPortal>
  );
}
```

- [ ] **Step 5: Convert `alert-dialog.tsx` — AlertDialogOverlay and AlertDialogContent**

Replace `AlertDialogOverlay`:

```tsx
const AlertDialogOverlay = React.forwardRef<
  React.ComponentRef<typeof AlertDialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <AlertDialogPrimitive.Overlay
    ref={ref}
    data-slot="alert-dialog-overlay"
    className={cn(
      "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 fixed inset-0 z-50 bg-black/50",
      className,
    )}
    {...props}
  />
));
AlertDialogOverlay.displayName = "AlertDialogOverlay";
```

With:

```tsx
function AlertDialogOverlay({
  className,
  ref,
  ...props
}: React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Overlay> & {
  ref?: React.Ref<React.ComponentRef<typeof AlertDialogPrimitive.Overlay>>;
}) {
  return (
    <AlertDialogPrimitive.Overlay
      ref={ref}
      data-slot="alert-dialog-overlay"
      className={cn(
        "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 fixed inset-0 z-50 bg-black/50",
        className,
      )}
      {...props}
    />
  );
}
```

Replace `AlertDialogContent`:

```tsx
const AlertDialogContent = React.forwardRef<
  React.ComponentRef<typeof AlertDialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Content>
>(({ className, ...props }, ref) => (
  <AlertDialogPortal>
    <AlertDialogOverlay />
    <AlertDialogPrimitive.Content
      ref={ref}
      data-slot="alert-dialog-content"
      className={cn(
        "bg-background data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 fixed top-[50%] left-[50%] z-50 grid w-full max-w-[calc(100%-2rem)] translate-x-[-50%] translate-y-[-50%] gap-4 rounded-lg border p-6 shadow-lg duration-200 sm:max-w-lg",
        className,
      )}
      {...props}
    />
  </AlertDialogPortal>
));
AlertDialogContent.displayName = "AlertDialogContent";
```

With:

```tsx
function AlertDialogContent({
  className,
  ref,
  ...props
}: React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Content> & {
  ref?: React.Ref<React.ComponentRef<typeof AlertDialogPrimitive.Content>>;
}) {
  return (
    <AlertDialogPortal>
      <AlertDialogOverlay />
      <AlertDialogPrimitive.Content
        ref={ref}
        data-slot="alert-dialog-content"
        className={cn(
          "bg-background data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 fixed top-[50%] left-[50%] z-50 grid w-full max-w-[calc(100%-2rem)] translate-x-[-50%] translate-y-[-50%] gap-4 rounded-lg border p-6 shadow-lg duration-200 sm:max-w-lg",
          className,
        )}
        {...props}
      />
    </AlertDialogPortal>
  );
}
```

- [ ] **Step 6: Run build and tests**

```bash
npm run build 2>&1 | tail -10
npm test 2>&1 | tail -30
```

Expected: build succeeds, tests pass.

- [ ] **Step 7: Commit**

```bash
git add src/app/components/ui/button.tsx src/app/components/ui/input.tsx src/app/components/ui/scroll-area.tsx src/app/components/ui/dialog.tsx src/app/components/ui/alert-dialog.tsx
git commit -m "refactor: remove forwardRef wrappers for React 19 ref-as-prop"
```

---

### Task 4: Upgrade all Radix UI packages

**Files:**
- Modify: `package.json`, `package-lock.json`

- [ ] **Step 1: Upgrade all Radix packages to latest**

```bash
npm install \
  @radix-ui/react-accordion@latest \
  @radix-ui/react-alert-dialog@latest \
  @radix-ui/react-aspect-ratio@latest \
  @radix-ui/react-avatar@latest \
  @radix-ui/react-checkbox@latest \
  @radix-ui/react-collapsible@latest \
  @radix-ui/react-context-menu@latest \
  @radix-ui/react-dialog@latest \
  @radix-ui/react-dropdown-menu@latest \
  @radix-ui/react-hover-card@latest \
  @radix-ui/react-label@latest \
  @radix-ui/react-menubar@latest \
  @radix-ui/react-navigation-menu@latest \
  @radix-ui/react-popover@latest \
  @radix-ui/react-progress@latest \
  @radix-ui/react-radio-group@latest \
  @radix-ui/react-scroll-area@latest \
  @radix-ui/react-select@latest \
  @radix-ui/react-separator@latest \
  @radix-ui/react-slider@latest \
  @radix-ui/react-slot@latest \
  @radix-ui/react-switch@latest \
  @radix-ui/react-tabs@latest \
  @radix-ui/react-toggle@latest \
  @radix-ui/react-toggle-group@latest \
  @radix-ui/react-tooltip@latest
```

- [ ] **Step 2: Build and test**

```bash
npm run build 2>&1 | tail -10
npm test 2>&1 | tail -30
```

Expected: build succeeds, tests pass. Radix minor/patch bumps should not break anything.

- [ ] **Step 3: Commit**

```bash
git add package.json package-lock.json
git commit -m "deps: upgrade all radix-ui packages to latest"
```

---

### Task 5: Phase 1 validation

- [ ] **Step 1: Full build**

```bash
npm run build
```

Expected: clean build, no warnings about React 18 APIs.

- [ ] **Step 2: Full test suite**

```bash
npm test
```

Expected: all tests pass.

- [ ] **Step 3: Dev server smoke test**

```bash
npm run dev &
sleep 3
curl -sk https://localhost:5173 | head -5
kill %1
```

Expected: HTML response with React app shell.

---

## Phase 2: Vite 8 + Plugin React v6

### Task 6: Upgrade Vite and build plugins

**Files:**
- Modify: `package.json`, `package-lock.json`
- Modify: `vite.config.ts` (if needed)

- [ ] **Step 1: Upgrade Vite, plugin-react, and Tailwind CSS**

```bash
npm install vite@latest @vitejs/plugin-react@latest
npm install @tailwindcss/vite@latest tailwindcss@latest @tailwindcss/typography@latest
```

- [ ] **Step 2: Remove the pnpm vite override from `package.json`**

Delete this block from `package.json`:

```json
"pnpm": {
  "overrides": {
    "vite": "6.4.1"
  }
},
```

- [ ] **Step 3: Check for Vite 8 config deprecations**

```bash
npm run build 2>&1 | head -30
```

If there are deprecation warnings or errors about `vite.config.ts`, fix them. Common Vite 8 changes:
- `server.https` config may need adjustment
- Plugin API changes (check `configureServer` and `configurePreviewServer` signatures)
- `rollupOptions.output.manualChunks` may have Rollup 4 changes

- [ ] **Step 4: Run tests**

```bash
npm test 2>&1 | tail -30
```

- [ ] **Step 5: Commit**

```bash
git add package.json package-lock.json vite.config.ts
git commit -m "build: upgrade vite to 8, plugin-react to 6, tailwind to 4.2"
```

---

### Task 7: Phase 2 validation

- [ ] **Step 1: Full build**

```bash
npm run build
```

- [ ] **Step 2: Full test suite**

```bash
npm test
```

- [ ] **Step 3: Dev server HMR test**

```bash
npm run dev &
sleep 3
curl -sk https://localhost:5173 | head -5
kill %1
```

- [ ] **Step 4: Preview server test**

```bash
npm run build && npx agency-web &
sleep 3
curl -sk https://localhost:8280 | head -5
kill %1
```

---

## Phase 3: Remaining Dependencies

### Task 8: Upgrade lucide-react 0.x to 1.x

This is the biggest change — 71 files import from lucide-react. The 1.x release may rename some icons.

**Files:**
- Modify: `package.json`
- Potentially modify: 71 source files if icon names changed

- [ ] **Step 1: Upgrade**

```bash
npm install lucide-react@latest
```

- [ ] **Step 2: Build to find broken imports**

```bash
npm run build 2>&1 | grep -i "error\|not found\|cannot find"
```

If any icon imports fail, check the lucide-react 1.x changelog for renames. Common renames follow the pattern `FooIcon` → `Foo`. Fix all broken imports.

- [ ] **Step 3: Run tests**

```bash
npm test 2>&1 | tail -30
```

- [ ] **Step 4: Commit**

```bash
git add -u
git commit -m "deps: upgrade lucide-react from 0.x to 1.x"
```

---

### Task 9: Upgrade recharts 2 to 3

**Files:**
- Modify: `package.json`
- Modify: `src/app/screens/missions/MissionHealthTab.tsx`
- Modify: `src/app/components/ui/chart.tsx`

- [ ] **Step 1: Upgrade**

```bash
npm install recharts@latest
```

- [ ] **Step 2: Build and check for API changes**

```bash
npm run build 2>&1 | grep -i "error"
```

Recharts 3 changes include new component prop types and possible import changes. Fix any build errors in the 2 affected files.

- [ ] **Step 3: Commit**

```bash
git add -u
git commit -m "deps: upgrade recharts from 2 to 3"
```

---

### Task 10: Upgrade react-day-picker 8 to 9

**Files:**
- Modify: `package.json`
- Modify: `src/app/components/ui/calendar.tsx`
- Modify: `src/app/screens/Usage.tsx`

- [ ] **Step 1: Upgrade**

```bash
npm install react-day-picker@latest
```

- [ ] **Step 2: Build and fix API changes**

```bash
npm run build 2>&1 | grep -i "error"
```

react-day-picker 9 has significant API changes — `DayPicker` props changed. Fix imports and component usage in the 2 affected files.

- [ ] **Step 3: Commit**

```bash
git add -u
git commit -m "deps: upgrade react-day-picker from 8 to 9"
```

---

### Task 11: Upgrade react-resizable-panels 2 to 4

**Files:**
- Modify: `package.json`
- Modify: `src/app/components/ui/resizable.tsx`

- [ ] **Step 1: Upgrade**

```bash
npm install react-resizable-panels@latest
```

- [ ] **Step 2: Build and fix**

```bash
npm run build 2>&1 | grep -i "error"
```

- [ ] **Step 3: Commit**

```bash
git add -u
git commit -m "deps: upgrade react-resizable-panels from 2 to 4"
```

---

### Task 12: Remove unused date-fns and upgrade minor deps

**Files:**
- Modify: `package.json`

- [ ] **Step 1: Remove date-fns (zero imports in codebase)**

```bash
npm uninstall date-fns
```

- [ ] **Step 2: Upgrade all remaining minor/patch deps**

```bash
npm install \
  @mui/material@latest @mui/icons-material@latest \
  motion@latest \
  sonner@latest \
  tailwind-merge@latest \
  tw-animate-css@latest \
  react-hook-form@latest \
  react-router@latest \
  embla-carousel-react@latest \
  msw@latest \
  jsdom@latest \
  vitest@latest
```

- [ ] **Step 3: Build and test**

```bash
npm run build 2>&1 | tail -10
npm test 2>&1 | tail -30
```

- [ ] **Step 4: Commit**

```bash
git add package.json package-lock.json
git commit -m "deps: remove unused date-fns, upgrade minor deps to latest"
```

---

### Task 13: Final validation

- [ ] **Step 1: Clean install**

```bash
rm -rf node_modules package-lock.json
npm install
```

- [ ] **Step 2: Full build**

```bash
npm run build
```

- [ ] **Step 3: Full test suite**

```bash
npm test
```

- [ ] **Step 4: Dev server**

```bash
npm run dev &
sleep 3
curl -sk https://localhost:5173 | head -5
kill %1
```

- [ ] **Step 5: Check for peer dep warnings**

```bash
npm ls 2>&1 | grep -i "peer dep\|ERESOLVE\|invalid" || echo "No peer dep issues"
```

- [ ] **Step 6: Verify React version in browser**

Start dev server and check browser console:

```bash
npm run dev
```

Open browser devtools and run: `__REACT_DEVTOOLS_GLOBAL_HOOK__ ? 'React DevTools detected' : 'Check React version in network tab'`

- [ ] **Step 7: Commit any remaining fixes**

```bash
git add -u
git commit -m "build: final validation — clean install, all tests pass"
```
