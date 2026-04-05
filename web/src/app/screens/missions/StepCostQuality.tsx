import { useState } from 'react';
import { type WizardState } from './serialize';

interface StepCostQualityProps {
  state: WizardState;
  onChange: (state: WizardState) => void;
}

type CostMode = 'frugal' | 'balanced' | 'thorough';

const PRESETS: Record<CostMode, Partial<WizardState>> = {
  frugal: {
    cost_mode: 'frugal',
    reflection: { enabled: false, max_rounds: 0, criteria: [] },
    success_criteria: { checklist: [], evaluation: { enabled: false, mode: 'checklist_only', on_failure: 'flag' } },
    procedural_memory: undefined,
    episodic_memory: { capture: true, retrieve: true, max_retrieved: 5, tool_enabled: false },
  },
  balanced: {
    cost_mode: 'balanced',
    reflection: { enabled: true, max_rounds: 2, criteria: [] },
    success_criteria: { checklist: [], evaluation: { enabled: true, mode: 'checklist_only', on_failure: 'flag' } },
    procedural_memory: { capture: true, retrieve: true, max_retrieved: 5 },
    episodic_memory: { capture: true, retrieve: true, max_retrieved: 10, tool_enabled: false },
  },
  thorough: {
    cost_mode: 'thorough',
    reflection: { enabled: true, max_rounds: 5, criteria: [] },
    success_criteria: { checklist: [], evaluation: { enabled: true, mode: 'llm', on_failure: 'retry' } },
    procedural_memory: { capture: true, retrieve: true, max_retrieved: 10 },
    episodic_memory: { capture: true, retrieve: true, max_retrieved: 20, tool_enabled: true },
  },
};

const CARD_DESCRIPTIONS: Record<CostMode, string> = {
  frugal: 'Minimize token usage. No reflection, minimal memory.',
  balanced: 'Moderate reflection and evaluation. Good for most tasks.',
  thorough: 'Full reflection, LLM evaluation, and rich memory. Best for complex missions.',
};

