import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { Setup } from './Setup';

vi.mock('./setup/PlatformReadyStep', () => ({
  PlatformReadyStep: ({ onComplete }: { onComplete: () => void }) => (
    <button type="button" onClick={onComplete}>Complete platform prep</button>
  ),
}));

vi.mock('./setup/WelcomeStep', () => ({
  WelcomeStep: ({ onSkip }: { onSkip: (channelName?: string) => void }) => (
    <div>
      <button type="button" onClick={() => onSkip()}>Skip setup</button>
      <button type="button" onClick={() => onSkip('dm-henry')}>Finish to DM</button>
    </div>
  ),
}));

vi.mock('./setup/ProvidersStep', () => ({
  ProvidersStep: () => <div>Providers step</div>,
}));

vi.mock('./setup/AgentStep', () => ({
  AgentStep: () => <div>Agent step</div>,
}));

vi.mock('./setup/CapabilitiesStep', () => ({
  CapabilitiesStep: () => <div>Capabilities step</div>,
}));

vi.mock('./setup/ChatStep', () => ({
  ChatStep: () => <div>Chat step</div>,
}));

function renderSetup() {
  const router = createMemoryRouter(
    [
      { path: '/setup', element: <Setup /> },
      { path: '/overview', element: <div>overview page</div> },
      { path: '/channels/:name', element: <div>channel page</div> },
    ],
    { initialEntries: ['/setup'] },
  );
  return { router, ...render(<RouterProvider router={router} />) };
}

describe('Setup', () => {
  it('routes to overview when setup is skipped without a channel', async () => {
    renderSetup();

    await userEvent.click(screen.getByRole('button', { name: 'Complete platform prep' }));
    await userEvent.click(screen.getByRole('button', { name: 'Skip setup' }));

    expect(screen.getByText('overview page')).toBeInTheDocument();
  });

  it('routes to the provided DM channel when setup finishes with a chat target', async () => {
    renderSetup();

    await userEvent.click(screen.getByRole('button', { name: 'Complete platform prep' }));
    await userEvent.click(screen.getByRole('button', { name: 'Finish to DM' }));

    expect(screen.getByText('channel page')).toBeInTheDocument();
  });
});
