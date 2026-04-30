import { describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../../test/server';
import { render } from '../../../test/render';
import { ProvidersStep } from './ProvidersStep';

const BASE = 'http://localhost:8200/api/v1';

describe('ProvidersStep', () => {
  it('derives a provider-local credential name when the provider metadata is wrong', async () => {
    let storedName = '';
    let testedOpenAI = false;
    let testedAnthropic = false;
    const onProviderUpdate = vi.fn();

    server.use(
      http.get(`${BASE}/infra/providers`, () =>
        HttpResponse.json([
          {
            name: 'openai',
            display_name: 'OpenAI',
            description: 'OpenAI models',
            category: 'cloud',
            installed: false,
            credential_name: 'anthropic-api-key',
            credential_label: 'API Key',
            credential_configured: false,
          },
        ]),
      ),
      http.post(`${BASE}/creds`, async ({ request }) => {
        const body = await request.json() as any;
        storedName = body.name;
        return HttpResponse.json({ status: 'ok', name: body.name });
      }),
      http.post(`${BASE}/creds/openai-api-key/test`, () => {
        testedOpenAI = true;
        return HttpResponse.json({ ok: true });
      }),
      http.post(`${BASE}/creds/anthropic-api-key/test`, () => {
        testedAnthropic = true;
        return HttpResponse.json({ ok: false, message: 'wrong credential' });
      }),
      http.post(`${BASE}/infra/providers/openai/install`, () =>
        HttpResponse.json({ status: 'installed', provider: 'openai' }),
      ),
    );

    render(
      <ProvidersStep
        providers={{}}
        tierStrategy="best_effort"
        onProviderUpdate={onProviderUpdate}
        onTierStrategyUpdate={() => {}}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );

    await userEvent.click(await screen.findByRole('button', { name: /openai/i }));
    await userEvent.type(screen.getByPlaceholderText(/enter your api key/i), 'sk-test');
    await userEvent.click(screen.getByRole('button', { name: /verify & save/i }));

    await waitFor(() => {
      expect(storedName).toBe('openai-api-key');
      expect(testedOpenAI).toBe(true);
      expect(testedAnthropic).toBe(false);
      expect(onProviderUpdate).toHaveBeenCalledWith('openai', true, true);
    });
  });
});
