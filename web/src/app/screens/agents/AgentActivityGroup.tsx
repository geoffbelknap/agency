import { useState } from 'react';
import { AgentMemoryPanel } from './AgentMemoryPanel';

interface Props {
  agentName: string;
  activityContent: React.ReactNode;
}

export function AgentActivityGroup({ agentName, activityContent }: Props) {
  const [subTab, setSubTab] = useState<'feed' | 'memory'>('feed');
  return (
    <div className="flex flex-col h-full">
      <div className="flex gap-2 px-2 py-1 border-b border-border">
        {(['feed', 'memory'] as const).map((t) => (
          <button key={t} onClick={() => setSubTab(t)}
            className={`text-xs px-2 py-1 rounded capitalize transition-colors focus-visible:ring-2 focus-visible:ring-primary/50 ${
              subTab === t ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:text-foreground'
            }`}>
            {t === 'feed' ? 'Feed' : 'Memory'}
          </button>
        ))}
      </div>
      <div className="flex-1 overflow-auto">
        {subTab === 'feed' ? activityContent : <AgentMemoryPanel agentName={agentName} />}
      </div>
    </div>
  );
}
