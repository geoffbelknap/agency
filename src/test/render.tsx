import { render, type RenderOptions } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import type { ReactElement } from 'react';

interface Options extends RenderOptions {
  route?: string;
}

export function renderWithRouter(ui: ReactElement, { route = '/', ...options }: Options = {}) {
  return render(
    <MemoryRouter initialEntries={[route]}>
      {ui}
    </MemoryRouter>,
    options,
  );
}

export { render };
