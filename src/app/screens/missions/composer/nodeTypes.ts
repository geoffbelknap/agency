// Node type components are typed with NodeProps<CanvasNodeData> which is
// narrower than the generic NodeTypes expects. We export the map untyped
// and let ReactFlow handle the runtime dispatch.
import { TriggerNode } from './nodes/TriggerNode';
import { AgentNode } from './nodes/AgentNode';
import { OutputNode } from './nodes/OutputNode';
import { ModifierNode } from './nodes/ModifierNode';
import { HubNode } from './nodes/HubNode';

// Register all node definitions on import
import { registerTriggerNodes } from './config/triggerConfigs';
import { registerAgentNode } from './config/agentConfig';
import { registerOutputNodes } from './config/outputConfigs';
import { registerModifierNodes } from './config/modifierConfigs';
import { registerHubNodes } from './config/hubConfigs';

registerTriggerNodes();
registerAgentNode();
registerOutputNodes();
registerModifierNodes();
registerHubNodes();

// Map category prefixes to render components
export const composerNodeTypes = {
  'trigger/schedule': TriggerNode,
  'trigger/webhook': TriggerNode,
  'trigger/connector-event': TriggerNode,
  'trigger/channel-pattern': TriggerNode,
  'trigger/platform-event': TriggerNode,
  'agent': AgentNode,
  'output/channel-post': OutputNode,
  'output/webhook-call': OutputNode,
  'output/escalation': OutputNode,
  'modifier/fallback-policy': ModifierNode,
  'modifier/success-criteria': ModifierNode,
  'modifier/reflection': ModifierNode,
  'modifier/budget-limits': ModifierNode,
  'hub/connector': HubNode,
  'hub/skill': HubNode,
};
