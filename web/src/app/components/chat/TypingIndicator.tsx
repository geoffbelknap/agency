import { Bot } from 'lucide-react';

interface TypingIndicatorProps {
  agents: string[];
  activity?: Record<string, string>;
}

function formatAgents(agents: string[], activity?: Record<string, string>): string {
  if (agents.length === 1) {
    const act = activity?.[agents[0]];
    return act ? `${agents[0]} is ${act}` : `${agents[0]} is thinking`;
  }
  if (agents.length === 2) {
    return `${agents[0]} and ${agents[1]} are thinking`;
  }
  const others = agents.length - 2;
  return `${agents[0]}, ${agents[1]}, and ${others} others are thinking`;
}

export function TypingIndicator({ agents, activity }: TypingIndicatorProps) {
  if (agents.length === 0) return null;

  return (
    <div
      className="flex items-center gap-3 px-7 py-2"
      aria-live="polite"
      aria-atomic="true"
      aria-label={formatAgents(agents, activity)}
      style={{ background: 'var(--warm)' }}
    >
      <div
        className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg"
        style={{ background: 'var(--warm-3)', color: 'var(--ink-mid)' }}
      >
        <Bot size={14} strokeWidth={1.7} />
      </div>
      <div className="flex h-8 items-center gap-1 opacity-70">
        {[0, 1, 2].map((i) => (
          <span
            key={i}
            className="h-[5px] w-[5px] rounded-full"
            style={{
              background: 'var(--ink-mid)',
              animation: `agencyPulse 1.2s ease-out ${i * 0.2}s infinite`,
            }}
          />
        ))}
      </div>
      <span className="sr-only">{formatAgents(agents, activity)}</span>
    </div>
  );
}
