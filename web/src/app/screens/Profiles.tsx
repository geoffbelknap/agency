import { useState, useEffect, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router';
import { RefreshCw, Plus } from 'lucide-react';
import { toast } from 'sonner';
import type { Profile, ProfileType } from '../types';
import { Button } from '../components/ui/button';
import { api, type RawProfile } from '../lib/api';
import { ProfileList } from './profiles/ProfileList';
import { ProfileDetail } from './profiles/ProfileDetail';

function mapProfile(p: RawProfile): Profile {
  return {
    id: p.id,
    type: (p.type || 'operator') as ProfileType,
    displayName: p.display_name || p.id,
    email: p.email,
    avatarUrl: p.avatar_url,
    bio: p.bio,
    status: p.status,
    settings: p.settings,
    createdAt: p.created_at,
    updatedAt: p.updated_at,
  };
}

type FilterType = 'all' | 'operator' | 'agent';

export function Profiles() {
  const { id: urlProfileId } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [filter, setFilter] = useState<FilterType>('all');

  const selectedProfile = urlProfileId ? profiles.find((p) => p.id === urlProfileId) ?? null : null;

  const load = useCallback(async () => {
    setRefreshing(true);
    try {
      const typeParam = filter === 'all' ? undefined : filter;
      const data = await api.profiles.list(typeParam);
      setProfiles((data ?? []).map(mapProfile));
    } catch (e) {
      console.error(e);
    } finally {
      setRefreshing(false);
      setLoading(false);
    }
  }, [filter]);

  useEffect(() => { load(); }, [load]);

  const handleSelect = (id: string) => {
    navigate(`/profiles/${encodeURIComponent(id)}`, { replace: true });
  };

  const handleCloseDetail = () => {
    navigate('/profiles', { replace: true });
  };

  const handleDeleted = (id: string) => {
    setProfiles((prev) => prev.filter((p) => p.id !== id));
    navigate('/profiles', { replace: true });
  };

  const handleCreateNew = async () => {
    const name = prompt('Profile ID (e.g. username or agent name):');
    if (!name) return;
    const type = prompt('Type (operator or agent):', 'operator') as ProfileType;
    if (type !== 'operator' && type !== 'agent') {
      toast.error('Type must be "operator" or "agent"');
      return;
    }
    try {
      await api.profiles.update(name, { type, display_name: name });
      toast.success(`Profile "${name}" created`);
      await load();
      navigate(`/profiles/${encodeURIComponent(name)}`, { replace: true });
    } catch (e: any) {
      toast.error(e.message || 'Failed to create profile');
    }
  };

  const filterButtons: { label: string; value: FilterType }[] = [
    { label: 'All', value: 'all' },
    { label: 'Operators', value: 'operator' },
    { label: 'Agents', value: 'agent' },
  ];

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="shrink-0 p-4 md:px-8 md:pt-6 md:pb-4 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-xl md:text-2xl text-foreground">Profiles</h1>
          <p className="text-sm text-muted-foreground mt-1">{profiles.length} profile{profiles.length !== 1 ? 's' : ''}</p>
        </div>
        <div className="flex items-center gap-2">
          {/* Type filter */}
          <div className="flex items-center rounded-md border border-border overflow-hidden text-xs">
            {filterButtons.map((f) => (
              <button
                key={f.value}
                onClick={() => setFilter(f.value)}
                className={`px-3 py-1.5 transition-colors ${
                  filter === f.value
                    ? 'bg-primary text-primary-foreground'
                    : 'text-muted-foreground hover:bg-muted'
                }`}
              >
                {f.label}
              </button>
            ))}
          </div>
          <Button variant="ghost" size="sm" className="h-8 px-3" onClick={handleCreateNew}>
            <Plus className="w-3.5 h-3.5 mr-1" aria-hidden="true" />
            Create
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="h-8 w-8 p-0"
            onClick={load}
            disabled={refreshing}
            aria-label={refreshing ? 'Refreshing profiles' : 'Refresh profiles'}
          >
            <RefreshCw className={`w-3.5 h-3.5 ${refreshing ? 'animate-spin' : ''}`} aria-hidden="true" />
          </Button>
        </div>
      </div>

      {/* Master-Detail split */}
      <div className="flex-1 min-h-0 flex flex-col">
        {/* Profile List — top portion */}
        <div className="shrink-0 max-h-[30vh] min-h-[120px] overflow-auto border-b border-border px-4 md:px-8">
          {loading ? (
            <div className="text-sm text-muted-foreground py-4">Loading profiles…</div>
          ) : (
            <ProfileList
              profiles={profiles}
              selectedId={selectedProfile?.id ?? null}
              onSelect={handleSelect}
            />
          )}
        </div>

        {/* Detail — bottom portion */}
        <div className="flex-1 min-h-0 overflow-auto">
          {selectedProfile ? (
            <ProfileDetail
              profile={selectedProfile}
              onClose={handleCloseDetail}
              onUpdated={load}
              onDeleted={handleDeleted}
            />
          ) : (
            <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
              Select a profile to view details
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
