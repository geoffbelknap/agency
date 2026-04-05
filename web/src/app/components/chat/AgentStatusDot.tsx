import { Tooltip, TooltipTrigger, TooltipContent } from '../ui/tooltip';
import { cn } from '../ui/utils';

type AgentStatus = 'running' | 'idle' | 'halted' | 'unknown';

interface AgentStatusDotProps {
  status: AgentStatus;
  className?: string;
}

const colorMap: Record<AgentStatus, string> = {
  running: 'bg-green-500',
  idle: 'bg-yellow-500',
  halted: 'bg-red-500',
  unknown: 'bg-muted-foreground',
};

const labelMap: Record<AgentStatus, string> = {
  running: 'Running',
  idle: 'Idle',
  halted: 'Halted',
  unknown: 'Unknown',
};

export function AgentStatusDot({ status, className }: AgentStatusDotProps) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          className={cn('w-2 h-2 rounded-full inline-block', colorMap[status], className)}
          aria-label={labelMap[status]}
        />
      </TooltipTrigger>
      <TooltipContent>{labelMap[status]}</TooltipContent>
    </Tooltip>
  );
}
