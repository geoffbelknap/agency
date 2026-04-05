import React from 'react';
import { Badge } from '@/app/components/ui/badge';

interface SignalData {
  type: string;
  data: Record<string, any>;
}

export function SignalRenderer({ signal }: { signal: SignalData }) {
  const { type, data } = signal;

  if (type === 'reflection_cycle') {
    const isRevision = data.verdict === 'revision-needed';
    return (
      <div data-signal className={isRevision ? 'text-amber-500' : 'text-muted-foreground'}>
        <span>Reflection round {data.round}: {data.verdict}</span>
        {data.issues?.length > 0 && (
          <ul className="ml-4 mt-1 text-xs space-y-0.5">
            {data.issues.map((issue: string, i: number) => (
              <li key={i} className="text-muted-foreground"><span aria-hidden>•</span> {issue}</li>
            ))}
          </ul>
        )}
      </div>
    );
  }

  if (type === 'fallback_activated') {
    return (
      <div data-signal className="text-amber-500">
        <span>Fallback: {data.trigger} on {data.tool}</span>
        {data.policy_steps?.length > 0 && (
          <div className="flex gap-1 mt-1 flex-wrap">
            {data.policy_steps.map((step: string, i: number) => (
              <Badge key={i} variant="secondary" className="text-[10px]">{step}</Badge>
            ))}
          </div>
        )}
      </div>
    );
  }

  if (type === 'trajectory_anomaly') {
    const isCritical = data.severity === 'critical';
    return (
      <div data-signal className={isCritical ? 'text-red-500 shadow-[0_0_6px_hsl(0,72%,55%,0.4)] rounded px-2 py-1 animate-pulse-warning' : 'text-amber-500'}>
        <span>Trajectory: {data.detail}</span>
        <span className="text-[10px] text-muted-foreground ml-2">{data.detector}</span>
      </div>
    );
  }

  if (type === 'task_complete') {
    const parts: React.ReactNode[] = [];
    if (data.reflection_rounds > 0) {
      parts.push(<span key="refl" className="text-muted-foreground">(reflected {data.reflection_rounds}x)</span>);
      if (data.reflection_forced) parts.push(<Badge key="forced" variant="secondary" className="text-[10px] ml-1">forced</Badge>);
    }
    if (data.evaluation) {
      if (data.evaluation.passed === false) {
        const failed = data.evaluation.criteria_results?.filter((c: any) => !c.passed).length ?? 0;
        parts.push(<span key="eval" className="text-amber-500 ml-1">(evaluation: {failed} criteria failed)</span>);
      } else if (data.evaluation.passed === true) {
        parts.push(<span key="eval" className="text-green-500 ml-1">(evaluation: passed)</span>);
      }
    }
    if (data.tier) parts.push(<Badge key="tier" variant="secondary" className="text-[10px] ml-1">{data.tier}</Badge>);
    return (
      <div data-signal className="flex items-center flex-wrap gap-1">
        <span>Task complete</span>
        {parts}
      </div>
    );
  }

  // Default: render type and summary
  return (
    <div data-signal className="text-muted-foreground">
      <span className="text-[10px] uppercase tracking-wide">{type}</span>
      {data.message && <span className="ml-2">{data.message}</span>}
      {data.status && <span className="ml-2">{data.status}</span>}
    </div>
  );
}
