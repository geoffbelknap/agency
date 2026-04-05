import { useState } from 'react';
import { ChevronRight, ChevronDown } from 'lucide-react';

interface JsonViewProps {
  data: any;
  defaultExpanded?: boolean;
}

function JsonSection({ label, data, defaultOpen = false }: { label: string; data: any; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="border-b border-border last:border-0">
      <button
        className="flex items-center gap-2 w-full text-left px-3 py-2 hover:bg-secondary/50 transition-colors"
        onClick={() => setOpen(!open)}
      >
        {open ? (
          <ChevronDown className="w-3 h-3 text-muted-foreground" />
        ) : (
          <ChevronRight className="w-3 h-3 text-muted-foreground" />
        )}
        <span className="text-xs font-medium text-foreground/80">{label}</span>
        {!open && (
          <span className="text-[10px] text-muted-foreground/70 ml-auto">
            {Array.isArray(data) ? `${data.length} items` : typeof data === 'object' && data ? `${Object.keys(data).length} keys` : String(data)}
          </span>
        )}
      </button>
      {open && (
        <pre className="px-3 pb-3 font-mono text-xs text-muted-foreground overflow-x-auto">
          {JSON.stringify(data, null, 2)}
        </pre>
      )}
    </div>
  );
}

export function JsonView({ data, defaultExpanded = false }: JsonViewProps) {
  if (!data || typeof data !== 'object') {
    return <pre className="font-mono text-xs text-muted-foreground">{JSON.stringify(data, null, 2)}</pre>;
  }

  const keys = Object.keys(data);

  return (
    <div className="bg-card border border-border rounded overflow-hidden">
      {keys.map((key) => (
        <JsonSection key={key} label={key} data={data[key]} defaultOpen={defaultExpanded} />
      ))}
    </div>
  );
}
