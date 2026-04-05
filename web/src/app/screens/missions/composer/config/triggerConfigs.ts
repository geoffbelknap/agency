import type { NodeDefinition } from '../canvasTypes';
import { registerNode } from '../nodeRegistry';

const scheduleNode: NodeDefinition = {
  typeId: 'trigger/schedule',
  category: 'trigger',
  label: 'Schedule',
  icon: 'Clock',
  ports: {
    inputs: [],
    outputs: [{ id: 'trigger-out', type: 'trigger', label: 'On Schedule' }],
  },
  configSchema: [
    { key: 'cron', label: 'Cron Expression', type: 'cron', required: true, placeholder: '0 9 * * 1-5' },
    { key: 'timezone', label: 'Timezone', type: 'text', placeholder: 'America/Los_Angeles' },
  ],
  serialize: (data) => ({
    triggers: [{ source: 'schedule', cron: data.cron, ...(data.timezone ? { timezone: data.timezone } : {}) }],
  }),
  validate: (data) => {
    const errors = [];
    if (!data.cron) errors.push({ field: 'cron', message: 'Cron expression is required' });
    return errors;
  },
};

const webhookNode: NodeDefinition = {
  typeId: 'trigger/webhook',
  category: 'trigger',
  label: 'Webhook',
  icon: 'Link',
  ports: {
    inputs: [],
    outputs: [{ id: 'trigger-out', type: 'trigger', label: 'On Webhook' }],
  },
  configSchema: [
    { key: 'name', label: 'Webhook Name', type: 'text', required: true, placeholder: 'my-webhook' },
    { key: 'event_type', label: 'Event Type Filter', type: 'text', placeholder: 'Optional' },
  ],
  serialize: (data) => ({
    triggers: [{ source: 'webhook', name: data.name, ...(data.event_type ? { event_type: data.event_type } : {}) }],
  }),
  validate: (data) => {
    const errors = [];
    if (!data.name) errors.push({ field: 'name', message: 'Webhook name is required' });
    return errors;
  },
};

const connectorEventNode: NodeDefinition = {
  typeId: 'trigger/connector-event',
  category: 'trigger',
  label: 'Connector Event',
  icon: 'Plug',
  ports: {
    inputs: [],
    outputs: [{ id: 'trigger-out', type: 'trigger', label: 'On Event' }],
  },
  configSchema: [
    { key: 'connector', label: 'Connector', type: 'text', required: true, placeholder: 'limacharlie' },
    { key: 'event_type', label: 'Event Type', type: 'text', required: true, placeholder: 'alert_created' },
    { key: 'match', label: 'Match Pattern', type: 'text', placeholder: 'Optional glob' },
  ],
  serialize: (data) => ({
    triggers: [{
      source: 'connector',
      connector: data.connector,
      event_type: data.event_type,
      ...(data.match ? { match: data.match } : {}),
    }],
  }),
  validate: (data) => {
    const errors = [];
    if (!data.connector) errors.push({ field: 'connector', message: 'Connector is required' });
    if (!data.event_type) errors.push({ field: 'event_type', message: 'Event type is required' });
    return errors;
  },
};

const channelPatternNode: NodeDefinition = {
  typeId: 'trigger/channel-pattern',
  category: 'trigger',
  label: 'Channel Message',
  icon: 'MessageSquare',
  ports: {
    inputs: [],
    outputs: [{ id: 'trigger-out', type: 'trigger', label: 'On Message' }],
  },
  configSchema: [
    { key: 'channel', label: 'Channel', type: 'text', required: true, placeholder: 'security-ops' },
    { key: 'match', label: 'Pattern', type: 'text', placeholder: 'hunt:*' },
  ],
  serialize: (data) => ({
    triggers: [{
      source: 'channel',
      channel: data.channel,
      ...(data.match ? { match: data.match } : {}),
    }],
  }),
  validate: (data) => {
    const errors = [];
    if (!data.channel) errors.push({ field: 'channel', message: 'Channel is required' });
    return errors;
  },
};

const platformEventNode: NodeDefinition = {
  typeId: 'trigger/platform-event',
  category: 'trigger',
  label: 'Platform Event',
  icon: 'Zap',
  ports: {
    inputs: [],
    outputs: [{ id: 'trigger-out', type: 'trigger', label: 'On Event' }],
  },
  configSchema: [
    { key: 'name', label: 'Event Name', type: 'text', required: true, placeholder: 'daily-digest' },
  ],
  serialize: (data) => ({
    triggers: [{ source: 'platform', name: data.name }],
  }),
  validate: (data) => {
    const errors = [];
    if (!data.name) errors.push({ field: 'name', message: 'Event name is required' });
    return errors;
  },
};

export function registerTriggerNodes(): void {
  registerNode(scheduleNode);
  registerNode(webhookNode);
  registerNode(connectorEventNode);
  registerNode(channelPatternNode);
  registerNode(platformEventNode);
}
