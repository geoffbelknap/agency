import type { NodeDefinition } from '../canvasTypes';
import { registerNode } from '../nodeRegistry';

const hubConnectorNode: NodeDefinition = {
  typeId: 'hub/connector',
  category: 'hub',
  label: 'Connector',
  icon: 'Plug2',
  ports: {
    inputs: [],
    outputs: [{ id: 'trigger-out', type: 'trigger', label: 'Events' }],
  },
  configSchema: [
    { key: 'instance', label: 'Connector Instance', type: 'text', required: true, placeholder: 'limacharlie' },
    { key: 'event_type', label: 'Event Type Filter', type: 'text', placeholder: 'alert_created' },
  ],
  serialize: (data) => ({
    triggers: [{
      source: 'connector',
      connector: data.instance as string,
      ...(data.event_type ? { event_type: data.event_type } : {}),
    }],
  }),
  validate: (data) => {
    if (!data.instance) return [{ field: 'instance', message: 'Connector instance is required' }];
    return [];
  },
};

const hubSkillNode: NodeDefinition = {
  typeId: 'hub/skill',
  category: 'hub',
  label: 'Skill',
  icon: 'Wrench',
  ports: {
    inputs: [],
    outputs: [{ id: 'modifier-out', type: 'modifier', label: 'Capability' }],
  },
  configSchema: [
    { key: 'skill', label: 'Skill Name', type: 'text', required: true, placeholder: 'code-review' },
  ],
  serialize: (data) => ({
    requires: { capabilities: [data.skill as string] },
  }),
  validate: (data) => {
    if (!data.skill) return [{ field: 'skill', message: 'Skill name is required' }];
    return [];
  },
};

export function registerHubNodes(): void {
  registerNode(hubConnectorNode);
  registerNode(hubSkillNode);
}
