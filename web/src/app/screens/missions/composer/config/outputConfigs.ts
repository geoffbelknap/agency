import type { NodeDefinition } from '../canvasTypes';
import { registerNode } from '../nodeRegistry';

const channelPostNode: NodeDefinition = {
  typeId: 'output/channel-post',
  category: 'output',
  label: 'Channel Post',
  icon: 'Hash',
  ports: {
    inputs: [{ id: 'output-in', type: 'output', label: 'Results' }],
    outputs: [],
  },
  configSchema: [
    { key: 'channel', label: 'Channel', type: 'text', required: true, placeholder: 'security-findings' },
  ],
  serialize: (data) => ({
    requires: { channels: [data.channel as string] },
  }),
  validate: (data) => {
    if (!data.channel) return [{ field: 'channel', message: 'Channel is required' }];
    return [];
  },
};

const webhookCallNode: NodeDefinition = {
  typeId: 'output/webhook-call',
  category: 'output',
  label: 'Webhook Call',
  icon: 'Send',
  ports: {
    inputs: [{ id: 'output-in', type: 'output', label: 'Results' }],
    outputs: [],
  },
  configSchema: [
    { key: 'url', label: 'URL', type: 'text', required: true, placeholder: 'https://hooks.example.com/...' },
  ],
  serialize: () => ({}),
  validate: (data) => {
    if (!data.url) return [{ field: 'url', message: 'URL is required' }];
    return [];
  },
};

const escalationNode: NodeDefinition = {
  typeId: 'output/escalation',
  category: 'output',
  label: 'Escalation',
  icon: 'AlertTriangle',
  ports: {
    inputs: [{ id: 'output-in', type: 'output', label: 'Escalation' }],
    outputs: [],
  },
  configSchema: [
    { key: 'severity', label: 'Severity', type: 'select', required: true, options: [
      { value: 'info', label: 'Info' },
      { value: 'warn', label: 'Warning' },
      { value: 'error', label: 'Error' },
      { value: 'critical', label: 'Critical' },
    ]},
    { key: 'message', label: 'Message Template', type: 'textarea', placeholder: 'Escalation reason...' },
  ],
  serialize: () => ({}),
  validate: (data) => {
    if (!data.severity) return [{ field: 'severity', message: 'Severity is required' }];
    return [];
  },
};

export function registerOutputNodes(): void {
  registerNode(channelPostNode);
  registerNode(webhookCallNode);
  registerNode(escalationNode);
}
