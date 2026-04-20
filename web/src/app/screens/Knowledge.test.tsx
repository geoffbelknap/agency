import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Knowledge } from './Knowledge';

vi.mock('../lib/features', () => ({
  adminFeatureFlags: {
    graphAdmin: true,
  },
}));

const BASE = 'http://localhost:8200/api/v1';

function mockGraphAdminData({
  candidates = [],
  curationEntries = [],
  pending = [],
  memoryProposals = [],
  approvedMemories = [],
  quarantined = [],
  classification = { tiers: [] },
  principals = [],
  communities = { communities: [] },
  hubs = { hubs: [] },
}: {
  candidates?: unknown[];
  curationEntries?: unknown[];
  pending?: unknown[];
  memoryProposals?: unknown[];
  approvedMemories?: unknown[];
  quarantined?: unknown[];
  classification?: unknown;
  principals?: unknown;
  communities?: unknown;
  hubs?: unknown;
} = {}) {
  server.use(
    http.get(`${BASE}/graph/ontology/candidates`, () =>
      HttpResponse.json({ candidates }),
    ),
    http.get(`${BASE}/graph/curation-log`, () =>
      HttpResponse.json({ entries: curationEntries }),
    ),
    http.get(`${BASE}/graph/pending`, () =>
      HttpResponse.json({ pending }),
    ),
    http.get(`${BASE}/graph/memory/proposals`, () =>
      HttpResponse.json({ items: memoryProposals }),
    ),
    http.get(`${BASE}/graph/memory`, () =>
      HttpResponse.json({ items: approvedMemories }),
    ),
    http.get(`${BASE}/graph/quarantine`, () =>
      HttpResponse.json({ nodes: quarantined }),
    ),
    http.get(`${BASE}/graph/classification`, () =>
      HttpResponse.json(classification),
    ),
    http.get(`${BASE}/graph/principals`, () =>
      HttpResponse.json(principals),
    ),
    http.get(`${BASE}/graph/communities`, () =>
      HttpResponse.json(communities),
    ),
    http.get(`${BASE}/graph/hubs`, () =>
      HttpResponse.json(hubs),
    ),
  );
}

