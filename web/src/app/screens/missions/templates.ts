import type { WizardState } from './serialize';

export interface MissionTemplate {
  id: string;
  icon: string;
  label: string;
  description: string;
  defaults: Partial<WizardState>;
}

export const BUILT_IN_TEMPLATES: MissionTemplate[] = [
  {
    id: 'channel-monitor',
    icon: '📡',
    label: 'Channel Monitor',
    description: 'Watch a channel and respond to messages',
    defaults: {
      instructions: 'Monitor the specified channel for messages matching the trigger pattern.\nWhen a matching message arrives:\n1. Read the message context\n2. Take appropriate action based on the content\n3. Report back in the channel with your findings',
      triggers: [{ source: 'channel', channel: '', match: '' }],
    },
  },
  {
    id: 'scheduled-report',
    icon: '📊',
    label: 'Scheduled Report',
    description: 'Run periodic tasks on a schedule',
    defaults: {
      instructions: 'On each scheduled trigger:\n1. Gather the relevant data from available sources\n2. Analyze trends and notable changes\n3. Compile a summary report\n4. Post the report to the designated channel',
      triggers: [{ source: 'schedule', cron: '0 9 * * 1-5', name: 'daily-report' }],
    },
  },
  {
    id: 'webhook-handler',
    icon: '🔗',
    label: 'Webhook Handler',
    description: 'Process incoming webhook events',
    defaults: {
      instructions: 'When a webhook event arrives:\n1. Parse the event payload\n2. Determine the appropriate response action\n3. Execute the action using available tools\n4. Log the outcome',
      triggers: [{ source: 'webhook', name: '', event_type: '' }],
    },
  },
  {
    id: 'intake-processor',
    icon: '📥',
    label: 'Intake Processor',
    description: 'Process work items from the intake queue',
    defaults: {
      instructions: 'Monitor the intake queue for new work items.\nFor each item:\n1. Review the item summary and payload\n2. Classify priority and type\n3. Take the appropriate action\n4. Update the item status when complete',
      triggers: [{ source: 'connector', connector: '', event_type: 'intake.new' }],
    },
  },
];
