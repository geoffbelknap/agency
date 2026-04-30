import { useState, useEffect, useCallback } from 'react';
import { Link, useNavigate } from 'react-router';
import { AlertTriangle, Plus, RefreshCw, Workflow } from 'lucide-react';
import { Button } from '../components/ui/button';
import { Badge } from '../components/ui/badge';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '../components/ui/card';
import { api, type RawMission, type MissionHealthResponse } from '../lib/api';
import { socket } from '../lib/ws';
import { MissionWizard } from './MissionWizard';

const statusColors: Record<string, string> = {
  active: 'bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-400',
  paused: 'bg-amber-50 dark:bg-amber-950 text-amber-700 dark:text-amber-400',
  completed: 'bg-blue-50 dark:bg-blue-950 text-blue-700 dark:text-blue-400',
};

function StatusBadge({ status }: { status: string }) {
  const colors = statusColors[status] || 'bg-secondary text-muted-foreground';
  return (
    <span className={`inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium ${colors}`}>
      {status}
    </span>
  );
}

export function MissionList() {
  const navigate = useNavigate();
  const [missions, setMissions] = useState<RawMission[]>([]);
  const [healthMap, setHealthMap] = useState<Record<string, MissionHealthResponse>>({});
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [wizardOpen, setWizardOpen] = useState(false);

  const load = useCallback(async () => {
    setRefreshing(true);
    try {
      const [data, healthData] = await Promise.all([
        api.missions.list(),
        api.missions.health().catch(() => ({ missions: [] })),
      ]);
      setMissions(data ?? []);
      const hm: Record<string, MissionHealthResponse> = {};
      for (const h of ((healthData as any).missions ?? [])) {
        hm[h.mission] = h;
      }
      setHealthMap(hm);
    } catch (e) {
      console.error(e);
    } finally {
      setRefreshing(false);
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    const unsub = socket.on('agent_status', load);
    return unsub;
  }, [load]);

  const counts = missions.reduce<Record<string, number>>((acc, m) => {
    acc[m.status] = (acc[m.status] || 0) + 1;
    return acc;
  }, {});

  const breakdown = Object.entries(counts)
    .map(([status, count]) => `${count} ${status}`)
    .join(', ');

  const degradedMissions = missions.filter((mission) => healthMap[mission.name]?.status === 'degraded');
  const unhealthyMissions = missions.filter((mission) => healthMap[mission.name]?.status === 'unhealthy');
  const attentionMissions = [...unhealthyMissions, ...degradedMissions];

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64 text-muted-foreground">
        Loading missions...
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between px-4 py-4 md:px-6">
        <div className="text-sm text-muted-foreground">
          {missions.length} mission{missions.length !== 1 ? 's' : ''}
          {breakdown ? ` — ${breakdown}` : ''}
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={load} disabled={refreshing}>
            <RefreshCw className={refreshing ? 'animate-spin' : ''} />
          </Button>
          <Button size="sm" onClick={() => setWizardOpen(true)}>
            <Plus data-icon="inline-start" />
            New Mission
          </Button>
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-auto p-4 md:p-8">
        {attentionMissions.length > 0 && (
          <Card className="mb-4 border-amber-900/50 bg-amber-950/20">
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-sm text-amber-300">
                  <AlertTriangle className="h-4 w-4" />
                  {attentionMissions.length} mission{attentionMissions.length !== 1 ? 's' : ''} need attention
              </CardTitle>
              <CardDescription>
                  {unhealthyMissions.length > 0 && `${unhealthyMissions.length} unhealthy`}
                  {unhealthyMissions.length > 0 && degradedMissions.length > 0 && ' · '}
                  {degradedMissions.length > 0 && `${degradedMissions.length} degraded`}
                  {' · '}
                  Review mission health details, then use Doctor or Infrastructure if the issue is platform-wide.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-wrap gap-2">
              <Button asChild variant="outline" size="sm">
                <Link to="/admin/doctor">Open Doctor</Link>
              </Button>
              <Button asChild variant="outline" size="sm">
                <Link to="/admin/infrastructure">Open Infrastructure</Link>
              </Button>
            </CardContent>
          </Card>
        )}
        {missions.length === 0 ? (
          <Card className="min-h-[18rem] items-center justify-center text-center">
            <CardContent className="flex flex-col items-center justify-center gap-4 py-12 text-muted-foreground">
              <p>No missions yet. Create one to get started.</p>
              <Button size="sm" onClick={() => setWizardOpen(true)}>
                <Plus data-icon="inline-start" />
                Create Mission
              </Button>
            </CardContent>
          </Card>
        ) : (
          <div className="grid gap-3">
            {missions.map((mission) => (
              <Card
                key={mission.name}
                className="cursor-pointer gap-4 border-border/80 py-4 hover:border-primary/50"
                onClick={() => navigate('/missions/' + mission.name)}
              >
                <CardContent className="px-4">
                {/* Top row */}
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-2">
                    {(() => {
                      const h = healthMap[mission.name];
                      if (!h) return null;
                      const color = h.status === 'healthy' ? 'bg-emerald-500' : h.status === 'degraded' ? 'bg-amber-500' : h.status === 'unhealthy' ? 'bg-red-500' : 'bg-muted-foreground/30';
                      return <span className={`w-2 h-2 rounded-full flex-shrink-0 ${color}`} title={h.summary} />;
                    })()}
                    <span className="font-mono font-medium text-sm">{mission.name}</span>
                    {mission.has_canvas && <Workflow size={12} className="text-zinc-400" />}
                  </div>
                  <StatusBadge status={mission.status} />
                  {mission.cost_mode && (
                    <Badge
                      className={`text-[10px] ${
                        mission.cost_mode === 'frugal' ? 'bg-secondary text-muted-foreground' :
                        mission.cost_mode === 'balanced' ? 'bg-blue-100 text-blue-700 dark:bg-primary/20 dark:text-primary' :
                        'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400'
                      }`}
                    >
                      {mission.cost_mode}
                    </Badge>
                  )}
                </div>

                {/* Description */}
                {mission.description && (
                  <p className="text-sm text-muted-foreground mt-1 line-clamp-2">
                    {mission.description}
                  </p>
                )}

                {/* Bottom row */}
                <div className="flex items-center gap-4 mt-2 text-xs text-muted-foreground">
                  {mission.assigned_to && (
                    <div className="flex items-center gap-1.5">
                      <span className="inline-flex items-center justify-center h-5 w-5 rounded-full bg-primary/10 text-primary text-[10px] font-medium">
                        {mission.assigned_to.charAt(0).toUpperCase()}
                      </span>
                      <span>{mission.assigned_to}</span>
                    </div>
                  )}
                  {mission.triggers && mission.triggers.length > 0 && (
                    <span>
                      {mission.triggers.length} trigger{mission.triggers.length !== 1 ? 's' : ''}
                    </span>
                  )}
                  {healthMap[mission.name] && healthMap[mission.name].status !== 'healthy' && (
                    <span
                      className={healthMap[mission.name].status === 'unhealthy' ? 'text-red-400 text-[11px]' : 'text-amber-400 text-[11px]'}
                    >
                      {healthMap[mission.name].status}: {healthMap[mission.name].summary}
                    </span>
                  )}
                </div>
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </div>

      {/* Wizard */}
      <MissionWizard
        open={wizardOpen}
        onOpenChange={setWizardOpen}
        onComplete={load}
      />
    </div>
  );
}
