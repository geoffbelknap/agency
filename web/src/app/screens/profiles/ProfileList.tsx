import type { Profile } from '../../types';

interface ProfileListProps {
  profiles: Profile[];
  selectedId: string | null;
  onSelect: (id: string) => void;
}

const typeBadge: Record<string, string> = {
  operator: 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400',
  agent: 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400',
};

export function ProfileList({ profiles, selectedId, onSelect }: ProfileListProps) {
  if (profiles.length === 0) {
    return <div className="text-sm text-muted-foreground py-4">No profiles found.</div>;
  }

  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="text-left text-[11px] font-medium text-muted-foreground uppercase tracking-wider">
          <th className="pb-2 pr-4">Name</th>
          <th className="pb-2 pr-4">Type</th>
          <th className="pb-2 pr-4">Email</th>
          <th className="pb-2 pr-4">Status</th>
          <th className="pb-2">Updated</th>
        </tr>
      </thead>
      <tbody>
        {profiles.map((p) => (
          <tr
            key={p.id}
            onClick={() => onSelect(p.id)}
            className={`cursor-pointer border-t border-border transition-colors hover:bg-muted/50 ${
              selectedId === p.id ? 'bg-muted/60' : ''
            }`}
          >
            <td className="py-2 pr-4 font-medium">{p.displayName || p.id}</td>
            <td className="py-2 pr-4">
              <span className={`inline-block px-2 py-0.5 rounded text-[11px] font-medium ${typeBadge[p.type] || 'bg-muted text-muted-foreground'}`}>
                {p.type}
              </span>
            </td>
            <td className="py-2 pr-4 text-muted-foreground">{p.email || '—'}</td>
            <td className="py-2 pr-4 text-muted-foreground">{p.status || '—'}</td>
            <td className="py-2 text-muted-foreground text-xs">{p.updatedAt || p.createdAt || '—'}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
