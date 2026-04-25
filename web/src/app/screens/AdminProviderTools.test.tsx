import { describe, expect, it } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { AdminProviderTools } from './AdminProviderTools';

const BASE = 'http://localhost:8200/api/v1';

const inventory = {
  version: '0.1',
  capabilities: {
    'provider-web-search': {
      title: 'Web search',
      risk: 'medium',
      default_grant: true,
      execution: 'provider_hosted',
      description: 'Provider-side web search or search grounding.',
      providers: {
        'provider-a': {
          status: 'supported',
          request_tools: ['search'],
          pricing: { unit: 'tool_call', confidence: 'unknown' },
          tests: ['detect', 'grant_deny'],
        },
        'provider-b': {
          status: 'supported',
          request_tools: ['search_20250305'],
          pricing: { unit: 'search', usd_per_unit: 0.01, confidence: 'exact' },
          tests: ['detect', 'normalize_generic', 'grant_deny'],
        },
        'provider-c': {
          status: 'supported',
          request_tools: ['grounded_search'],
          pricing: { unit: 'grounded_request', confidence: 'unknown' },
          tests: ['detect'],
        },
      },
    },
    'provider-computer-use': {
      title: 'Computer use',
      risk: 'critical',
      default_grant: false,
      execution: 'agency_harnessed',
      description: 'Provider-defined computer-use loop.',
      providers: {
        'provider-a': {
          status: 'unsupported_by_agency',
          request_tools: ['computer_use_preview'],
          pricing: { unit: 'harness_action', confidence: 'unknown' },
          tests: ['detect', 'unsupported_by_agency'],
        },
        'provider-b': {
          status: 'unsupported_by_agency',
          request_tools: ['computer_20250124'],
          pricing: { unit: 'harness_action', confidence: 'unknown' },
          tests: ['detect', 'unsupported_by_agency'],
        },
        'provider-c': {
          status: 'unconfirmed',
          pricing: { unit: 'harness_action', confidence: 'unknown' },
          tests: ['inventory_only'],
        },
      },
    },
    'provider-shell': {
      title: 'Shell',
      risk: 'critical',
      default_grant: false,
      execution: 'agency_harnessed',
      description: 'Provider-defined shell action proposal.',
      providers: {
        'provider-a': {
          status: 'harnessed',
          request_tools: ['shell'],
          pricing: { unit: 'harness_action', confidence: 'unknown' },
          tests: ['detect', 'harness_translate'],
        },
        'provider-b': {
          status: 'harnessed',
          request_tools: ['bash_20250124'],
          pricing: { unit: 'harness_action', confidence: 'unknown' },
          tests: ['detect', 'harness_translate'],
        },
        'provider-c': {
          status: 'no_equivalent',
        },
      },
    },
  },
};

describe('AdminProviderTools', () => {
  it('renders provider tool matrix from inventory', async () => {
    server.use(http.get(`${BASE}/infra/provider-tools`, () => HttpResponse.json(inventory)));

    renderWithRouter(<AdminProviderTools />);

    expect(await screen.findByText('Web search')).toBeInTheDocument();
    expect(screen.getByText('Computer use')).toBeInTheDocument();
    expect(screen.getByText('provider-web-search')).toBeInTheDocument();
    expect(screen.getByText('provider-computer-use')).toBeInTheDocument();
    expect(screen.getAllByText('supported').length).toBeGreaterThanOrEqual(3);
    expect(screen.getAllByText('harnessed').length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText('unsupported by agency').length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText('default grant')).toBeInTheDocument();
    expect(screen.getByText('exact · $0.0100 · search')).toBeInTheDocument();
  });

  it('filters tools by provider metadata', async () => {
    server.use(http.get(`${BASE}/infra/provider-tools`, () => HttpResponse.json(inventory)));

    renderWithRouter(<AdminProviderTools />);

    await screen.findByText('Web search');
    await userEvent.type(screen.getByPlaceholderText('Filter tools...'), 'computer');

    await waitFor(() => {
      expect(screen.getByText('Computer use')).toBeInTheDocument();
      expect(screen.queryByText('Web search')).not.toBeInTheDocument();
    });
  });
});
