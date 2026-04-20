import '@testing-library/jest-dom/vitest';
import { afterAll, afterEach, beforeAll } from 'vitest';

let server: typeof import('./server')['server'];

const originalConsoleError = console.error.bind(console);
console.error = (...args: unknown[]) => {
  const first = args[0];
  if (typeof first === 'string' && first.includes("Not implemented: HTMLFormElement's requestSubmit() method")) {
    return;
  }
  originalConsoleError(...args);
};

const originalStderrWrite = process.stderr.write.bind(process.stderr);
process.stderr.write = ((chunk: string | Uint8Array, ...args: unknown[]) => {
  const text = typeof chunk === 'string' ? chunk : Buffer.from(chunk).toString('utf8');
  if (text.includes("Not implemented: HTMLFormElement's requestSubmit() method")) {
    return true;
  }
  return originalStderrWrite(chunk as never, ...(args as []));
}) as typeof process.stderr.write;

// Install this before MSW imports. Node 25 exposes an experimental localStorage
// accessor that warns unless started with --localstorage-file.
const store: Record<string, string> = {};
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  writable: true,
  value: {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => { store[key] = String(value); },
    removeItem: (key: string) => { delete store[key]; },
    clear: () => { Object.keys(store).forEach(k => delete store[k]); },
    get length() { return Object.keys(store).length; },
    key: (index: number) => Object.keys(store)[index] ?? null,
  } as Storage,
});

// Polyfill ResizeObserver for jsdom (required by @radix-ui/react-scroll-area)
globalThis.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// Polyfill window.matchMedia for jsdom (required by use-mobile hook)
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
});

// Override jsdom's unimplemented requestSubmit with a submit-event shim.
Object.defineProperty(HTMLFormElement.prototype, 'requestSubmit', {
  configurable: true,
  writable: true,
  value: function (submitter?: HTMLElement) {
    const EventCtor = typeof SubmitEvent === 'function' ? SubmitEvent : Event;
    const event = new EventCtor('submit', {
      bubbles: true,
      cancelable: true,
      ...(typeof SubmitEvent === 'function'
        ? { submitter: submitter instanceof HTMLElement ? submitter : null }
        : {}),
    });
    this.dispatchEvent(event);
  },
});

beforeAll(async () => {
  server = (await import('./server')).server;
  server.listen({ onUnhandledRequest: 'warn' });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
