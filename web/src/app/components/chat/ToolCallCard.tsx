import { Terminal } from 'lucide-react';

interface ToolCall {
  tool: string;
  input: any;
  output?: string;
  duration_ms?: number;
}

interface ToolCallCardProps {
  call: ToolCall;
  agent: string;
}

function compactInput(input: any): string {
  if (input === undefined || input === null) return '';
  if (typeof input === 'string') return input;
  if (typeof input === 'object') {
    const entries = Object.entries(input)
      .slice(0, 3)
      .map(([key, value]) => `${key}=${JSON.stringify(value)}`);
    return entries.length > 0 ? `(${entries.join(', ')})` : '';
  }
  return String(input);
}

export function ToolCallCard({ call }: ToolCallCardProps) {
  const durationLabel = call.duration_ms !== undefined ? ` · ${(call.duration_ms / 1000).toFixed(1)}s` : '';
  const inputLabel = compactInput(call.input);
  const outputLabel = call.output ? ` → ${call.output}` : '';

  return (
    <div style={{ marginLeft: 44 }}>
      <div
        className="mono inline-flex max-w-full items-center gap-2"
        style={{
          padding: '5px 10px',
          background: 'var(--warm-2)',
          border: '0.5px solid var(--ink-hairline)',
          borderRadius: 6,
          fontSize: 11,
          color: 'var(--ink-mid)',
        }}
      >
        <Terminal size={11} className="shrink-0" />
        <span className="truncate">
          {call.tool}{inputLabel}{outputLabel}{durationLabel}
        </span>
      </div>
    </div>
  );
}
