import { Circle } from 'lucide-react';
import { Agent } from '../../types';
import { Button } from '../../components/ui/button';

const TRUST_DESCRIPTIONS: Record<number, { label: string; description: string }> = {
  1: { label: 'Minimal', description: 'Read-only access, no external actions' },
  2: { label: 'Restricted', description: 'Limited tool use, supervised execution' },
  3: { label: 'Standard', description: 'Normal agent operations within policy' },
  4: { label: 'Elevated', description: 'Extended capabilities, reduced restrictions' },
  5: { label: 'Autonomous', description: 'Full autonomous operation including destructive actions' },
};

const TrustMeter = ({ level }: { level: number }) => (
  <div className="flex items-center gap-1" aria-hidden="true">
    {[1, 2, 3, 4, 5].map((i) => (
      <Circle
        key={i}
        className={`w-2 h-2 ${
          i <= level ? 'fill-slate-400 text-slate-400' : 'text-muted-foreground/70'
        }`}
      />
    ))}
  </div>
);

interface TrustTabProps {
  agents: Agent[];
  agentsLoading: boolean;
  trustError: string | null;
  onTrust: (agentName: string, action: 'elevate' | 'demote') => void;
}

export function TrustTab({ agents, agentsLoading, trustError, onTrust }: TrustTabProps) {
  return (
    <>
      {trustError && (
        <div className="mb-4 text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
          {trustError}
        </div>
      )}
      <div className="bg-card border border-border rounded overflow-x-auto">
        <table className="w-full text-sm min-w-[600px]">
          <thead>
            <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
              <th className="text-left p-3 md:p-4 font-medium">Agent</th>
              <th className="text-left p-3 md:p-4 font-medium">Trust Level</th>
              <th className="text-left p-3 md:p-4 font-medium">Restrictions</th>
              <th className="text-left p-3 md:p-4 font-medium">Actions</th>
            </tr>
          </thead>
          <tbody>
            {agentsLoading ? (
              <tr>
                <td colSpan={4} className="p-8 text-center text-muted-foreground text-sm">
                  Loading agents...
                </td>
              </tr>
            ) : agents.length === 0 ? (
              <tr>
                <td colSpan={4} className="p-8 text-center text-muted-foreground text-sm">
                  No agents found
                </td>
              </tr>
            ) : (
              agents.map((agent) => (
                <tr
                  key={agent.id}
                  className="border-b border-border hover:bg-secondary/50 transition-colors"
                >
                  <td className="p-4">
                    <code className="text-foreground">{agent.name}</code>
                  </td>
                  <td className="p-4">
                    <div className="flex items-center gap-3">
                      <TrustMeter level={agent.trustLevel || 3} />
                      <div>
                        <span className="text-xs text-muted-foreground">{agent.trustLevel}/5</span>
                        <span className="text-[10px] text-muted-foreground/70 ml-2">
                          {TRUST_DESCRIPTIONS[agent.trustLevel || 3]?.label}
                        </span>
                      </div>
                    </div>
                  </td>
                  <td className="p-4">
                    {agent.restrictions && agent.restrictions.length > 0 ? (
                      <div className="flex flex-wrap gap-1">
                        {agent.restrictions.map((r, i) => (
                          <span
                            key={i}
                            className="text-xs bg-amber-50 dark:bg-amber-950 text-amber-700 dark:text-amber-400 px-2 py-0.5 rounded"
                          >
                            {r}
                          </span>
                        ))}
                      </div>
                    ) : (
                      <span className="text-xs text-muted-foreground/70">None</span>
                    )}
                  </td>
                  <td className="p-4">
                    <div className="flex gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-7 text-xs"
                        onClick={() => onTrust(agent.name, 'elevate')}
                      >
                        Elevate
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-7 text-xs"
                        onClick={() => onTrust(agent.name, 'demote')}
                      >
                        Demote
                      </Button>
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      <div className="mt-4 grid grid-cols-2 sm:grid-cols-3 md:grid-cols-5 gap-2">
        {Object.entries(TRUST_DESCRIPTIONS).map(([level, info]) => (
          <div key={level} className="text-center">
            <div className="text-xs font-medium text-muted-foreground">{level} — {info.label}</div>
            <div className="text-[10px] text-muted-foreground/70 mt-0.5">{info.description}</div>
          </div>
        ))}
      </div>
    </>
  );
}
