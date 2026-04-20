import { describe, it, expect, vi } from 'vitest';
import type { ComponentProps } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../../test/server';
import { ProvidersStep } from './ProvidersStep';

const BASE = 'http://localhost:8200/api/v1';

function renderProviders(overrides?: Partial<ComponentProps<typeof ProvidersStep>>) {
  const props: ComponentProps<typeof ProvidersStep> = {
    providers: {},
    onProviderUpdate: vi.fn(),
    onNext: vi.fn(),
    onBack: vi.fn(),
    ...overrides,
  };
  render(<ProvidersStep {...props} />);
  return props;
}

describe('ProvidersStep', () => {
  it('uses an existing configured provider without testing the catalog credential alias', async () => {
    let credentialTested = false;
    let installed = false;
    const onProviderUpdate = vi.fn();

    server.use(
      http.get(`${BASE}/infra/providers`, () => HttpResponse.json([
        {
          name: 'anthropic',
          display_name: 'Anthropic',
          description: 'Claude models',
          category: 'cloud',
          installed: false,
          credential_name: 'anthropic-api-key',
          credential_label: 'API Key',
          credential_configured: true,
        },
      ])),
      http.post(`${BASE}/creds/anthropic-api-key/test`, () => {
        credentialTested = true;
        return HttpResponse.json({ ok: false, message: 'credential "anthropic-api-key" not found' });
      }),
      http.post(`${BASE}/infra/providers/anthropic/install`, () => {
        installed = true;
        return HttpResponse.json({ status: 'installed', provider: 'anthropic' });
      }),
    );

    renderProviders({
      providers: { anthropic: { configured: true, validated: true } },
      onProviderUpdate,
    });

    await userEvent.click(await screen.findByText('Anthropic'));
    await userEvent.click(screen.getByRole('button', { name: 'Use Existing Credential' }));

    await waitFor(() => {
      expect(installed).toBe(true);
      expect(onProviderUpdate).toHaveBeenCalledWith('anthropic', true, true);
    });
    expect(credentialTested).toBe(false);
  });

  it('shows a validation message when a provider needs a key', async () => {
    server.use(
      http.get(`${BASE}/infra/providers`, () => HttpResponse.json([
        {
          name: 'openai',
          display_name: 'OpenAI',
          description: 'GPT models',
          category: 'cloud',
          installed: false,
          credential_name: 'openai-api-key',
          credential_label: 'API Key',
          credential_configured: false,
        },
      ])),
    );

    renderProviders();

    await userEvent.click(await screen.findByText('OpenAI'));
    await userEvent.click(screen.getByRole('button', { name: 'Verify & Save' }));

    expect(await screen.findByText('Enter API Key before verifying.')).toBeInTheDocument();
  });

  it('explains why OpenAI-compatible cannot be completed from this setup step yet', async () => {
    server.use(
      http.get(`${BASE}/infra/providers`, () => HttpResponse.json([
        {
          name: 'openai-compatible',
          display_name: 'OpenAI-Compatible',
          description: 'Connect a compatible OpenAI-style endpoint with custom base URL',
          category: 'compatible',
          installed: false,
          credential_name: 'openai-compatible-api-key',
          credential_label: 'API Key',
          api_base_configurable: true,
          credential_configured: false,
        },
      ])),
    );

    renderProviders();

    await userEvent.click(await screen.findByText('OpenAI-Compatible'));
    await userEvent.click(screen.getByRole('button', { name: 'Setup info' }));

    expect(await screen.findByText(/This setup step cannot save that mapping yet/i)).toBeInTheDocument();
  });
});
