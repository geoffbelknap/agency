export const AGENT_NAMES = [
  'ada', 'archie', 'atlas', 'bard', 'beacon', 'bolt', 'cipher', 'coral',
  'dash', 'echo', 'ember', 'felix', 'flint', 'forge', 'ghost', 'halo',
  'haven', 'hex', 'iris', 'juno', 'kite', 'lark', 'luna', 'mako',
  'maven', 'neo', 'nexus', 'nyx', 'onyx', 'orbit', 'pace', 'pax',
  'pixel', 'prism', 'quest', 'radar', 'raven', 'reef', 'sage', 'scout',
  'sigma', 'spark', 'storm', 'terra', 'trace', 'vale', 'vex', 'wren',
  'zara', 'zen',
];

export function randomAgentName(): string {
  return AGENT_NAMES[Math.floor(Math.random() * AGENT_NAMES.length)];
}
