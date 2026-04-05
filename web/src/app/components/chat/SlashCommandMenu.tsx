import { useState, useEffect, useCallback } from 'react';

interface SlashCommand {
  name: string;
  description: string;
}

const COMMANDS: SlashCommand[] = [
  { name: '/summarize', description: 'Summarize the conversation' },
  { name: '/task', description: 'Create a task for an agent' },
  { name: '/status', description: 'Show agent status overview' },
  { name: '/help', description: 'Show available commands' },
];

interface SlashCommandMenuProps {
  filter: string;
  onSelect: (command: string) => void;
  onClose: () => void;
}

export function SlashCommandMenu({ filter, onSelect, onClose }: SlashCommandMenuProps) {
  const [selectedIndex, setSelectedIndex] = useState(0);

  const filtered = COMMANDS.filter((cmd) =>
    cmd.name.slice(1).toLowerCase().startsWith(filter.toLowerCase())
  );

  // Reset selected index when filter changes
  useEffect(() => {
    setSelectedIndex(0);
  }, [filter]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
        return;
      }
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setSelectedIndex((i) => (i + 1) % Math.max(filtered.length, 1));
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        setSelectedIndex((i) => (i - 1 + Math.max(filtered.length, 1)) % Math.max(filtered.length, 1));
        return;
      }
      if ((e.key === 'Enter' || e.key === 'Tab') && filtered.length > 0) {
        e.preventDefault();
        onSelect(filtered[selectedIndex]?.name ?? filtered[0].name);
        return;
      }
    },
    [filtered, selectedIndex, onSelect, onClose]
  );

  return (
    <div
      className="absolute bottom-full mb-1 left-0 w-72 bg-card border border-border rounded-md shadow-lg overflow-hidden z-50"
      onKeyDown={handleKeyDown}
    >
      <div className="px-2 py-1.5 text-xs text-muted-foreground border-b border-border">
        Commands
      </div>
      <div role="listbox" aria-label="Commands" className="max-h-48 overflow-y-auto py-1">
        {filtered.length === 0 ? (
          <div className="px-3 py-2 text-sm text-muted-foreground">No matching commands</div>
        ) : (
          filtered.map((cmd, i) => (
            <button
              key={cmd.name}
              role="option"
              aria-selected={i === selectedIndex}
              data-selected={i === selectedIndex}
              className={`w-full px-3 py-2 flex items-baseline gap-2 text-sm text-left transition-colors ${
                i === selectedIndex
                  ? 'bg-primary/20 text-primary'
                  : 'text-foreground/80 hover:bg-accent'
              }`}
              onMouseDown={(e) => {
                e.preventDefault(); // prevent blur on input
                onSelect(cmd.name);
              }}
              onMouseEnter={() => setSelectedIndex(i)}
              onKeyDown={handleKeyDown}
            >
              <span className="font-bold text-foreground">{cmd.name}</span>
              <span className="text-muted-foreground text-xs">{cmd.description}</span>
            </button>
          ))
        )}
      </div>
    </div>
  );
}
