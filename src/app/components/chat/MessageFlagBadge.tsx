import { Flag, CircleHelp, CheckCircle } from 'lucide-react';
import { Badge } from '../ui/badge';

type FlagType = 'DECISION' | 'BLOCKER' | 'QUESTION';

interface MessageFlagBadgeProps {
  flag: FlagType | null | undefined;
}

const flagConfig: Record<FlagType, { className: string; icon: React.ReactNode; label: string }> = {
  DECISION: {
    className: 'bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-400 border-green-200 dark:border-green-900',
    icon: <CheckCircle className="w-3 h-3" />,
    label: 'DECISION',
  },
  BLOCKER: {
    className: 'bg-red-50 dark:bg-red-950 text-red-700 dark:text-red-400 border-red-200 dark:border-red-900',
    icon: <Flag className="w-3 h-3" />,
    label: 'BLOCKER',
  },
  QUESTION: {
    className: 'bg-amber-50 dark:bg-amber-950 text-amber-700 dark:text-amber-400 border-amber-200 dark:border-amber-900',
    icon: <CircleHelp className="w-3 h-3" />,
    label: 'QUESTION',
  },
};

export function MessageFlagBadge({ flag }: MessageFlagBadgeProps) {
  if (!flag) return null;

  const config = flagConfig[flag];
  if (!config) return null;

  return (
    <Badge className={config.className}>
      {config.icon}
      {config.label}
    </Badge>
  );
}
