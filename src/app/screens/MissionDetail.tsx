import { useState, useEffect, useCallback, useMemo } from 'react';
import { MissionQualityTab } from './missions/MissionQualityTab';
import { MissionHealthTab } from './missions/MissionHealthTab';
import { useParams, useNavigate } from 'react-router';
import {
  ArrowLeft,
  Play,
  Pause,
  CheckCircle,
  Trash2,
  Pencil,
  ChevronDown,
  ChevronRight,
  RefreshCw,
  Workflow,
} from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '../components/ui/button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { JsonView } from '../components/JsonView';
import { api, type RawMission } from '../lib/api';
import { parseFromRaw, serializeToYaml } from './missions/serialize';
import { InlineEditField } from './missions/InlineEditField';
import { MissionWizard } from './MissionWizard';

const statusColors: Record<string, string> = {
  active: 'bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-400',
  paused: 'bg-amber-50 dark:bg-amber-950 text-amber-700 dark:text-amber-400',
  completed: 'bg-blue-50 dark:bg-blue-950 text-blue-700 dark:text-blue-400',
};

const triggerIcons: Record<string, string> = {
  channel: '\u{1F4AC}',
  schedule: '\u23F0',
  webhook: '\u{1F517}',
  connector: '\u{1F50C}',
  platform: '\u26A1',
};

const MISSION_TABS = ['overview', 'quality', 'health'] as const;

function StatusBadge({ status }: { status: string }) {
  const colors = statusColors[status] || 'bg-secondary text-muted-foreground';
  return (
    <span className={`inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium ${colors}`}>
      {status}
    </span>
  );
}

function triggerSummary(t: RawMission['triggers'] extends (infer T)[] | undefined ? T : never): string {
  const parts: string[] = [];
  if (t.channel) parts.push(t.channel);
  if (t.connector) parts.push(t.connector);
  if (t.cron) parts.push(`cron: ${t.cron}`);
  if (t.event_type) parts.push(t.event_type);
  if (t.match) parts.push(`match: ${t.match}`);
  if (t.name) parts.push(t.name);
  return parts.join(' \u00B7 ');
}

