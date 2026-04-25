export type AgentStatus = 'running' | 'stopped' | 'halted' | 'paused' | 'unhealthy';
export type AgentMode = 'assisted' | 'autonomous';
export type HealthStatus = 'healthy' | 'unhealthy' | 'idle' | 'starting' | 'stopping' | 'restarting';
export type ServiceState = 'running' | 'stopped' | 'missing' | 'created' | 'restarting' | 'starting' | 'stopping' | 'exited' | 'dead';
export type ComponentKind =
  | 'pack'
  | 'preset'
  | 'connector'
  | 'service'
  | 'mission'
  | 'skill'
  | 'workspace'
  | 'policy'
  | 'ontology'
  | 'provider'
  | 'setup';
export type MessageFlag = 'DECISION' | 'BLOCKER' | 'QUESTION' | null;
export type ConnectorState = 'active' | 'inactive';
export type WorkItemState = 'pending' | 'processing' | 'done' | 'failed';
export type CheckStatus = 'pass' | 'warn' | 'fail';
export type CapabilityKind = 'service' | 'tool' | 'integration' | 'provider-tool' | 'mcp-server' | 'skill';
export type CapabilityState = 'enabled' | 'available' | 'restricted' | 'disabled';

export interface AgentTask {
  task_id: string;
  content: string;
  timestamp: string;
  source?: string;
}

export interface Agent {
  id: string;
  name: string;
  status: AgentStatus;
  mode: AgentMode;
  type: string;
  preset: string;
  team: string;
  enforcerState: string;
  model?: string;
  role?: string;
  created?: string;
  uptime?: string;
  lastActive?: string;
  trustLevel?: number;
  restrictions?: string[];
  grantedCapabilities?: string[];
  currentTask?: AgentTask;
  mission?: string;
  missionStatus?: string;
  buildId?: string;
}

export interface Team {
  id: string;
  name: string;
  memberCount: number;
  created: string;
  members?: Agent[];
}

export interface InfrastructureService {
  id: string;
  name: string;
  state: ServiceState;
  health: HealthStatus;
  containerId: string;
  uptime: string;
}

export interface AuditEvent {
  id: string;
  timestamp: string;
  type: string;
  message: string;
  agentId?: string;
  agentName?: string;
}

export interface Channel {
  id: string;
  name: string;
  topic?: string;
  type?: string;
  state?: string;
  availability?: string;
  unreadCount: number;
  mentionCount: number;
  lastActivity: string;
  members: string[];
}

export interface Message {
  id: string;
  channelId: string;
  author: string;
  displayAuthor: string;
  isAgent: boolean;
  isSystem: boolean;
  isError?: boolean;
  timestamp: string;
  rawTimestamp?: string;
  content: string;
  flag: MessageFlag;
  parentId?: string;
  metadata?: Record<string, any>;
}

export interface Component {
  id: string;
  name: string;
  kind: ComponentKind;
  description: string;
  source: string;
  installed: boolean;
  installedAt?: string;
  version?: string;
}

export interface DoctorCheck {
  id: string;
  agentName?: string;
  name: string;
  scope?: string;
  backend?: string;
  status: CheckStatus;
  message: string;
  fix?: string;
}

export interface Connector {
  id: string;
  name: string;
  kind: string;
  source: string;
  state: ConnectorState;
  version?: string;
}

export interface WorkItem {
  id: string;
  connector: string;
  status: string;
  target_type?: string;
  target_name?: string;
  payload?: string;
  created_at: string;
  route_index?: number;
  priority?: number;
  brief_content?: string;
  // legacy fields kept for backward compat
  state?: WorkItemState;
  source?: string;
  summary?: string;
  created?: string;
}

export interface KnowledgeNode {
  id: string;
  topic: string;
  content: string;
  confidence: number;
  sourceAgent: string;
  timestamp: string;
}

export interface Capability {
  id: string;
  name: string;
  kind: CapabilityKind;
  state: CapabilityState;
  scopedAgents: string[];
  description?: string;
  spec?: Record<string, any>;
}

export type MissionStatus = 'unassigned' | 'active' | 'paused' | 'completed';

export interface MissionTrigger {
  source: string;
  connector?: string;
  channel?: string;
  eventType?: string;
  match?: string;
  name?: string;
  cron?: string;
}

export interface Mission {
  id: string;
  name: string;
  description: string;
  version: number;
  status: MissionStatus;
  assignedTo?: string;
  assignedType?: string;
  instructions?: string;
  triggers: MissionTrigger[];
  requires?: { capabilities?: string[]; channels?: string[] };
  budget?: { daily?: number; monthly?: number; perTask?: number };
  meeseeks?: boolean;
  meeseeksLimit?: number;
  meeseeksModel?: string;
  meeseeksBudget?: number;
  health?: { indicators?: string[]; businessHours?: string };
  cost_mode?: CostMode;
  min_task_tier?: TaskTier;
  reflection?: MissionReflection;
  success_criteria?: MissionSuccessCriteria;
  fallback?: MissionFallback;
  procedural_memory?: { capture: boolean; retrieve: boolean; max_retrieved: number };
  episodic_memory?: { capture: boolean; retrieve: boolean; max_retrieved: number; tool_enabled: boolean };
}

