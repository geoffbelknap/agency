import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Layout } from '../app/components/Layout';

const mockInfraStatus = vi.hoisted(() => ({
  value: { components: [] as Array<{ name: string; state: string; health: string }>, build_id: '' },
}));

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
    infra: { status: () => Promise.resolve(mockInfraStatus.value) },
  },
  ensureConfig: () => Promise.resolve(),
  getVia: () => 'local' as const,
  getAuthenticated: () => false,
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
  beforeEach(() => {
    mockInfraStatus.value = { components: [], build_id: '' };
  });

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

  it('renders infrastructure footer from reported components', async () => {
    mockInfraStatus.value = {
      build_id: 'build-123',
      components: [
        { name: 'gateway', state: 'running', health: 'healthy' },
        { name: 'knowledge', state: 'running', health: 'starting' },
        { name: 'comms', state: 'exited', health: 'unhealthy' },
      ],
    };

    await renderLayoutAt('/');

    expect(screen.getByText('gateway')).toBeInTheDocument();
    expect(screen.getByText('knowledge')).toBeInTheDocument();
    expect(screen.getByText('comms')).toBeInTheDocument();
    expect(screen.queryByText('postgres')).not.toBeInTheDocument();
    expect(screen.getByText('build-123 build')).toBeInTheDocument();
  });
});