export function MissionDetail() {
  const { name } = useParams<{ name: string }>();
  const navigate = useNavigate();

  const [mission, setMission] = useState<RawMission | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [activeTab, setActiveTab] = useState<'overview' | 'quality' | 'health'>('overview');

  // Wizard
  const [wizardOpen, setWizardOpen] = useState(false);

  // Delete dialog
  const [deleteOpen, setDeleteOpen] = useState(false);

  // Assign form
  const [showAssignForm, setShowAssignForm] = useState(false);
  const [assignTarget, setAssignTarget] = useState('');
  const [assignType, setAssignType] = useState<'agent' | 'team'>('agent');

  // YAML section
  const [yamlOpen, setYamlOpen] = useState(false);
  const [yamlEditing, setYamlEditing] = useState(false);
  const [yamlDraft, setYamlDraft] = useState('');

  // History section
  const [historyOpen, setHistoryOpen] = useState(false);
  const [history, setHistory] = useState<Record<string, unknown>[] | null>(null);
  const [historyLoading, setHistoryLoading] = useState(false);

  const load = useCallback(async () => {
    if (!name) return;
    try {
      const data = await api.missions.show(name);
      setMission(data);
      setError(null);
    } catch (e: any) {
      setError(e.message || 'Failed to load mission');
    } finally {
      setLoading(false);
    }
  }, [name]);

  useEffect(() => {
    load();
  }, [load]);

  // Load history lazily
  useEffect(() => {
    if (historyOpen && !history && !historyLoading && name) {
      setHistoryLoading(true);
      api.missions.history(name)
        .then((data) => setHistory(data ?? []))
        .catch((e) => toast.error('Failed to load history: ' + (e.message || 'Unknown error')))
        .finally(() => setHistoryLoading(false));
    }
  }, [historyOpen, history, historyLoading, name]);

  // Inline field save helper: parse -> update field -> serialize -> PUT
  const saveField = useCallback(
    async (field: 'description' | 'instructions', value: string) => {
      if (!mission || !name) return;
      const state = parseFromRaw(mission);
      state[field] = value;
      const yaml = serializeToYaml(state);
      await api.missions.update(name, yaml);
      toast.success(`${field.charAt(0).toUpperCase() + field.slice(1)} updated`);
      await load();
    },
    [mission, name, load],
  );

  // Lifecycle actions
  const handlePause = async () => {
    if (!name) return;
    try {
      await api.missions.pause(name);
      toast.success('Mission paused');
      await load();
    } catch (e: any) {
      toast.error(e.message || 'Failed to pause');
    }
  };

  const handleResume = async () => {
    if (!name) return;
    try {
      await api.missions.resume(name);
      toast.success('Mission resumed');
      await load();
    } catch (e: any) {
      toast.error(e.message || 'Failed to resume');
    }
  };

  const handleComplete = async () => {
    if (!name) return;
    try {
      await api.missions.complete(name);
      toast.success('Mission completed');
      await load();
    } catch (e: any) {
      toast.error(e.message || 'Failed to complete');
    }
  };

  const handleDelete = async () => {
    if (!name) return;
    try {
      await api.missions.delete(name);
      toast.success('Mission deleted');
      navigate('/missions');
    } catch (e: any) {
      toast.error(e.message || 'Failed to delete');
    }
  };

  const handleAssign = async () => {
    if (!name || !assignTarget.trim()) return;
    try {
      await api.missions.assign(name, assignTarget.trim(), assignType);
      toast.success(`Mission assigned to ${assignTarget.trim()}`);
      setShowAssignForm(false);
      setAssignTarget('');
      await load();
    } catch (e: any) {
      toast.error(e.message || 'Failed to assign');
    }
  };

  const handleYamlSave = async () => {
    if (!name) return;
    try {
      await api.missions.update(name, yamlDraft);
      toast.success('Mission updated from YAML');
      setYamlEditing(false);
      await load();
    } catch (e: any) {
      toast.error(e.message || 'Failed to save YAML');
    }
  };

  const yamlPreview = useMemo(
    () => (yamlOpen && mission) ? serializeToYaml(parseFromRaw(mission)) : '',
    [mission, yamlOpen],
  );

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64 text-muted-foreground">
        Loading mission…
      </div>
    );
  }

  if (error || !mission) {
    return (
      <div className="flex flex-col items-center justify-center h-64 gap-4">
        <p className="text-muted-foreground">{error || 'Mission not found'}</p>
        <Button variant="outline" size="sm" onClick={() => navigate('/missions')}>
          <ArrowLeft className="h-4 w-4 mr-1" /> Back to Missions
        </Button>
      </div>
    );
  }
  const { budget, health, requires, triggers } = mission;

  const budgetParts: string[] = [];
  if (budget?.daily != null) budgetParts.push(`$${budget.daily}/day`);
  if (budget?.monthly != null) budgetParts.push(`$${budget.monthly}/month`);
  if (budget?.per_task != null) budgetParts.push(`$${budget.per_task}/task`);

  return (
    <div className="flex flex-col h-full">
      {/* Back link */}
      <div className="px-4 md:px-8 pt-4">
        <button
          className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors"
          onClick={() => navigate('/missions')}
        >
          <ArrowLeft className="h-3.5 w-3.5" />
          Missions
        </button>
      </div>

      {/* Header */}
      <div className="flex items-center justify-between border-b px-4 md:px-8 py-4">
        <div className="flex items-center gap-3">
          <code className="text-lg font-semibold">{mission.name}</code>
          <StatusBadge status={mission.status} />
          {mission.cost_mode && (
            <span
              className={`inline-flex items-center rounded-md px-2 py-0.5 text-[10px] font-medium ${
                mission.cost_mode === 'frugal' ? 'bg-secondary text-muted-foreground' :
                mission.cost_mode === 'balanced' ? 'bg-blue-100 text-blue-700 dark:bg-primary/20 dark:text-primary' :
                'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400'
              }`}
              title={
                mission.cost_mode === 'frugal' ? 'Minimize cost — reflection off, no evaluation' :
                mission.cost_mode === 'balanced' ? 'Default tradeoffs — reflection, checklist eval' :
                'Full quality — LLM evaluation, full reflection'
              }
            >
              {mission.cost_mode}
            </span>
          )}
          {mission.version != null && (
            <span className="text-xs text-muted-foreground">v{mission.version}</span>
          )}
        </div>
        <div className="flex items-center gap-2">
          {/* Lifecycle buttons */}
          {mission.status === 'active' && (
            <>
              <Button variant="outline" size="sm" onClick={handlePause}>
                <Pause className="h-4 w-4 mr-1" /> Pause
              </Button>
              <Button variant="outline" size="sm" onClick={handleComplete}>
                <CheckCircle className="h-4 w-4 mr-1" /> Complete
              </Button>
            </>
          )}
          {mission.status === 'paused' && (
            <Button variant="outline" size="sm" onClick={handleResume}>
              <Play className="h-4 w-4 mr-1" /> Resume
            </Button>
          )}
          {(mission.status === 'unassigned' || mission.status === 'completed') && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setShowAssignForm(!showAssignForm)}
            >
              Assign
            </Button>
          )}
          <Button variant="outline" size="sm" onClick={() => navigate(`/missions/${name}/composer`)}>
            <Workflow className="h-4 w-4 mr-1" /> Visual Editor
          </Button>
          <Button variant="outline" size="sm" onClick={() => setWizardOpen(true)}>
            <Pencil className="h-4 w-4 mr-1" /> Open in Wizard
          </Button>
          <Button variant="outline" size="sm" onClick={load} aria-label="Refresh">
            <RefreshCw className="h-4 w-4" />
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="text-red-400 hover:text-red-300"
            onClick={() => setDeleteOpen(true)}
            aria-label="Delete mission"
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {/* Tab bar */}
      <div role="tablist" className="flex gap-4 border-b border-border px-4 md:px-8 pt-3 mb-4">
        {MISSION_TABS.map((t) => (
          <button
            key={t}
            role="tab"
            aria-selected={activeTab === t}
            aria-controls={`mission-panel-${t}`}
            onClick={() => setActiveTab(t)}
            className={`pb-2 text-sm capitalize transition-colors border-b-2 ${
              activeTab === t
                ? 'border-primary text-foreground'
                : 'border-transparent text-muted-foreground hover:text-foreground'
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      {/* Content */}
      <div role="tabpanel" id={`mission-panel-${activeTab}`} className="flex-1 overflow-auto p-4 md:p-8 space-y-6">
        {activeTab === 'quality' && <MissionQualityTab mission={mission as any} />}
        {activeTab === 'health' && <MissionHealthTab missionName={name!} />}
        {activeTab === 'overview' && <>
        {/* Inline-editable fields */}
        <InlineEditField
          label="Description"
          value={mission.description ?? ''}
          onSave={(v) => saveField('description', v)}
        />
        <InlineEditField
          label="Instructions"
          value={mission.instructions ?? ''}
          onSave={(v) => saveField('instructions', v)}
          multiline
          mono
        />

        {/* Assigned To */}
        <div className="space-y-1">
          <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
            Assigned To
          </span>
          {mission.assigned_to ? (
            <div className="flex items-center gap-2">
              <span className="inline-flex items-center justify-center h-6 w-6 rounded-full bg-primary/10 text-primary text-xs font-medium">
                {mission.assigned_to.charAt(0).toUpperCase()}
              </span>
              <span className="text-sm">{mission.assigned_to}</span>
              {mission.assigned_type && (
                <span className="text-xs text-muted-foreground">({mission.assigned_type})</span>
              )}
              <button
                className="text-[10px] text-primary hover:text-primary/80 ml-2 bg-transparent border-none p-0 cursor-pointer"
                onClick={() => setShowAssignForm(!showAssignForm)}
              >
                reassign
              </button>
            </div>
          ) : (
            <div className="text-sm text-muted-foreground italic">
              Unassigned
              <button
                className="text-[10px] text-primary hover:text-primary/80 ml-2 not-italic bg-transparent border-none p-0 cursor-pointer"
                onClick={() => setShowAssignForm(!showAssignForm)}
              >
                assign
              </button>
            </div>
          )}
          {showAssignForm && (
            <div className="flex items-center gap-2 mt-2">
              <input
                aria-label="Assign target"
                name="assign-target"
                autoComplete="off"
                className="bg-background border border-border rounded px-3 py-1.5 text-sm text-foreground focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/50"
                placeholder="Agent or team name"
                value={assignTarget}
                onChange={(e) => setAssignTarget(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && handleAssign()}
              />
              <label className="flex items-center gap-1 text-xs text-muted-foreground">
                <input
                  type="radio"
                  name="assignType"
                  checked={assignType === 'agent'}
                  onChange={() => setAssignType('agent')}
                />
                agent
              </label>
              <label className="flex items-center gap-1 text-xs text-muted-foreground">
                <input
                  type="radio"
                  name="assignType"
                  checked={assignType === 'team'}
                  onChange={() => setAssignType('team')}
                />
                team
              </label>
              <Button size="sm" onClick={handleAssign} disabled={!assignTarget.trim()}>
                Assign
              </Button>
            </div>
          )}
        </div>

        {/* Triggers */}
        {triggers && triggers.length > 0 && (
          <div className="space-y-1">
            <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
              Triggers
            </span>
            <div className="grid gap-2">
              {triggers.map((t, i) => (
                <div key={i} className="bg-background rounded p-2 flex items-center gap-2 text-sm">
                  <span>{triggerIcons[t.source] || '\u26A1'}</span>
                  <span className="font-medium">{t.source}</span>
                  {triggerSummary(t) && (
                    <span className="text-muted-foreground">{triggerSummary(t)}</span>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Requirements */}
        {(requires?.capabilities?.length || requires?.channels?.length) && (
          <div className="space-y-1">
            <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
              Requirements
            </span>
            <div className="flex flex-wrap gap-1.5">
              {requires?.capabilities?.map((c) => (
                <span
                  key={c}
                  className="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium bg-accent text-primary dark:text-primary"
                >
                  {c}
                </span>
              ))}
              {requires?.channels?.map((c) => (
                <span
                  key={c}
                  className="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium bg-purple-50 dark:bg-purple-950 text-purple-700 dark:text-purple-400"
                >
                  {c}
                </span>
              ))}
            </div>
          </div>
        )}

        {/* Budget */}
        <div className="space-y-1">
          <span className="text-[10px] uppercase tracking-wide text-muted-foreground">Budget</span>
          <div className="text-sm">
            {budgetParts.length > 0 ? budgetParts.join(' \u00B7 ') : (
              <span className="text-muted-foreground italic">No budget set</span>
            )}
          </div>
        </div>

        {/* Health */}
        {(health?.indicators?.length || health?.business_hours) && (
          <div className="space-y-1">
            <span className="text-[10px] uppercase tracking-wide text-muted-foreground">Health</span>
            <div className="flex flex-wrap gap-1.5">
              {health?.indicators?.map((ind) => (
                <span
                  key={ind}
                  className="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium bg-secondary text-muted-foreground"
                >
                  {ind}
                </span>
              ))}
            </div>
            {health?.business_hours && (
              <div className="text-sm text-muted-foreground">{health.business_hours}</div>
            )}
          </div>
        )}

        {/* Meeseeks */}
        <div className="space-y-1">
          <span className="text-[10px] uppercase tracking-wide text-muted-foreground">Meeseeks</span>
          <div className="text-sm">
            {mission.meeseeks ? (
              <span>
                Enabled
                {mission.meeseeks_limit != null && ` (limit: ${mission.meeseeks_limit}`}
                {mission.meeseeks_model && `${mission.meeseeks_limit != null ? ', ' : ' ('}model: ${mission.meeseeks_model}`}
                {(mission.meeseeks_limit != null || mission.meeseeks_model) && ')'}
              </span>
            ) : (
              <span className="text-muted-foreground italic">Disabled</span>
            )}
          </div>
        </div>

        {/* YAML section (collapsible) */}
        <div className="space-y-1">
          <button
            className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors"
            onClick={() => setYamlOpen(!yamlOpen)}
          >
            {yamlOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
            View as YAML
          </button>
          {yamlOpen && (
            <div className="space-y-2">
              {yamlEditing ? (
                <>
                  <textarea
                    aria-label="YAML editor"
                    name="yaml-editor"
                    className="w-full bg-background border border-border rounded px-3 py-2 font-mono text-xs text-foreground focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/50 resize-y"
                    style={{ minHeight: 200 }}
                    value={yamlDraft}
                    onChange={(e) => setYamlDraft(e.target.value)}
                  />
                  <div className="flex gap-2">
                    <Button size="sm" onClick={handleYamlSave}>Save</Button>
                    <Button variant="outline" size="sm" onClick={() => setYamlEditing(false)}>Cancel</Button>
                  </div>
                </>
              ) : (
                <>
                  <pre className="bg-background border border-border rounded p-3 font-mono text-xs text-foreground overflow-x-auto whitespace-pre-wrap">
                    {yamlPreview}
                  </pre>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      setYamlDraft(yamlPreview);
                      setYamlEditing(true);
                    }}
                  >
                    Edit YAML
                  </Button>
                </>
              )}
            </div>
          )}
        </div>

        {/* History section (collapsible, lazy-loaded) */}
        <div className="space-y-1">
          <button
            className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors"
            onClick={() => setHistoryOpen(!historyOpen)}
          >
            {historyOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
            History
          </button>
          {historyOpen && (
            <div>
              {historyLoading ? (
                <div className="text-sm text-muted-foreground py-2">Loading history…</div>
              ) : history && history.length > 0 ? (
                <JsonView data={history} />
              ) : (
                <div className="text-sm text-muted-foreground italic py-2">No history</div>
              )}
            </div>
          )}
        </div>
        </>}
      </div>

      {/* Wizard */}
      <MissionWizard
        open={wizardOpen}
        onOpenChange={setWizardOpen}
        editMission={mission}
        onComplete={load}
      />

      {/* Delete confirm */}
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title="Delete Mission"
        description={`Are you sure you want to delete "${mission.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={handleDelete}
      />
    </div>
  );
}
