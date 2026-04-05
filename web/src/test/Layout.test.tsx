import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { describe, expect, it, vi } from 'vitest';
import { Layout } from '../app/components/Layout';

vi.mock('../app/lib/ws', () => ({
  socket: {
    connect: () => {},
    onConnectionChange: () => () => {},
    on: () => () => {},
  },
}));

vi.mock('../app/lib/api', () => ({
  api: {
    routing: { config: () => Promise.resolve({ configured: true }) },
    infra: { status: () => Promise.resolve({ components: [] }) },
  },
}));

vi.mock('../app/components/ThemeProvider', () => ({
  useTheme: () => ({ theme: 'dark' as const, resolvedTheme: 'dark' as const, setTheme: () => {} }),
}));

async function renderLayoutAt(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<div>Overview page</div>} />
          <Route path="/channels/:name" element={<div>Channel page</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );

  const label = path.startsWith('/channels') ? 'Channel page' : 'Overview page';
  // Layout renders a loading placeholder until async setup check completes
  const el = await screen.findByText(label);
  return el.parentElement;
}

describe('Layout scrolling behavior', () => {
  it('uses overflow-hidden for channels routes to keep page chrome fixed', async () => {
    const outletContainer = await renderLayoutAt('/channels/general');

    expect(outletContainer).toBeTruthy();
    expect(outletContainer).toHaveClass('overflow-hidden');
    expect(outletContainer).not.toHaveClass('overflow-auto');
  });

  it('uses overflow-auto for non-channels routes', async () => {
    const outletContainer = await renderLayoutAt('/');

    expect(outletContainer).toBeTruthy();
    expect(outletContainer).toHaveClass('overflow-auto');
    expect(outletContainer).not.toHaveClass('overflow-hidden');
  });
});