describe('Knowledge admin', () => {
  it('renders graph administration surfaces from graph governance APIs', async () => {
    server.use(
      http.get(`${BASE}/graph/stats`, () =>
        HttpResponse.json({ node_count: 42, edge_count: 100 }),
      ),
    );
    mockGraphAdminData({
      pending: [
        {
          id: 'pending-1',
          title: 'Promote release process',
          subject: 'release',
          proposed: 'runbook',
          agent: 'henry',
          confidence: 0.91,
        },
      ],
      memoryProposals: [
        {
          id: 'memory-1',
          summary: 'Use SEC primary filings first.',
          properties: JSON.stringify({
            memory_type: 'procedural',
            confidence: 'medium',
            agent: 'jarvis',
            channel: 'dm-jarvis',
            decision_reason: 'confidence is medium',
          }),
        },
      ],
      approvedMemories: [
        {
          id: 'approved-memory-1',
          summary: 'Operator prefers SEC EDGAR primary filings.',
          kind: 'procedure',
          properties: JSON.stringify({
            memory_type: 'procedural',
            agent: 'jarvis',
            channel: 'dm-jarvis',
            approved_by: 'knowledge_manager',
          }),
        },
      ],
      quarantined: [
        {
          id: 'quarantine-1',
          label: 'untrusted note',
          reason: 'source boundary mismatch',
        },
      ],
      candidates: [
        {
          id: 'candidate-1',
          value: 'field_report',
          count: 3,
          status: 'candidate',
        },
      ],
      curationEntries: [
        {
          id: 'decision-1',
          action: 'ontology_promote',
          node_id: 'candidate-2',
          detail: JSON.stringify({ value: 'governance_note' }),
          timestamp: '2026-04-09T10:10:00Z',
        },
      ],
      classification: { tiers: [{ tier: 'restricted', description: 'Operator-only facts' }] },
      principals: [{ uuid: 'agent:alice', type: 'agent', name: 'alice' }],
      communities: { communities: [{ id: 'community-1', label: 'Platform Ops', node_count: 4 }] },
      hubs: { hubs: [{ id: 'hub-1', label: 'release process', degree: 9 }] },
    });

    renderWithRouter(<Knowledge />);

    await waitFor(() => {
      expect(screen.getByText('42')).toBeInTheDocument();
      expect(screen.getByText('100')).toBeInTheDocument();
      expect(screen.getByText('Structural Review')).toBeInTheDocument();
      expect(screen.getByText('Memory Review')).toBeInTheDocument();
      expect(screen.getByText('Durable Memory')).toBeInTheDocument();
      expect(screen.getByText('Graph Topology')).toBeInTheDocument();
      expect(screen.getByText('Quarantine')).toBeInTheDocument();
      expect(screen.getByText('Ontology Review')).toBeInTheDocument();
    });

    expect(screen.getByText('Promote release process')).toBeInTheDocument();
    expect(screen.getByText('Use SEC primary filings first.')).toBeInTheDocument();
    expect(screen.getByText('Operator prefers SEC EDGAR primary filings.')).toBeInTheDocument();
    expect(screen.getByText('untrusted note')).toBeInTheDocument();
    expect(screen.getByText('field_report')).toBeInTheDocument();
    expect(screen.getByText('restricted')).toBeInTheDocument();
  });

  it('approves pending structural contributions through the review API', async () => {
    let reviewed: { id?: string; action?: string } = {};
    server.use(
      http.get(`${BASE}/graph/stats`, () =>
        HttpResponse.json({ node_count: 0, edge_count: 0 }),
      ),
      http.post(`${BASE}/graph/review/:id`, async ({ params, request }) => {
        const body = await request.json() as { action?: string };
        reviewed = { id: String(params.id), action: body.action };
        return HttpResponse.json({ ok: true });
      }),
    );
    mockGraphAdminData({
      pending: [{ id: 'pending-approve', title: 'Approve me' }],
    });

    renderWithRouter(<Knowledge />);

    await screen.findByText('Approve me');
    await userEvent.click(screen.getByRole('button', { name: /^approve$/i }));

    await waitFor(() => {
      expect(reviewed).toEqual({ id: 'pending-approve', action: 'approve' });
    });
  });

  it('approves memory proposals through the memory review API', async () => {
    let reviewed: { id?: string; action?: string } = {};
    server.use(
      http.get(`${BASE}/graph/stats`, () =>
        HttpResponse.json({ node_count: 0, edge_count: 0 }),
      ),
      http.post(`${BASE}/graph/memory/proposals/:id/review`, async ({ params, request }) => {
        const body = await request.json() as { action?: string };
        reviewed = { id: String(params.id), action: body.action };
        return HttpResponse.json({ ok: true });
      }),
    );
    mockGraphAdminData({
      memoryProposals: [{ id: 'memory-approve', summary: 'Remember primary filings', properties: { memory_type: 'procedural', confidence: 'medium' } }],
    });

    renderWithRouter(<Knowledge />);

    await screen.findByText('Remember primary filings');
    const approveButtons = screen.getAllByRole('button', { name: /^approve$/i });
    await userEvent.click(approveButtons[0]);

    await waitFor(() => {
      expect(reviewed).toEqual({ id: 'memory-approve', action: 'approve' });
    });
  });

  it('revokes approved durable memory through the memory action API', async () => {
    let actioned: { id?: string; action?: string } = {};
    server.use(
      http.get(`${BASE}/graph/stats`, () =>
        HttpResponse.json({ node_count: 0, edge_count: 0 }),
      ),
      http.post(`${BASE}/graph/memory/:id/actions`, async ({ params, request }) => {
        const body = await request.json() as { action?: string };
        actioned = { id: String(params.id), action: body.action };
        return HttpResponse.json({ ok: true });
      }),
    );
    mockGraphAdminData({
      approvedMemories: [{ id: 'approved-memory-revoke', summary: 'Old SEC preference', properties: { memory_type: 'procedural', agent: 'jarvis' } }],
    });

    renderWithRouter(<Knowledge />);

    await screen.findByText('Old SEC preference');
    await userEvent.click(screen.getByRole('button', { name: /^revoke$/i }));

    await waitFor(() => {
      expect(actioned).toEqual({ id: 'approved-memory-revoke', action: 'revoke' });
    });
  });

  it('does not expose feature-tab search affordances in admin', async () => {
    server.use(
      http.get(`${BASE}/graph/stats`, () =>
        HttpResponse.json({ node_count: 0, edge_count: 0 }),
      ),
    );
    mockGraphAdminData();

    renderWithRouter(<Knowledge />);

    await waitFor(() => {
      expect(screen.getByLabelText('Knowledge metrics')).toBeInTheDocument();
    });
    expect(screen.queryByPlaceholderText(/search topics and content/i)).not.toBeInTheDocument();
    expect(screen.queryByText('Who knows')).not.toBeInTheDocument();
  });
});
