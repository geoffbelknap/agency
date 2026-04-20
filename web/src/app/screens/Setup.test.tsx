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

vi.mock('./setup/AgentStep', () => ({
  AgentStep: ({ onNext }: { onNext: () => void }) => (
    <button type="button" onClick={onNext}>Create agent</button>
  ),
}));

vi.mock('./setup/ProvidersStep', () => ({
  ProvidersStep: ({ onNext }: { onNext: () => void }) => (
    <button type="button" onClick={onNext}>Continue providers</button>
  ),
}));

vi.mock('./setup/StartingAgentStep', () => ({
  StartingAgentStep: ({ onReady }: { onReady: () => void }) => (
    <button type="button" onClick={onReady}>Agent ready</button>
  ),
}));

vi.mock('./setup/ChatStep', () => ({
  ChatStep: ({ onFinish }: { onFinish: (channelName?: string) => void }) => (
    <div>
      <button type="button" onClick={() => onFinish()}>Finish setup</button>
      <button type="button" onClick={() => onFinish('dm-henry')}>Finish to DM</button>
    </div>
  ),
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
  it('routes to overview when setup finishes without a channel', async () => {
    renderSetup();

    await userEvent.click(screen.getByRole('button', { name: 'Complete platform prep' }));
    await userEvent.click(screen.getByRole('button', { name: 'Continue providers' }));
    await userEvent.click(screen.getByRole('button', { name: 'Create agent' }));
    await userEvent.click(screen.getByRole('button', { name: 'Agent ready' }));
    await userEvent.click(screen.getByRole('button', { name: 'Finish setup' }));

    expect(screen.getByText('overview page')).toBeInTheDocument();
  });

  it('routes to the provided DM channel when setup finishes with a chat target', async () => {
    renderSetup();

    await userEvent.click(screen.getByRole('button', { name: 'Complete platform prep' }));
    await userEvent.click(screen.getByRole('button', { name: 'Continue providers' }));
    await userEvent.click(screen.getByRole('button', { name: 'Create agent' }));
    await userEvent.click(screen.getByRole('button', { name: 'Agent ready' }));
    await userEvent.click(screen.getByRole('button', { name: 'Finish to DM' }));

    expect(screen.getByText('channel page')).toBeInTheDocument();
  });
});