export type MeeseeksStatus = 'spawned' | 'working' | 'completed' | 'distressed' | 'terminated';

export interface Meeseeks {
  id: string;
  parentAgent: string;
  parentMissionId?: string;
  task: string;
  tools?: string[];
  model?: string;
  budget?: number;
  budgetUsed?: number;
  channel?: string;
  status: MeeseeksStatus;
  orphaned?: boolean;
  spawnedAt?: string;
  completedAt?: string;
}

export interface Deployment {
  id: string;
  packName: string;
  agentsCreated: string[];
  deployedAt: string;
}

export interface PlatformEvent {
  id: string;
  sourceType: string;
  sourceName: string;
  eventType: string;
  timestamp: string;
  data?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
}

// --- Agentic Design Patterns ---

export type CostMode = 'frugal' | 'balanced' | 'thorough';
export type TaskTier = 'minimal' | 'standard' | 'full';
export type MemoryOutcome = 'success' | 'partial' | 'failed';
export type EpisodeOutcome = 'success' | 'partial' | 'failed' | 'escalated';
export type OperationalTone = 'routine' | 'notable' | 'problematic';
export type AnomalySeverity = 'warning' | 'critical';

export interface ProcedureRecord {
  task_id: string;
  agent: string;
  mission_id: string;
  mission_name: string;
  task_type: string;
  timestamp: string;
  approach: string;
  tools_used: string[];
  outcome: MemoryOutcome;
  duration_minutes: number;
  lessons: string[];
}

export interface EpisodeRecord {
  task_id: string;
  agent: string;
  mission_name: string;
  timestamp: string;
  duration_minutes: number;
  outcome: EpisodeOutcome;
  summary: string;
  notable_events: string[];
  entities_mentioned: { type: string; name: string }[];
  operational_tone: OperationalTone;
  tags: string[];
}

export interface TrajectoryAnomaly {
  detector: string;
  detail: string;
  severity: AnomalySeverity;
  first_detected: string;
}

export interface TrajectoryDetector {
  status: string;
  last_fired: string | null;
}

export interface TrajectoryState {
  agent: string;
  enabled: boolean;
  window_size: number;
  current_entries: number;
  active_anomalies: TrajectoryAnomaly[];
  detectors: Record<string, TrajectoryDetector>;
}

export interface CriterionResult {
  id: string;
  passed: boolean;
  required: boolean;
  reasoning: string;
}

export interface EvaluationResult {
  task_id: string;
  passed: boolean;
  evaluation_mode: 'checklist_only' | 'llm' | 'checklist_only_fallback';
  model_used: string;
  criteria_results: CriterionResult[];
  evaluated_at: string;
}

export interface MissionReflection {
  enabled: boolean;
  max_rounds: number;
  criteria: string[];
}

export interface SuccessCriterionItem {
  id: string;
  description: string;
  required: boolean;
}

export interface MissionSuccessCriteria {
  checklist: SuccessCriterionItem[];
  evaluation: {
    enabled: boolean;
    mode: 'llm' | 'checklist_only';
    model: string;
    on_failure: 'flag' | 'retry' | 'block';
    max_retries: number;
  };
}

export type FallbackTrigger = 'tool_error' | 'capability_unavailable' | 'budget_warning' | 'consecutive_errors' | 'timeout' | 'no_progress';
export type FallbackAction = 'retry' | 'alternative_tool' | 'degrade' | 'simplify' | 'complete_partial' | 'pause_and_assess' | 'escalate';

export interface FallbackStrategyStep {
  action: FallbackAction;
  max_attempts?: number;
  backoff?: 'exponential' | 'fixed' | 'none';
  tool?: string;
  hint?: string;
  severity?: string;
  message?: string;
}

export interface FallbackPolicy {
  trigger: FallbackTrigger;
  tool?: string;
  capability?: string;
  threshold?: number;
  count?: number;
  strategy: FallbackStrategyStep[];
}

export interface MissionFallback {
  policies: FallbackPolicy[];
  default_policy: { strategy: FallbackStrategyStep[] };
}

export type ProfileType = 'operator' | 'agent';

export interface Profile {
  id: string;
  type: ProfileType;
  displayName: string;
  email?: string;
  avatarUrl?: string;
  bio?: string;
  status?: string;
  settings?: Record<string, unknown>;
  createdAt?: string;
  updatedAt?: string;
}

export type ProviderCategory = 'cloud' | 'local' | 'compatible';
export type TierStrategy = 'strict' | 'best_effort' | 'catch_all';

export interface Provider {
  name: string;
  display_name: string;
  description: string;
  category: ProviderCategory;
  quickstart_selectable?: boolean;
  quickstart_order?: number;
  quickstart_recommended?: boolean;
  quickstart_prompt_blurb?: string;
  installed: boolean;
  credential_name?: string;
  credential_label?: string;
  api_key_url?: string;
  api_base_configurable?: boolean;
  credential_configured: boolean;
}

export interface CapabilityTier {
  display_name: string;
  description: string;
  capabilities: string[];
}

export interface SetupConfig {
  capability_tiers: Record<string, CapabilityTier>;
}
