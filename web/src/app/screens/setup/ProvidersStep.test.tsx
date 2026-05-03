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
    let verified = false;
    let installed = false;
    const onProviderUpdate = vi.fn();

    server.use(
      http.get(`${BASE}/infra/providers`, () => HttpResponse.json([
        {
          name: 'provider-a',
          display_name: 'Provider A',
          description: 'Provider A models',
          category: 'cloud',
          quickstart_selectable: true,
          quickstart_order: 2,
          installed: false,
          credential_name: 'provider-a-api-key',
          credential_label: 'API Key',
          credential_configured: true,
        },
      ])),
      http.post(`${BASE}/infra/providers/provider-a/verify`, async ({ request }) => {
        const body = await request.json() as { api_key?: string };
        verified = true;
        expect(body.api_key).toBeUndefined();
        return HttpResponse.json({ ok: true, status: 200, message: 'OK' });
      }),
      http.post(`${BASE}/infra/providers/provider-a/install`, () => {
        installed = true;
        return HttpResponse.json({ status: 'installed', provider: 'provider-a' });
      }),
    );

    renderProviders({
      providers: { 'provider-a': { configured: true, validated: true } },
      onProviderUpdate,
    });

    await userEvent.click(await screen.findByText('Provider A'));
    await userEvent.click(screen.getByRole('button', { name: 'Use Existing Credential' }));

    await waitFor(() => {
      expect(installed).toBe(true);
      expect(onProviderUpdate).toHaveBeenCalledWith('provider-a', true, true);
    });
    expect(verified).toBe(true);
  });

  it('shows a validation message when a provider needs a key', async () => {
    server.use(
      http.get(`${BASE}/infra/providers`, () => HttpResponse.json([
        {
          name: 'provider-b',
          display_name: 'Provider B',
          description: 'Provider B models',
          category: 'cloud',
          quickstart_selectable: true,
          quickstart_order: 3,
          installed: false,
          credential_name: 'provider-b-api-key',
          credential_label: 'API Key',
          credential_configured: false,
        },
      ])),
    );

    renderProviders();

    await userEvent.click(await screen.findByText('Provider B'));
    await userEvent.click(screen.getByRole('button', { name: 'Verify & Save' }));

    expect(await screen.findByText('Enter API Key before verifying.')).toBeInTheDocument();
  });

  it('shows only quickstart-selectable providers in quickstart order', async () => {
    server.use(
      http.get(`${BASE}/infra/providers`, () => HttpResponse.json([
        {
          name: 'provider-b',
          display_name: 'Provider B',
          description: 'Provider B models',
          category: 'cloud',
          quickstart_selectable: true,
          quickstart_order: 3,
          installed: false,
          credential_configured: false,
        },
        {
          name: 'provider-a',
          display_name: 'Provider A',
          description: 'Provider A models',
          category: 'cloud',
          quickstart_selectable: true,
          quickstart_order: 1,
          quickstart_recommended: true,
          quickstart_prompt_blurb: 'recommended for alpha',
          installed: false,
          credential_configured: false,
        },
        {
          name: 'custom-hidden',
          display_name: 'Hidden Provider',
          description: 'Should not appear in setup',
          category: 'compatible',
          quickstart_selectable: false,
          installed: false,
          credential_configured: false,
        },
      ])),
    );

    renderProviders();

    expect(await screen.findByText('Provider A')).toBeInTheDocument();
    expect(screen.getByText('recommended for alpha')).toBeInTheDocument();
    expect(screen.queryByText('Hidden Provider')).not.toBeInTheDocument();

    const providerRows = screen.getAllByRole('button').filter((button) =>
      button.textContent?.includes('Provider A') || button.textContent?.includes('Provider B'));
    expect(providerRows[0]).toHaveTextContent('Provider A');
    expect(providerRows[1]).toHaveTextContent('Provider B');
  });

  it('verifies and installs configurable providers with an api_base override', async () => {
    let verifiedBase = '';
    let installedBase = '';

    server.use(
      http.get(`${BASE}/infra/providers`, () => HttpResponse.json([
        {
          name: 'ollama',
          display_name: 'Ollama',
          description: 'Run open models locally',
          category: 'local',
          quickstart_selectable: true,
          quickstart_order: 4,
          installed: false,
          api_base_configurable: true,
          credential_configured: true,
        },
      ])),
      http.post(`${BASE}/infra/providers/ollama/verify`, async ({ request }) => {
        const body = await request.json() as { api_base?: string };
        verifiedBase = body.api_base || '';
        return HttpResponse.json({ ok: true, status: 200, message: 'OK' });
      }),
      http.post(`${BASE}/infra/providers/ollama/install`, async ({ request }) => {
        const body = await request.json() as { api_base?: string };
        installedBase = body.api_base || '';
        return HttpResponse.json({ status: 'installed', provider: 'ollama' });
      }),
    );

    const onProviderUpdate = vi.fn();
    renderProviders({ onProviderUpdate });

    await userEvent.click(await screen.findByText('Ollama'));
    await userEvent.type(screen.getByLabelText('API Base URL'), 'http://127.0.0.1:11434/v1');
    await userEvent.click(screen.getByRole('button', { name: 'Use Existing Credential' }));

    await waitFor(() => {
      expect(verifiedBase).toBe('http://127.0.0.1:11434/v1');
      expect(installedBase).toBe('http://127.0.0.1:11434/v1');
      expect(onProviderUpdate).toHaveBeenCalledWith('ollama', true, true);
    });
  });
});
