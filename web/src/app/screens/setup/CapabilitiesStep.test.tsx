import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../../test/server';
import { CapabilitiesStep } from './CapabilitiesStep';

const BASE = 'http://localhost:8200/api/v1';

describe('CapabilitiesStep', () => {
  it('applies standard defaults with provider web tools', async () => {
    const enabled: string[] = [];
    const onUpdate = vi.fn();
    const onNext = vi.fn();

    server.use(
      http.get(`${BASE}/admin/capabilities`, () => HttpResponse.json([
        { name: 'provider-web-fetch', state: 'disabled' },
        { name: 'provider-web-search', state: 'disabled' },
        { name: 'custom-tool', state: 'disabled' },
      ])),
      http.get(`${BASE}/infra/setup/config`, () => HttpResponse.json({
        capability_tiers: {
          standard: { capabilities: ['custom-tool'] },
        },
      })),
      http.post(`${BASE}/admin/capabilities/:name/enable`, ({ params }) => {
        enabled.push(String(params.name));
        return HttpResponse.json({ ok: true });
      }),
    );

    render(<CapabilitiesStep onUpdate={onUpdate} onNext={onNext} onBack={() => {}} />);

    expect(await screen.findByText('provider-web-fetch')).toBeInTheDocument();
    expect(screen.getByText('provider-web-search')).toBeInTheDocument();
    expect(screen.getByText('custom-tool')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => {
      expect(enabled.sort()).toEqual(['custom-tool', 'provider-web-fetch', 'provider-web-search'].sort());
      expect(onUpdate).toHaveBeenCalledWith(['custom-tool', 'provider-web-fetch', 'provider-web-search']);
      expect(onNext).toHaveBeenCalled();
    });
  });

  it('skips provider defaults that are not present in the registry', async () => {
    const onUpdate = vi.fn();

    server.use(
      http.get(`${BASE}/admin/capabilities`, () => HttpResponse.json([
        { name: 'provider-web-search', state: 'available' },
      ])),
      http.get(`${BASE}/infra/setup/config`, () => HttpResponse.json({
        capability_tiers: {
          standard: { capabilities: [] },
        },
      })),
    );

    render(<CapabilitiesStep onUpdate={onUpdate} onNext={() => {}} onBack={() => {}} />);

    expect(await screen.findByText('provider-web-search')).toBeInTheDocument();
    expect(screen.getByText(/Not available in this registry: provider-web-fetch/i)).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => {
      expect(onUpdate).toHaveBeenCalledWith(['provider-web-search']);
    });
  });
});
