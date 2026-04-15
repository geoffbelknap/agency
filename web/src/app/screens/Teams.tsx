import { useState, useEffect, Fragment } from 'react';
import { Link } from 'react-router';
import { api } from '../lib/api';
import { Team } from '../types';
import { formatDateTimeShort } from '../lib/time';
import { Button } from '../components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '../components/ui/card';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { Input } from '../components/ui/input';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table';
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
    <div className="flex h-full flex-col gap-4 p-4 md:p-6">
      <div className="flex items-center justify-between">
        <div className="text-sm text-muted-foreground">{teams.length} team{teams.length !== 1 ? 's' : ''}</div>
        <Button size="sm" onClick={() => setShowCreateForm((v) => !v)}>
          <Plus data-icon="inline-start" />
          Create Team
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Use teams for shared ownership, not just naming</CardTitle>
          <CardDescription>
                Teams are most useful when several agents should share a mission area, activity view, or operator workflow. If an agent is standalone, keep it ungrouped until a real collaboration need appears.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-wrap gap-2">
          <Button asChild variant="outline" size="sm">
            <Link to="/agents">Open Agents</Link>
          </Button>
          <Button asChild variant="outline" size="sm">
            <Link to="/missions">Open Missions</Link>
          </Button>
        </CardContent>
      </Card>

      {showCreateForm && (
        <Card>
          <CardHeader>
            <CardTitle>Create team</CardTitle>
            <CardDescription>Define a shared ownership group only when several agents actually collaborate.</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2 sm:flex-row sm:items-center">
            <Input
              type="text"
              value={newTeamName}
              onChange={(e) => setNewTeamName(e.target.value)}
              placeholder="Team name..."
              className="flex-1"
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
          </CardContent>
        </Card>
      )}

      {error && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-400">
          {error}
        </div>
      )}

      <Card className="min-h-0 flex-1">
        <CardHeader>
          <CardTitle>Team directory</CardTitle>
          <CardDescription>Review membership, inspect recent activity, and remove stale groups.</CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="py-12 text-center text-sm text-muted-foreground">Loading teams...</div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Members</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-12 text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {teams.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className="p-12 text-center">
                      <AlertTriangle className="w-8 h-8 text-muted-foreground/70 mx-auto mb-3" />
                      <div className="text-sm text-muted-foreground mb-1">No teams yet</div>
                      <div className="text-xs text-muted-foreground/70">Create a team only when multiple agents should share ownership or mission context.</div>
                    </TableCell>
                  </TableRow>
                ) : (
                  teams.map((team) => (
                    <Fragment key={team.id}>
                      <TableRow
                        className="cursor-pointer"
                        onClick={() => handleRowClick(team.name)}
                      >
                        <TableCell>
                          <div className="flex items-center gap-2">
                            <Users className="w-4 h-4 text-muted-foreground/70" />
                            <code className="text-foreground">{team.name}</code>
                          </div>
                        </TableCell>
                        <TableCell>
                          <span className="text-xs text-muted-foreground">
                            {team.memberCount} member{team.memberCount !== 1 ? 's' : ''}
                          </span>
                        </TableCell>
                        <TableCell>
                          <span className="text-muted-foreground text-xs">{team.created}</span>
                        </TableCell>
                        <TableCell className="text-right">
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
                        </TableCell>
                      </TableRow>
                      {selectedTeam === team.name && (
                        <TableRow>
                          <TableCell colSpan={4} className="bg-background/50 p-4">
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
                          </TableCell>
                        </TableRow>
                      )}
                    </Fragment>
                  ))
                )}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

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
