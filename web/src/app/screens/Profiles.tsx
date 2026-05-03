import { useState, useEffect, useCallback } from 'react';
import { Link, useParams, useNavigate } from 'react-router';
import { AlertTriangle, Plus, RefreshCw } from 'lucide-react';
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
        <div className="shrink-0 px-4 md:px-8 pb-4">
          <div className="rounded-lg border border-border bg-card p-4">
            <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
              <div className="space-y-1">
                <div className="text-sm font-medium text-foreground">Use profiles to separate operator identity from agent identity</div>
                <p className="text-xs text-muted-foreground">
                  Operator profiles are for people and their preferences. Agent profiles are for long-lived personas or system actors that need their own visible identity in channels and history.
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                  <Link to="/channels">Open Channels</Link>
                </Button>
                <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                  <Link to="/agents">Open Agents</Link>
                </Button>
              </div>
            </div>
          </div>
        </div>

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
            <div className="flex flex-col items-center justify-center h-full text-sm text-muted-foreground gap-2">
              <div className="flex items-center gap-2 text-sm font-medium text-amber-300">
                <AlertTriangle className="h-4 w-4" />
                Select a profile to view details
              </div>
              <p className="max-w-md text-center text-xs text-muted-foreground/80">
                Start with the operator profile you use most often, then create agent profiles only for assistants that need a persistent identity across chats and missions.
              </p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
