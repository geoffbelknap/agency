import { useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { Badge } from '@/app/components/ui/badge';
import type { Mission, CostMode } from '@/app/types';

interface Props {
  mission: Mission;
}

const COST_MODE_BADGE: Record<CostMode, string> = {
  frugal: 'bg-secondary text-muted-foreground',
  balanced: 'bg-blue-100 text-blue-700 dark:bg-primary/20 dark:text-primary',
  thorough: 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400',
};

const COST_MODE_DESC: Record<CostMode, string> = {
  frugal: 'Minimize cost — reflection off, no evaluation, episodic memory only, minimal task tier',
  balanced: 'Default tradeoffs — reflection (2 rounds), checklist evaluation, both memories, standard tier',
  thorough: 'Full quality — reflection (5 rounds), LLM evaluation, both memories + tool, full tier',
};

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
      {children}
    </span>
  );
}

function Card({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={`bg-muted/30 border border-border rounded-lg p-4 space-y-3 ${className ?? ''}`}>
      {children}
    </div>
  );
}

export function MissionQualityTab({ mission }: Props) {
  const [expandedPolicy, setExpandedPolicy] = useState<number | null>(null);

  const { cost_mode, reflection, success_criteria, fallback, procedural_memory, episodic_memory } = mission;

  return (
    <div className="space-y-6 p-4 md:p-6">
      {/* Cost Mode — full-width header card */}
      <Card>
        <SectionLabel>Cost Mode</SectionLabel>
        {cost_mode ? (
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <span
                className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium capitalize ${COST_MODE_BADGE[cost_mode]}`}
              >
                {cost_mode}
              </span>
            </div>
            <p className="text-sm text-muted-foreground">{COST_MODE_DESC[cost_mode]}</p>
          </div>
        ) : (
          <p className="text-sm text-muted-foreground italic">No cost mode configured.</p>
        )}
      </Card>

      {/* Reflection + Memory — two-column grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* Reflection */}
        <Card>
          <SectionLabel>Reflection</SectionLabel>
          {reflection ? (
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <Badge
                  variant="outline"
                  className={reflection.enabled ? 'text-green-400 border-green-800' : 'text-muted-foreground'}
                >
                  {reflection.enabled ? 'Enabled' : 'Disabled'}
                </Badge>
                {reflection.enabled && (
                  <span className="text-xs text-muted-foreground">
                    max {reflection.max_rounds} round{reflection.max_rounds !== 1 ? 's' : ''}
                  </span>
                )}
              </div>
              {reflection.criteria && reflection.criteria.length > 0 && (
                <div className="space-y-1">
                  <span className="text-xs text-muted-foreground">Criteria</span>
                  <ul className="space-y-1">
                    {reflection.criteria.map((c, i) => (
                      <li key={i} className="flex items-start gap-1.5 text-sm">
                        <span className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-muted-foreground" />
                        {c}
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground italic">No reflection configuration.</p>
          )}
        </Card>

        {/* Memory */}
        <Card>
          <SectionLabel>Memory</SectionLabel>
          {procedural_memory || episodic_memory ? (
            <div className="space-y-3">
              {procedural_memory && (
                <div className="space-y-1">
                  <span className="text-xs font-medium text-foreground">Procedural</span>
                  <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
                    <span className="text-muted-foreground">Capture</span>
                    <span>{procedural_memory.capture ? 'Yes' : 'No'}</span>
                    <span className="text-muted-foreground">Retrieve</span>
                    <span>{procedural_memory.retrieve ? 'Yes' : 'No'}</span>
                    <span className="text-muted-foreground">Max retrieved</span>
                    <span>{procedural_memory.max_retrieved}</span>
                  </div>
                </div>
              )}
              {episodic_memory && (
                <div className="space-y-1">
                  <span className="text-xs font-medium text-foreground">Episodic</span>
                  <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
                    <span className="text-muted-foreground">Capture</span>
                    <span>{episodic_memory.capture ? 'Yes' : 'No'}</span>
                    <span className="text-muted-foreground">Retrieve</span>
                    <span>{episodic_memory.retrieve ? 'Yes' : 'No'}</span>
                    <span className="text-muted-foreground">Max retrieved</span>
                    <span>{episodic_memory.max_retrieved}</span>
                    <span className="text-muted-foreground">Tool enabled</span>
                    <span>{episodic_memory.tool_enabled ? 'Yes' : 'No'}</span>
                  </div>
                </div>
              )}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground italic">No memory configuration.</p>
          )}
        </Card>
      </div>

      {/* Success Criteria — full-width */}
      <Card>
        <SectionLabel>Success Criteria</SectionLabel>
        {success_criteria ? (
          <div className="space-y-4">
            {/* Checklist */}
            {success_criteria.checklist && success_criteria.checklist.length > 0 ? (
              <ul className="space-y-2">
                {success_criteria.checklist.map((item) => (
                  <li key={item.id} className="flex items-start gap-2 text-sm">
                    <span className="mt-0.5 h-4 w-4 shrink-0 rounded border border-border" />
                    <span className="flex-1">{item.description}</span>
                    <Badge
                      variant="outline"
                      className={
                        item.required
                          ? 'text-orange-400 border-orange-800 shrink-0'
                          : 'text-muted-foreground shrink-0'
                      }
                    >
                      {item.required ? 'required' : 'optional'}
                    </Badge>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground italic">No checklist items.</p>
            )}

            {/* Evaluation config */}
            {success_criteria.evaluation && (
              <div className="space-y-1 pt-2 border-t border-border">
                <span className="text-xs font-medium text-foreground">Evaluation</span>
                <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
                  <span className="text-muted-foreground">Enabled</span>
                  <span>{success_criteria.evaluation.enabled ? 'Yes' : 'No'}</span>
                  <span className="text-muted-foreground">Mode</span>
                  <span>{success_criteria.evaluation.mode}</span>
                  {success_criteria.evaluation.model && (
                    <>
                      <span className="text-muted-foreground">Model</span>
                      <span className="font-mono">{success_criteria.evaluation.model}</span>
                    </>
                  )}
                  <span className="text-muted-foreground">On failure</span>
                  <span>{success_criteria.evaluation.on_failure}</span>
                  <span className="text-muted-foreground">Max retries</span>
                  <span>{success_criteria.evaluation.max_retries}</span>
                </div>
              </div>
            )}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground italic">No success criteria configured.</p>
        )}
      </Card>

      {/* Fallback Policies — full-width */}
      <Card>
        <SectionLabel>Fallback Policies</SectionLabel>
        {fallback ? (
          <div className="space-y-2">
            {fallback.policies && fallback.policies.length > 0 ? (
              fallback.policies.map((policy, i) => (
                <div key={i} className="border border-border rounded-md overflow-hidden">
                  <button
                    className="w-full flex items-center gap-2 px-3 py-2 text-sm hover:bg-muted/50 transition-colors text-left"
                    onClick={() => setExpandedPolicy(expandedPolicy === i ? null : i)}
                  >
                    {expandedPolicy === i
                      ? <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                      : <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                    }
                    <Badge variant="outline" className="text-xs">
                      {policy.trigger}
                    </Badge>
                    {policy.tool && (
                      <span className="text-muted-foreground text-xs">tool: {policy.tool}</span>
                    )}
                    {policy.capability && (
                      <span className="text-muted-foreground text-xs">cap: {policy.capability}</span>
                    )}
                    {policy.threshold != null && (
                      <span className="text-muted-foreground text-xs">threshold: {policy.threshold}</span>
                    )}
                    {policy.count != null && (
                      <span className="text-muted-foreground text-xs">count: {policy.count}</span>
                    )}
                    <span className="ml-auto text-muted-foreground text-xs">
                      {policy.strategy.length} step{policy.strategy.length !== 1 ? 's' : ''}
                    </span>
                  </button>
                  {expandedPolicy === i && (
                    <div className="px-3 pb-3 pt-1 space-y-1.5 border-t border-border">
                      {policy.strategy.map((step, j) => (
                        <div key={j} className="flex items-start gap-2 text-xs">
                          <span className="text-muted-foreground shrink-0">{j + 1}.</span>
                          <span className="font-medium text-foreground">{step.action}</span>
                          {step.max_attempts != null && (
                            <span className="text-muted-foreground">×{step.max_attempts}</span>
                          )}
                          {step.backoff && (
                            <span className="text-muted-foreground">backoff: {step.backoff}</span>
                          )}
                          {step.tool && (
                            <span className="text-muted-foreground">tool: {step.tool}</span>
                          )}
                          {step.hint && (
                            <span className="text-muted-foreground italic">{step.hint}</span>
                          )}
                          {step.message && (
                            <span className="text-muted-foreground italic">{step.message}</span>
                          )}
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              ))
            ) : (
              <p className="text-sm text-muted-foreground italic">No policies configured.</p>
            )}

            {/* Default policy */}
            {fallback.default_policy && fallback.default_policy.strategy.length > 0 && (
              <div className="pt-2 border-t border-border space-y-1">
                <span className="text-xs font-medium text-foreground">Default Policy</span>
                <div className="space-y-1">
                  {fallback.default_policy.strategy.map((step, j) => (
                    <div key={j} className="flex items-start gap-2 text-xs">
                      <span className="text-muted-foreground shrink-0">{j + 1}.</span>
                      <span className="font-medium text-foreground">{step.action}</span>
                      {step.max_attempts != null && (
                        <span className="text-muted-foreground">×{step.max_attempts}</span>
                      )}
                      {step.hint && (
                        <span className="text-muted-foreground italic">{step.hint}</span>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground italic">No fallback policies configured.</p>
        )}
      </Card>
    </div>
  );
}
