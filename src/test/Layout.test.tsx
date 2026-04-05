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

vi.mock('../app/components/ThemeProvider', () => ({
  useTheme: () => ({ theme: 'dark' as const, resolvedTheme: 'dark' as const, setTheme: () => {} }),
}));

function renderLayoutAt(path: string) {
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

  return screen.getByText(path.startsWith('/channels') ? 'Channel page' : 'Overview page').parentElement;
}

describe('Layout scrolling behavior', () => {
  it('uses overflow-hidden for channels routes to keep page chrome fixed', () => {
    const outletContainer = renderLayoutAt('/channels/general');

    expect(outletContainer).toBeTruthy();
    expect(outletContainer).toHaveClass('overflow-hidden');
    expect(outletContainer).not.toHaveClass('overflow-auto');
  });

  it('uses overflow-auto for non-channels routes', () => {
    const outletContainer = renderLayoutAt('/');

    expect(outletContainer).toBeTruthy();
    expect(outletContainer).toHaveClass('overflow-auto');
    expect(outletContainer).not.toHaveClass('overflow-hidden');
  });
});
