import { useState } from 'react';
import { Button } from '../../components/ui/button';
import TriggerCards from './TriggerCards';
import { BUILT_IN_TEMPLATES } from './templates';
import type { WizardState } from './serialize';

export interface StepProps {
  state: WizardState;
  onChange: (updates: Partial<WizardState>) => void;
}

const inputClass =
  'bg-background border border-border text-foreground rounded px-3 py-1.5 text-sm placeholder:text-muted-foreground/70 w-full';
const labelClass = 'text-[10px] text-muted-foreground uppercase tracking-wide';

const NAME_RE = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;

// ---------------------------------------------------------------------------
// Step 1: Basics
// ---------------------------------------------------------------------------

export function StepBasics({ state, onChange }: StepProps) {
  const nameValid =
    state.name === '' || (state.name.length >= 2 && state.name.length <= 63 && NAME_RE.test(state.name));

  return (
    <div className="space-y-6">
      {/* Template picker */}
      <div className="space-y-2">
        <span className={labelClass}>Start from a template</span>
        <div className="flex gap-3 overflow-x-auto pb-2">
          {BUILT_IN_TEMPLATES.map((t) => (
            <div
              key={t.id}
              onClick={() => onChange({ ...t.defaults, templateId: t.id })}
              className={`border rounded-lg p-4 cursor-pointer hover:border-primary/50 transition-colors shrink-0 w-44 flex flex-col items-center text-center gap-2 ${
                state.templateId === t.id
                  ? 'border-primary bg-primary/10'
                  : 'border-border'
              }`}
            >
              <span className="text-3xl">{t.icon}</span>
              <span className="text-sm font-medium">{t.label}</span>
              <span className="text-xs text-muted-foreground">{t.description}</span>
            </div>
          ))}
          {/* Browse Hub placeholder */}
          <div className="border border-dashed border-border/60 rounded-lg p-4 cursor-default shrink-0 w-44 flex flex-col items-center text-center gap-2 opacity-60">
            <span className="text-3xl">🔍</span>
            <span className="text-sm font-medium">More templates</span>
            <span className="text-xs text-muted-foreground">Browse Hub...</span>
          </div>
        </div>
      </div>

      {/* Divider */}
      <div className="flex items-center gap-4">
        <div className="flex-1 border-t border-border" />
        <span className="text-xs text-muted-foreground">or start from scratch</span>
        <div className="flex-1 border-t border-border" />
      </div>

      {/* Name */}
      <div className="space-y-1.5">
        <label className={labelClass}>Name *</label>
        <input
          className={inputClass}
          value={state.name}
          onChange={(e) => onChange({ name: e.target.value })}
          placeholder="my-mission"
        />
        {!nameValid && (
          <p className="text-xs text-red-400">
            Name must be 2-63 chars, lowercase alphanumeric and hyphens, must start and end with alphanumeric.
          </p>
        )}
      </div>

      {/* Description */}
      <div className="space-y-1.5">
        <label className={labelClass}>Description *</label>
        <input
          className={inputClass}
          value={state.description}
          onChange={(e) => onChange({ description: e.target.value })}
          placeholder="What does this mission do?"
        />
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Step 2: Instructions
// ---------------------------------------------------------------------------

export function StepInstructions({ state, onChange }: StepProps) {
  return (
    <div className="space-y-2">
      <label className={labelClass}>Instructions</label>
      <textarea
        className={`${inputClass} min-h-[200px] font-mono resize-y`}
        value={state.instructions}
        onChange={(e) => onChange({ instructions: e.target.value })}
        placeholder={
          'Describe what the agent should do when this mission is active...\n\nExample:\nMonitor the #ops channel for deploy notifications.\nWhen a deploy fails:\n1. Check the logs\n2. Post a summary'
        }
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Step 3: Triggers
// ---------------------------------------------------------------------------

export function StepTriggers({ state, onChange }: StepProps) {
  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">
        Configure when this mission activates. This step is optional — skip if the mission is manually triggered.
      </p>
      <TriggerCards triggers={state.triggers} onChange={(triggers) => onChange({ triggers })} />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Step 4: Requirements
// ---------------------------------------------------------------------------

function TagInput({
  label,
  items,
  onChange,
  placeholder,
  color,
}: {
  label: string;
  items: string[];
  onChange: (items: string[]) => void;
  placeholder: string;
  color: string;
}) {
  const [draft, setDraft] = useState('');

  function add() {
    const val = draft.trim();
    if (val && !items.includes(val)) {
      onChange([...items, val]);
    }
    setDraft('');
  }

  return (
    <div className="space-y-1.5">
      <label className={labelClass}>{label}</label>
      <div className="flex gap-2">
        <input
          className={inputClass}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); add(); } }}
          placeholder={placeholder}
        />
        <Button variant="outline" size="sm" onClick={add} type="button">
          Add
        </Button>
      </div>
      {items.length > 0 && (
        <div className="flex flex-wrap gap-1.5 pt-1">
          {items.map((item) => (
            <span
              key={item}
              className={`text-xs px-2 py-0.5 rounded ${color} flex items-center gap-1`}
            >
              {item}
              <button
                type="button"
                className="hover:text-foreground"
                onClick={() => onChange(items.filter((i) => i !== item))}
              >
                &times;
              </button>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

export function StepRequirements({ state, onChange }: StepProps) {
  function setBudget(field: 'daily' | 'monthly' | 'per_task', raw: string) {
    const val = raw === '' ? null : Number(raw);
    onChange({ budget: { ...state.budget, [field]: val } });
  }

  function setHealth(updates: Partial<WizardState['health']>) {
    onChange({ health: { ...state.health, ...updates } });
  }

  return (
    <div className="space-y-6">
      {/* Requirements */}
      <div className="space-y-4">
        <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
          Requirements
        </h4>
        <TagInput
          label="Capabilities"
          items={state.requires.capabilities}
          onChange={(capabilities) =>
            onChange({ requires: { ...state.requires, capabilities } })
          }
          placeholder="e.g. web-search"
          color="bg-primary/15 text-primary/80"
        />
        <TagInput
          label="Channels"
          items={state.requires.channels}
          onChange={(channels) =>
            onChange({ requires: { ...state.requires, channels } })
          }
          placeholder="e.g. #ops"
          color="bg-purple-500/15 text-purple-300"
        />
      </div>

      {/* Budget */}
      <div className="space-y-3">
        <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
          Budget
        </h4>
        <div className="grid grid-cols-3 gap-3">
          {(['daily', 'monthly', 'per_task'] as const).map((field) => (
            <div key={field} className="space-y-1">
              <label className={labelClass}>
                {field === 'per_task' ? 'Per-task' : field.charAt(0).toUpperCase() + field.slice(1)} ($)
              </label>
              <div className="relative">
                <span className="absolute left-2.5 top-1/2 -translate-y-1/2 text-xs text-muted-foreground">
                  $
                </span>
                <input
                  type="number"
                  className={`${inputClass} pl-6`}
                  value={state.budget[field] ?? ''}
                  onChange={(e) => setBudget(field, e.target.value)}
                  placeholder="0"
                  min={0}
                />
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Health */}
      <div className="space-y-4">
        <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
          Health (optional)
        </h4>
        <TagInput
          label="Indicators"
          items={state.health.indicators}
          onChange={(indicators) => setHealth({ indicators })}
          placeholder="e.g. response-time"
          color="bg-emerald-500/15 text-emerald-300"
        />
        <div className="space-y-1.5">
          <label className={labelClass}>Business hours</label>
          <input
            className={inputClass}
            value={state.health.business_hours}
            onChange={(e) => setHealth({ business_hours: e.target.value })}
            placeholder="e.g. 9am-5pm PT"
          />
        </div>
      </div>

      {/* Meeseeks */}
      <div className="space-y-3">
        <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
          Meeseeks
        </h4>
        <label className="flex items-center gap-2 cursor-pointer">
          <button
            type="button"
            role="switch"
            aria-checked={state.meeseeks}
            onClick={() => onChange({ meeseeks: !state.meeseeks })}
            className={`relative w-9 h-5 rounded-full transition-colors ${
              state.meeseeks ? 'bg-primary' : 'bg-border'
            }`}
          >
            <span
              className={`absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full transition-transform ${
                state.meeseeks ? 'translate-x-4' : ''
              }`}
            />
          </button>
          <span className="text-sm">Enable Meeseeks</span>
        </label>

        {state.meeseeks && (
          <div className="space-y-3 pl-1">
            <div className="space-y-1.5">
              <label className={labelClass}>Limit</label>
              <input
                type="number"
                className={inputClass}
                value={state.meeseeksLimit ?? ''}
                onChange={(e) =>
                  onChange({
                    meeseeksLimit: e.target.value === '' ? null : Number(e.target.value),
                  })
                }
                placeholder="Max concurrent"
                min={1}
              />
            </div>
            <div className="space-y-1.5">
              <label className={labelClass}>Model</label>
              <div className="flex gap-1">
                {(['haiku', 'sonnet'] as const).map((m) => (
                  <Button
                    key={m}
                    variant={state.meeseeksModel === m ? 'default' : 'outline'}
                    size="sm"
                    type="button"
                    onClick={() => onChange({ meeseeksModel: m })}
                  >
                    {m.charAt(0).toUpperCase() + m.slice(1)}
                  </Button>
                ))}
              </div>
            </div>
            <div className="space-y-1.5">
              <label className={labelClass}>Budget per meeseeks ($)</label>
              <div className="relative">
                <span className="absolute left-2.5 top-1/2 -translate-y-1/2 text-xs text-muted-foreground">
                  $
                </span>
                <input
                  type="number"
                  className={`${inputClass} pl-6`}
                  value={state.meeseeksBudget ?? ''}
                  onChange={(e) =>
                    onChange({
                      meeseeksBudget: e.target.value === '' ? null : Number(e.target.value),
                    })
                  }
                  placeholder="0"
                  min={0}
                />
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Step 5: Review
// ---------------------------------------------------------------------------

interface StepReviewProps extends StepProps {
  onSubmit: () => void;
  onGoToStep: (step: number) => void;
  isEdit: boolean;
  yamlPreview: string;
}

function SummaryCard({
  title,
  step,
  onGoToStep,
  children,
}: {
  title: string;
  step: number;
  onGoToStep: (step: number) => void;
  children: React.ReactNode;
}) {
  return (
    <div
      onClick={() => onGoToStep(step)}
      className="border border-border rounded-lg p-4 cursor-pointer hover:border-primary/50 transition-colors flex items-start justify-between gap-4"
    >
      <div className="min-w-0 space-y-1">
        <h4 className="text-sm font-medium">{title}</h4>
        <div className="text-xs text-muted-foreground">{children}</div>
      </div>
      <span className="text-xs text-primary shrink-0">edit</span>
    </div>
  );
}

export function StepReview({
  state,
  onChange,
  onSubmit,
  onGoToStep,
  isEdit,
  yamlPreview,
}: StepReviewProps) {
  const [yamlOpen, setYamlOpen] = useState(false);

  const instrPreview = state.instructions
    ? state.instructions.split('\n').slice(0, 2).join(' ')
    : '(none)';
  const triggerText =
    state.triggers.length === 0
      ? 'No triggers (manual only)'
      : `${state.triggers.length} trigger${state.triggers.length > 1 ? 's' : ''}`;

  const reqParts: string[] = [];
  if (state.requires.capabilities.length > 0)
    reqParts.push(`${state.requires.capabilities.length} cap${state.requires.capabilities.length > 1 ? 's' : ''}`);
  if (state.requires.channels.length > 0)
    reqParts.push(`${state.requires.channels.length} channel${state.requires.channels.length > 1 ? 's' : ''}`);
  const budgetParts: string[] = [];
  if (state.budget.daily !== null) budgetParts.push(`$${state.budget.daily}/day`);
  if (state.budget.monthly !== null) budgetParts.push(`$${state.budget.monthly}/mo`);
  if (state.budget.per_task !== null) budgetParts.push(`$${state.budget.per_task}/task`);
  if (budgetParts.length > 0) reqParts.push(budgetParts.join(', '));
  const reqSummary = reqParts.length > 0 ? reqParts.join(' · ') : 'No requirements';

  return (
    <div className="space-y-6">
      {/* Summary cards */}
      <div className="space-y-3">
        <SummaryCard title="Basics" step={0} onGoToStep={onGoToStep}>
          <p>
            <span className="font-medium text-foreground">{state.name || '(unnamed)'}</span>
            {state.description && <> — {state.description}</>}
          </p>
        </SummaryCard>
        <SummaryCard title="Instructions" step={1} onGoToStep={onGoToStep}>
          <p className="truncate">{instrPreview}</p>
        </SummaryCard>
        <SummaryCard title="Triggers" step={2} onGoToStep={onGoToStep}>
          <p>{triggerText}</p>
        </SummaryCard>
        <SummaryCard title="Requirements" step={3} onGoToStep={onGoToStep}>
          <p>{reqSummary}</p>
        </SummaryCard>
      </div>

      {/* Assignment */}
      <div className="space-y-3">
        <label className={labelClass}>Assign to (optional)</label>
        <input
          className={inputClass}
          value={state.assignTarget}
          onChange={(e) => onChange({ assignTarget: e.target.value })}
          placeholder="Agent or team name"
        />
        <div className="flex gap-3">
          {(['agent', 'team'] as const).map((t) => (
            <label key={t} className="flex items-center gap-1.5 cursor-pointer text-sm">
              <input
                type="radio"
                name="assignType"
                checked={state.assignType === t}
                onChange={() => onChange({ assignType: t })}
                className="accent-primary"
              />
              {t.charAt(0).toUpperCase() + t.slice(1)}
            </label>
          ))}
        </div>
      </div>

      {/* YAML preview */}
      <div className="space-y-2">
        <button
          type="button"
          className="text-sm text-muted-foreground hover:text-foreground transition-colors"
          onClick={() => setYamlOpen(!yamlOpen)}
        >
          {yamlOpen ? '▼' : '▶'} View as YAML
        </button>
        {yamlOpen && (
          <div className="space-y-2">
            <pre className="bg-background border border-border rounded p-4 text-xs font-mono overflow-x-auto whitespace-pre">
              {yamlPreview}
            </pre>
            <span className="text-xs text-primary cursor-pointer hover:underline">
              Switch to YAML editor
            </span>
          </div>
        )}
      </div>

      {/* Submit */}
      <Button onClick={onSubmit}>{isEdit ? 'Save Changes' : 'Create Mission'}</Button>
    </div>
  );
}