export function StepCostQuality({ state, onChange }: StepCostQualityProps) {
  const [advancedOpen, setAdvancedOpen] = useState(false);

  function selectMode(mode: CostMode) {
    onChange({ ...state, ...PRESETS[mode] });
  }

  function updateReflection(updates: Partial<NonNullable<WizardState['reflection']>>) {
    onChange({
      ...state,
      reflection: { ...(state.reflection ?? { enabled: false, max_rounds: 0, criteria: [] }), ...updates },
    });
  }

  function updateEvaluation(updates: Partial<NonNullable<WizardState['success_criteria']>['evaluation']>) {
    const existing = state.success_criteria ?? { checklist: [], evaluation: { enabled: false, mode: 'checklist_only', on_failure: 'flag' } };
    onChange({
      ...state,
      success_criteria: {
        ...existing,
        evaluation: { ...existing.evaluation, ...updates },
      },
    });
  }

  function updateProceduralMemory(updates: Partial<NonNullable<WizardState['procedural_memory']>>) {
    onChange({
      ...state,
      procedural_memory: { ...(state.procedural_memory ?? { capture: false, retrieve: false, max_retrieved: 5 }), ...updates },
    });
  }

  function updateEpisodicMemory(updates: Partial<NonNullable<WizardState['episodic_memory']>>) {
    onChange({
      ...state,
      episodic_memory: { ...(state.episodic_memory ?? { capture: false, retrieve: false, max_retrieved: 5, tool_enabled: false }), ...updates },
    });
  }

  const modes: CostMode[] = ['frugal', 'balanced', 'thorough'];

  return (
    <div className="space-y-4">
      <div>
        <h3 className="text-sm font-medium mb-1">Cost &amp; Quality Mode</h3>
        <p className="text-xs text-muted-foreground mb-3">
          Choose how much compute and token budget this mission should use.
        </p>
      </div>

      <div className="grid grid-cols-3 gap-3">
        {modes.map((mode) => {
          const isSelected = state.cost_mode === mode;
          const baseClass = mode === 'thorough' ? 'border-border bg-primary/5' : 'border-border bg-card';
          const selectedClass = isSelected ? 'border-primary ring-1 ring-primary/30' : '';
          return (
            <button
              key={mode}
              type="button"
              data-cost-card
              onClick={() => selectMode(mode)}
              className={`rounded-lg border p-4 text-left cursor-pointer transition-all ${baseClass} ${selectedClass}`}
            >
              <div className="font-medium text-sm capitalize mb-1">
                {mode.charAt(0).toUpperCase() + mode.slice(1)}
              </div>
              <div className="text-xs text-muted-foreground">{CARD_DESCRIPTIONS[mode]}</div>
            </button>
          );
        })}
      </div>

      <div className="mt-4">
        <button
          type="button"
          onClick={() => setAdvancedOpen((v) => !v)}
          className="text-xs text-muted-foreground hover:text-foreground flex items-center gap-1"
        >
          {advancedOpen ? '▾' : '▸'} Advanced
        </button>

        {advancedOpen && (
          <div className="mt-3 space-y-4 border border-border rounded-lg p-4">
            {/* Reflection */}
            <div>
              <div className="text-sm font-medium mb-2">Reflection</div>
              <div className="flex items-center gap-2 mb-2">
                <input
                  type="checkbox"
                  id="reflection-enabled"
                  checked={state.reflection?.enabled ?? false}
                  onChange={(e) => updateReflection({ enabled: e.target.checked })}
                  className="rounded focus-visible:ring-2 focus-visible:ring-primary/50"
                />
                <label htmlFor="reflection-enabled" className="text-xs">Enable reflection</label>
              </div>
              {state.reflection?.enabled && (
                <div className="flex items-center gap-2">
                  <label htmlFor="reflection-rounds" className="text-xs text-muted-foreground">Max rounds:</label>
                  <input
                    id="reflection-rounds"
                    type="number"
                    min={0}
                    max={10}
                    value={state.reflection?.max_rounds ?? 0}
                    onChange={(e) => updateReflection({ max_rounds: parseInt(e.target.value, 10) || 0 })}
                    className="w-16 text-xs border border-border rounded px-1 py-0.5 bg-background focus-visible:ring-2 focus-visible:ring-primary/50"
                  />
                </div>
              )}
            </div>

            {/* Success Criteria */}
            <div>
              <div className="text-sm font-medium mb-2">Success Criteria</div>
              <div className="flex items-center gap-2">
                <input
                  type="checkbox"
                  id="evaluation-enabled"
                  checked={state.success_criteria?.evaluation?.enabled ?? false}
                  onChange={(e) => updateEvaluation({ enabled: e.target.checked })}
                  className="rounded focus-visible:ring-2 focus-visible:ring-primary/50"
                />
                <label htmlFor="evaluation-enabled" className="text-xs">Enable evaluation</label>
              </div>
            </div>

            {/* Memory */}
            <div>
              <div className="text-sm font-medium mb-2">Memory</div>
              <div className="space-y-1">
                <div className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    id="procedural-capture"
                    checked={state.procedural_memory?.capture ?? false}
                    onChange={(e) => updateProceduralMemory({ capture: e.target.checked })}
                    className="rounded focus-visible:ring-2 focus-visible:ring-primary/50"
                  />
                  <label htmlFor="procedural-capture" className="text-xs">Procedural memory capture</label>
                </div>
                <div className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    id="episodic-capture"
                    checked={state.episodic_memory?.capture ?? false}
                    onChange={(e) => updateEpisodicMemory({ capture: e.target.checked })}
                    className="rounded focus-visible:ring-2 focus-visible:ring-primary/50"
                  />
                  <label htmlFor="episodic-capture" className="text-xs">Episodic memory capture</label>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
