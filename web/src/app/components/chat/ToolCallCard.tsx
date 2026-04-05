import { useState } from 'react';
import { ChevronRight } from 'lucide-react';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../ui/collapsible';
import { cn } from '../ui/utils';

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

export function ToolCallCard({ call, agent }: ToolCallCardProps) {
  const [open, setOpen] = useState(false);

  const durationLabel =
    call.duration_ms !== undefined
      ? `${(call.duration_ms / 1000).toFixed(1)}s`
      : null;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <div
        className={cn(
          'border-l-2 border-border bg-card/50 rounded-r px-2 py-1 text-xs',
        )}
      >
        <CollapsibleTrigger asChild>
          <button className="flex w-full items-center gap-1.5 text-left text-muted-foreground hover:text-foreground/80">
            <ChevronRight
              className={cn(
                'w-3 h-3 flex-shrink-0 transition-transform duration-150',
                open && 'rotate-90',
              )}
            />
            <span>
              {agent} ran <span className="font-mono text-foreground/80">{call.tool}</span>
            </span>
            {durationLabel && (
              <span className="ml-auto text-muted-foreground">{durationLabel}</span>
            )}
          </button>
        </CollapsibleTrigger>

        <CollapsibleContent>
          <div className="mt-1.5 space-y-1.5 pl-4">
            <div>
              <div className="text-muted-foreground mb-0.5">Input</div>
              <pre className="text-foreground/80 whitespace-pre-wrap break-all bg-card rounded px-2 py-1 text-xs overflow-x-auto">
                {JSON.stringify(call.input, null, 2)}
              </pre>
            </div>
            {call.output !== undefined && (
              <div>
                <div className="text-muted-foreground mb-0.5">Output</div>
                <pre className="text-foreground/80 whitespace-pre-wrap break-all bg-card rounded px-2 py-1 text-xs overflow-x-auto">
                  {call.output}
                </pre>
              </div>
            )}
          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  );
}
