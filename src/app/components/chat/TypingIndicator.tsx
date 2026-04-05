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
    <div className="flex items-center gap-2 px-4 py-1.5 text-xs text-muted-foreground" aria-live="polite" aria-atomic="true">
      <div className="flex items-center gap-0.5">
        <span
          className="w-1.5 h-1.5 rounded-full bg-muted-foreground animate-bounce"
          style={{ animationDelay: '0ms' }}
        />
        <span
          className="w-1.5 h-1.5 rounded-full bg-muted-foreground animate-bounce"
          style={{ animationDelay: '150ms' }}
        />
        <span
          className="w-1.5 h-1.5 rounded-full bg-muted-foreground animate-bounce"
          style={{ animationDelay: '300ms' }}
        />
      </div>
      <span>{formatAgents(agents, activity)}</span>
    </div>
  );
}
