import { useState, useEffect, Fragment } from 'react';
import { Link } from 'react-router';
import { api } from '../lib/api';
import { Team } from '../types';
import { formatDateTimeShort } from '../lib/time';
import { Button } from '../components/ui/button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { AlertTriangle, Plus, Trash2, Users } from 'lucide-react';

interface TeamActivity {
  id: string;
  timestamp: string;
  type: string;
  message: string;
}

export function Teams() {
  const [teams, setTeams] = useState<Team[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedTeam, setSelectedTeam] = useState<string | null>(null);
  const [activity, setActivity] = useState<TeamActivity[]>([]);
  const [activityLoading, setActivityLoading] = useState(false);
  const [members, setMembers] = useState<string[]>([]);
  const [newTeamName, setNewTeamName] = useState('');
  const [creating, setCreating] = useState(false);
  const [showCreateForm, setShowCreateForm] = useState(false);
  const [teamToDelete, setTeamToDelete] = useState<string | null>(null);
  const [deletingTeam, setDeletingTeam] = useState(false);

  const loadTeams = async () => {
    try {
      setLoading(true);
      setError(null);
      const raw = await api.teams.list();
      const mapped: Team[] = (raw ?? []).map((t: any) => ({
        id: t.name,
        name: t.name,
        memberCount: t.member_count || 0,
        created: t.created || '',
      }));
      setTeams(mapped);
    } catch (e: any) {
      setError(e.message || 'Failed to load teams');
    } finally {
      setLoading(false);
    }
  };

  const loadActivity = async (name: string) => {
    try {
      setActivityLoading(true);
      const activityRaw = await api.teams.activity(name);
      const mapped: TeamActivity[] = (activityRaw ?? []).map((e: any, i: number) => ({
        id: e.id || `${name}-${e.timestamp || ''}-${i}`,
        timestamp: e.timestamp || '',
        type: e.event || e.type || '',
        message: e.detail || e.event || '',
      }));
      setActivity(mapped);
    } catch {
      setActivity([]);
    } finally {
      setActivityLoading(false);
    }
  };

  const handleCreateTeam = async () => {
    if (!newTeamName.trim()) return;
    try {
      setCreating(true);
      await api.teams.create(newTeamName.trim(), []);
      setNewTeamName('');
      setShowCreateForm(false);
      await loadTeams();
    } catch (e: any) {
      setError(e.message || 'Failed to create team');
    } finally {
      setCreating(false);
    }
  };

  const handleRowClick = (teamName: string) => {
    if (selectedTeam === teamName) {
      setSelectedTeam(null);
      setActivity([]);
      setMembers([]);
    } else {
      setSelectedTeam(teamName);
      loadActivity(teamName);
      api.teams.show(teamName).then((data: any) => {
        const raw = Array.isArray(data.members) ? data.members : [];
        // Normalize: members may be strings or objects with a name property
        setMembers(raw.map((m: any) => (typeof m === 'string' ? m : m?.name ?? String(m))));
      }).catch(() => setMembers([]));
    }
  };

  const handleDeleteTeam = async () => {
    if (!teamToDelete) return;
    try {
      setDeletingTeam(true);
      setError(null);
      await api.teams.delete(teamToDelete);
      setTeams((current) => current.filter((team) => team.name !== teamToDelete));
      if (selectedTeam === teamToDelete) {
        setSelectedTeam(null);
        setActivity([]);
        setMembers([]);
      }
      setTeamToDelete(null);
    } catch (e: any) {
      setError(e.message || 'Failed to delete team');
    } finally {
      setDeletingTeam(false);
    }
  };

  useEffect(() => {
    loadTeams();
  }, []);

  return (
    <div className="h-full flex flex-col">
      {/* Header */}
      <div className="border-b border-border px-4 md:px-8 py-4 flex items-center justify-between">
        <div>
          <h1 className="text-xl text-foreground">Teams</h1>
          <p className="text-sm text-muted-foreground mt-0.5">Manage agent groups</p>
        </div>
        <Button size="sm" onClick={() => setShowCreateForm((v) => !v)}>
          <Plus className="w-4 h-4 mr-1" />
          Create Team
        </Button>
      </div>

      {/* Content */}
      <div className="flex-1 p-4 md:p-8 overflow-auto">
        <div className="mb-4 rounded-lg border border-border bg-card p-4">
          <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
            <div className="space-y-1">
              <div className="text-sm font-medium text-foreground">Use teams for shared ownership, not just naming</div>
              <p className="text-xs text-muted-foreground">
                Teams are most useful when several agents should share a mission area, activity view, or operator workflow. If an agent is standalone, keep it ungrouped until a real collaboration need appears.
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                <Link to="/agents">Open Agents</Link>
              </Button>
              <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                <Link to="/missions">Open Missions</Link>
              </Button>
            </div>
          </div>
        </div>

        {showCreateForm && (
          <div className="mb-4 flex gap-2 items-center bg-card border border-border rounded p-3">
            <input
              type="text"
              value={newTeamName}
              onChange={(e) => setNewTeamName(e.target.value)}
              placeholder="Team name..."
              className="flex-1 bg-background border border-border text-foreground rounded px-3 py-1.5 text-sm placeholder:text-muted-foreground/70"
              onKeyDown={(e) => e.key === 'Enter' && handleCreateTeam()}
            />
            <Button size="sm" onClick={handleCreateTeam} disabled={creating}>
              {creating ? 'Creating...' : 'Create'}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setShowCreateForm(false);
                setNewTeamName('');
              }}
            >
              Cancel
            </Button>
          </div>
        )}

        {error && (
          <div className="mb-4 text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
            {error}
          </div>
        )}

        {loading ? (
          <div className="text-sm text-muted-foreground text-center py-12">Loading teams...</div>
        ) : (
          <div className="bg-card border border-border rounded overflow-x-auto">
            <table className="w-full text-sm min-w-[480px]">
              <thead>
                <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
                  <th className="text-left p-4 font-medium">Name</th>
                  <th className="text-left p-4 font-medium">Members</th>
                  <th className="text-left p-4 font-medium">Created</th>
                  <th className="text-left p-4 font-medium sr-only">Actions</th>
                </tr>
              </thead>
              <tbody>
                {teams.length === 0 ? (
                  <tr>
                    <td colSpan={4} className="p-12 text-center">
                      <AlertTriangle className="w-8 h-8 text-muted-foreground/70 mx-auto mb-3" />
                      <div className="text-sm text-muted-foreground mb-1">No teams yet</div>
                      <div className="text-xs text-muted-foreground/70">Create a team only when multiple agents should share ownership or mission context.</div>
                    </td>
                  </tr>
                ) : (
                  teams.map((team) => (
                    <Fragment key={team.id}>
                      <tr
                        className="border-b border-border hover:bg-secondary/50 cursor-pointer transition-colors"
                        onClick={() => handleRowClick(team.name)}
                      >
                        <td className="p-4">
                          <div className="flex items-center gap-2">
                            <Users className="w-4 h-4 text-muted-foreground/70" />
                            <code className="text-foreground">{team.name}</code>
                          </div>
                        </td>
                        <td className="p-4">
                          <span className="text-xs text-muted-foreground">
                            {team.memberCount} member{team.memberCount !== 1 ? 's' : ''}
                          </span>
                        </td>
                        <td className="p-4">
                          <span className="text-muted-foreground text-xs">{team.created}</span>
                        </td>
                        <td className="p-4 w-0">
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            className="text-muted-foreground hover:text-red-600"
                            aria-label={`Delete team ${team.name}`}
                            onClick={(event) => {
                              event.stopPropagation();
                              setTeamToDelete(team.name);
                            }}
                          >
                            <Trash2 className="w-4 h-4" />
                          </Button>
                        </td>
                      </tr>
                      {selectedTeam === team.name && (
                        <tr className="border-b border-border">
                          <td colSpan={4} className="p-4 bg-background">
                            {members.length > 0 && (
                              <div className="mb-3">
                                <div className="text-xs text-muted-foreground uppercase tracking-wide mb-1">Members</div>
                                <div className="flex flex-wrap gap-1">
                                  {members.map((m) => (
                                    <span key={m} className="text-xs bg-secondary text-foreground/80 px-2 py-0.5 rounded">{m}</span>
                                  ))}
                                </div>
                              </div>
                            )}
                            <div className="text-xs text-muted-foreground uppercase tracking-wide mb-2">
                              Activity — {team.name}
                            </div>
                            {activityLoading ? (
                              <div className="text-xs text-muted-foreground py-2">Loading activity...</div>
                            ) : activity.length === 0 ? (
                              <div className="text-xs text-muted-foreground/70 py-2">No activity found</div>
                            ) : (
                              <div className="space-y-1 max-h-48 overflow-y-auto">
                                {activity.map((e) => (
                                  <div key={e.id} className="flex gap-3 text-xs">
                                    <span className="text-muted-foreground shrink-0">{formatDateTimeShort(e.timestamp)}</span>
                                    <span className="text-muted-foreground shrink-0">{e.type}</span>
                                    <span className="text-muted-foreground">{e.message}</span>
                                  </div>
                                ))}
                              </div>
                            )}
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  ))
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <ConfirmDialog
        open={teamToDelete !== null}
        onOpenChange={(open) => {
          if (!open && !deletingTeam) setTeamToDelete(null);
        }}
        title={teamToDelete ? `Delete team "${teamToDelete}"?` : 'Delete team'}
        description="This removes the team definition and any live test cleanup path that depends on it. This action cannot be undone."
        confirmLabel={deletingTeam ? 'Deleting...' : 'Delete'}
        variant="destructive"
        onConfirm={handleDeleteTeam}
      />
    </div>
  );
}
