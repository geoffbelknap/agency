import { type ComponentType, useCallback, useEffect, useMemo, useState } from 'react';
import { Link, Outlet, useLocation, useNavigate } from 'react-router';
import {
  Activity,
  Bot,
  Brain,
  Cable,
  ChevronDown,
  Command,
  FlaskConical,
  KeyRound,
  Layers3,
  LogOut,
  Menu,
  MessageSquare,
  Monitor,
  Moon,
  Package,
  PanelLeftClose,
  PanelLeftOpen,
  Route,
  Settings,
  ShieldCheck,
  Sun,
  Target,
  UserCircle,
  Users,
  X,
} from 'lucide-react';
import { socket } from '../lib/ws';
import { api, ensureConfig, getAuthenticated, getVia } from '../lib/api';
import { cn } from '../lib/utils';
import {
  contractModules,
  findModuleForSurface,
  findSurfaceForPath,
  moduleVisibleSurfaces,
  surfaceIsVisible,
  type ContractModule,
  type ContractSurface,
} from '../lib/contract-surface';
import { experimentalSurfacesEnabled } from '../lib/features';
import { useTheme, type Theme } from './ThemeProvider';
import { useVisualViewport } from '../hooks/useVisualViewport';
import { TextScaleControl } from './TextScaleControl';
import { Avatar, AvatarFallback } from './ui/avatar';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from './ui/dropdown-menu';

const DETAIL_STORAGE_KEY = 'agency-contract-detail-collapsed';

const moduleIcons: Record<string, ComponentType<{ className?: string }>> = {
  operate: Activity,
  govern: ShieldCheck,
  extend: FlaskConical,
};

const surfaceIcons: Record<string, ComponentType<{ className?: string }>> = {
  overview: Layers3,
  agents: Bot,
  channels: MessageSquare,
  knowledge: Brain,
  admin: Settings,
  setup: KeyRound,
  audit: ShieldCheck,
  mcp: Cable,
  missions: Target,
  teams: Users,
  profiles: UserCircle,
  events: Activity,
  hub: Package,
  intake: Route,
};

function AgencyMark() {
  return (
    <svg width="22" height="22" viewBox="0 0 52 52" aria-hidden="true" className="flex-shrink-0">
      <rect x="0" y="0" width="22" height="22" rx="3" className="fill-primary" />
      <rect x="26" y="0" width="22" height="22" rx="3" className="fill-foreground" />
      <rect x="0" y="26" width="22" height="22" rx="3" className="fill-foreground" />
      <rect x="26" y="26" width="22" height="22" rx="3" className="fill-foreground" />
    </svg>
  );
}

function tierVariant(surface: ContractSurface) {
  if (surface.tier === 'core') return 'secondary' as const;
  if (surface.tier === 'experimental') return 'outline' as const;
  return 'destructive' as const;
}

function ShellUserMenu({
  isRelay,
  isRelayAuthenticated,
  onSignOut,
}: {
  isRelay: boolean;
  isRelayAuthenticated: boolean;
  onSignOut: () => void | Promise<void>;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button className="flex w-full items-center gap-3 rounded-2xl border border-sidebar-border bg-background px-2.5 py-2 text-left hover:bg-sidebar-accent">
          <Avatar className="border border-sidebar-border bg-primary/10 text-primary">
            <AvatarFallback className="bg-transparent font-mono text-[11px] text-primary">OP</AvatarFallback>
          </Avatar>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium text-sidebar-foreground">Operator</div>
            <div className="truncate text-xs text-sidebar-foreground/56">
              {isRelay ? 'Relay session' : 'Local gateway'}
            </div>
          </div>
          <ChevronDown className="h-4 w-4 text-sidebar-foreground/50" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-60 rounded-xl">
        <DropdownMenuLabel className="px-3 py-2">
          <div className="text-sm font-medium">Operator workspace</div>
          <div className="mt-1 text-xs font-normal text-muted-foreground">
            {isRelay ? 'Authenticated through relay transport.' : 'Connected directly to the local gateway.'}
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem className="gap-2">
          <Command className="h-4 w-4" />
          <span>Command search</span>
        </DropdownMenuItem>
        <DropdownMenuItem asChild className="gap-2">
          <Link to="/admin">
            <Settings className="h-4 w-4" />
            <span>Workspace controls</span>
          </Link>
        </DropdownMenuItem>
        {isRelay && isRelayAuthenticated && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem className="gap-2 text-destructive focus:text-destructive" onClick={() => void onSignOut()}>
              <LogOut className="h-4 w-4" />
              <span>Sign out</span>
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function RailButton({
  module,
  active,
  onClick,
}: {
  module: ContractModule;
  active: boolean;
  onClick: () => void;
}) {
  const Icon = moduleIcons[module.id] ?? Layers3;
  const visibleCount = moduleVisibleSurfaces(module).length;

  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'group flex w-full flex-col items-center gap-1 rounded-2xl px-2 py-3 text-[11px] transition-colors',
        active
          ? 'bg-sidebar-accent text-sidebar-accent-foreground'
          : 'text-sidebar-foreground/58 hover:bg-sidebar-accent/70 hover:text-sidebar-foreground',
      )}
      aria-pressed={active}
    >
      <Icon className="h-5 w-5" />
      <span>{module.label}</span>
      <span className="rounded-full bg-background px-1.5 py-0.5 font-mono text-[10px] text-sidebar-foreground/52">
        {visibleCount}
      </span>
    </button>
  );
}

function SurfaceLink({
  surface,
  active,
  hasUnread,
  compact,
}: {
  surface: ContractSurface;
  active: boolean;
  hasUnread: boolean;
  compact?: boolean;
}) {
  const Icon = surfaceIcons[surface.id] ?? Layers3;

  return (
    <Link
      to={surface.route}
      title={compact ? surface.label : undefined}
      className={cn(
        'group flex items-start gap-3 rounded-2xl border px-3 py-3 transition-colors',
        active
          ? 'border-sidebar-border bg-background text-sidebar-foreground shadow-sm'
          : 'border-transparent text-sidebar-foreground/68 hover:border-sidebar-border hover:bg-sidebar-accent/60 hover:text-sidebar-foreground',
        compact && 'justify-center px-2',
      )}
    >
      <Icon className="mt-0.5 h-4 w-4 flex-shrink-0" />
      {!compact && (
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-2">
            <span className="truncate text-sm font-medium">{surface.label}</span>
            {hasUnread ? <span className="h-2 w-2 rounded-full bg-primary" /> : null}
          </span>
          <span className="mt-1 line-clamp-2 block text-xs leading-5 text-sidebar-foreground/52">
            {surface.summary}
          </span>
          <span className="mt-2 flex items-center gap-2">
            <Badge variant={tierVariant(surface)} className="rounded-full px-2 py-0 text-[10px] uppercase tracking-[0.12em]">
              {surface.tier}
            </Badge>
            <span className="truncate font-mono text-[10px] text-sidebar-foreground/42">{surface.tag}</span>
          </span>
        </span>
      )}
    </Link>
  );
}

export function Layout() {
  const location = useLocation();
  const navigate = useNavigate();
  const isChannelsRoute = location.pathname.startsWith('/channels');
  const [isConnected, setIsConnected] = useState(false);
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false);
  const [hasChannelUnread, setHasChannelUnread] = useState(false);
  const [detailCollapsed, setDetailCollapsed] = useState(() => localStorage.getItem(DETAIL_STORAGE_KEY) === 'true');
  const [setupChecked, setSetupChecked] = useState(false);
  const [isRelay, setIsRelay] = useState(false);
  const [isRelayAuthenticated, setIsRelayAuthenticated] = useState(false);
  const [startingServices, setStartingServices] = useState<string[]>([]);
  const [downServices, setDownServices] = useState<string[]>([]);

  const { theme, setTheme } = useTheme();
  useVisualViewport();

  const currentSurface = useMemo(() => findSurfaceForPath(location.pathname), [location.pathname]);
  const currentModule = useMemo(() => findModuleForSurface(currentSurface.id), [currentSurface.id]);
  const [selectedModuleId, setSelectedModuleId] = useState(currentModule.id);

  useEffect(() => {
    setSelectedModuleId(currentModule.id);
  }, [currentModule.id]);

  const selectedModule = contractModules.find((module) => module.id === selectedModuleId) ?? currentModule;
  const selectedSurfaces = moduleVisibleSurfaces(selectedModule);
  const visibleSurfaces = contractModules.flatMap(moduleVisibleSurfaces);

  useEffect(() => {
    ensureConfig().then(() => {
      setIsRelay(getVia() === 'relay');
      setIsRelayAuthenticated(getAuthenticated());
    });
  }, []);

  const handleSignOut = useCallback(async () => {
    try {
      await fetch('/auth/signout', { method: 'POST' });
    } catch {
      // best effort
    }
    window.location.reload();
  }, []);

  useEffect(() => {
    api.routing.config().then((cfg: any) => {
      if (cfg.configured === false) {
        navigate('/setup', { replace: true });
      } else {
        setSetupChecked(true);
      }
    }).catch(() => {
      setSetupChecked(true);
    });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const loadInfraHealth = useCallback(async () => {
    try {
      const infra = await api.infra.status();
      const components = infra.components ?? [];
      setStartingServices(
        components.filter((c: any) => c.state === 'running' && c.health !== 'healthy').map((c: any) => c.name),
      );
      setDownServices(
        components.filter((c: any) => c.state && c.state !== 'running' && c.state !== 'missing').map((c: any) => c.name),
      );
    } catch {
      setStartingServices([]);
      setDownServices([]);
    }
  }, []);

  useEffect(() => {
    void loadInfraHealth();
  }, [loadInfraHealth]);

  useEffect(() => {
    const unsub = socket.on('infra_status', () => void loadInfraHealth());
    return () => unsub();
  }, [loadInfraHealth]);

  useEffect(() => {
    socket.connect();
    const unsub = socket.onConnectionChange(setIsConnected);
    return () => unsub();
  }, []);

  useEffect(() => {
    const unsub = socket.on('message', () => {
      if (!location.pathname.startsWith('/channels')) {
        setHasChannelUnread(true);
      }
    });
    return () => unsub();
  }, [location.pathname]);

  useEffect(() => {
    if (location.pathname.startsWith('/channels')) {
      setHasChannelUnread(false);
    }
  }, [location.pathname]);

  useEffect(() => {
    setIsMobileMenuOpen(false);
  }, [location.pathname]);

  const cycleTheme = () => {
    const order: Theme[] = ['dark', 'light', 'system'];
    setTheme(order[(order.indexOf(theme) + 1) % order.length]);
  };

  const toggleDetail = () => {
    setDetailCollapsed((prev) => {
      const next = !prev;
      localStorage.setItem(DETAIL_STORAGE_KEY, String(next));
      return next;
    });
  };

  const ThemeIcon = theme === 'light' ? Sun : theme === 'system' ? Monitor : Moon;

  if (!setupChecked) {
    return <div className="min-h-screen bg-background" />;
  }

  return (
    <div className="flex h-dvh overflow-hidden bg-[radial-gradient(circle_at_top_left,hsl(var(--primary)/0.10),transparent_34%),hsl(var(--muted)/0.42)] text-foreground">
      <div className="fixed left-0 right-0 top-0 z-50 flex items-center justify-between border-b border-sidebar-border bg-background/95 px-4 py-3 backdrop-blur lg:hidden">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-xl border border-sidebar-border bg-sidebar">
            <AgencyMark />
          </div>
          <div className="min-w-0">
            <div className="truncate text-sm font-medium">{currentSurface.label}</div>
            <div className="truncate text-xs text-muted-foreground">{currentSurface.tag}</div>
          </div>
        </div>
        <button
          onClick={() => setIsMobileMenuOpen((open) => !open)}
          className="rounded-xl border border-sidebar-border bg-sidebar p-2 text-sidebar-foreground"
          aria-label={isMobileMenuOpen ? 'Close navigation menu' : 'Open navigation menu'}
          aria-expanded={isMobileMenuOpen}
        >
          {isMobileMenuOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
        </button>
      </div>

      <div
        className={cn(
          'fixed inset-y-0 left-0 z-40 w-[21rem] border-r border-sidebar-border bg-sidebar transition-transform lg:hidden',
          isMobileMenuOpen ? 'translate-x-0' : '-translate-x-full',
        )}
      >
        <div className="mt-header-mobile flex h-full flex-col">
          <div className="border-b border-sidebar-border px-4 py-4">
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-sidebar-border bg-background">
                <AgencyMark />
              </div>
              <div>
                <div style={{ fontFamily: 'var(--font-display)', fontWeight: 300 }} className="text-lg text-sidebar-foreground">
                  Agency
                </div>
                <div className="text-sm text-sidebar-foreground/58">Contract shell</div>
              </div>
            </div>
          </div>

          <nav className="flex-1 overflow-y-auto px-3 py-4">
            {contractModules.map((module) => {
              const surfaces = moduleVisibleSurfaces(module);
              if (surfaces.length === 0) return null;
              return (
                <div key={module.id} className="mb-5">
                  <div className="mb-2 px-3 text-[11px] font-medium uppercase tracking-[0.14em] text-sidebar-foreground/44">
                    {module.label}
                  </div>
                  <div className="space-y-1">
                    {surfaces.map((surface) => (
                      <SurfaceLink
                        key={surface.id}
                        surface={surface}
                        active={currentSurface.id === surface.id}
                        hasUnread={surface.id === 'channels' && hasChannelUnread && currentSurface.id !== 'channels'}
                      />
                    ))}
                  </div>
                </div>
              );
            })}
          </nav>

          <div className="border-t border-sidebar-border px-4 py-4">
            <div className="mb-3 flex items-center justify-between text-xs text-sidebar-foreground/60">
              <span>{isConnected ? 'Gateway online' : 'Gateway offline'}</span>
              {isRelay && <Badge className="rounded-full bg-primary/12 text-primary">Relay</Badge>}
            </div>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" className="flex-1 justify-start" onClick={cycleTheme}>
                <ThemeIcon className="h-4 w-4" />
                <span className="capitalize">{theme}</span>
              </Button>
              <div className="flex-1">
                <ShellUserMenu
                  isRelay={isRelay}
                  isRelayAuthenticated={isRelayAuthenticated}
                  onSignOut={handleSignOut}
                />
              </div>
            </div>
          </div>
        </div>
      </div>

      {isMobileMenuOpen && (
        <div className="fixed inset-0 z-30 bg-black/30 lg:hidden" onClick={() => setIsMobileMenuOpen(false)} />
      )}

      <aside className="hidden w-20 flex-shrink-0 flex-col border-r border-sidebar-border bg-sidebar/98 lg:flex">
        <div className="flex h-[4.5rem] items-center justify-center border-b border-sidebar-border">
          <Link to="/overview" className="flex h-11 w-11 items-center justify-center rounded-2xl border border-sidebar-border bg-background">
            <AgencyMark />
          </Link>
        </div>
        <div className="flex-1 space-y-2 overflow-y-auto px-2 py-3">
          {contractModules.map((module) => (
            <RailButton
              key={module.id}
              module={module}
              active={module.id === selectedModule.id}
              onClick={() => {
                setSelectedModuleId(module.id);
                if (detailCollapsed) toggleDetail();
              }}
            />
          ))}
        </div>
        <div className="border-t border-sidebar-border px-2 py-3">
          <button
            onClick={cycleTheme}
            className="mb-2 flex w-full items-center justify-center rounded-2xl px-2 py-3 text-sidebar-foreground/58 hover:bg-sidebar-accent hover:text-sidebar-foreground"
            aria-label={`Theme: ${theme}`}
          >
            <ThemeIcon className="h-5 w-5" />
          </button>
          <div className="flex justify-center rounded-2xl px-2 py-3 text-sidebar-foreground/58">
            <TextScaleControl />
          </div>
        </div>
      </aside>

      <aside
        className={cn(
          'hidden flex-shrink-0 flex-col border-r border-sidebar-border bg-sidebar transition-[width] lg:flex',
          detailCollapsed ? 'w-0 overflow-hidden border-r-0' : 'w-80',
        )}
      >
        <div className="border-b border-sidebar-border px-4 py-4">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <div className="text-[11px] font-medium uppercase tracking-[0.14em] text-sidebar-foreground/44">
                {selectedModule.eyebrow}
              </div>
              <div style={{ fontFamily: 'var(--font-display)', fontWeight: 300 }} className="mt-1 text-2xl leading-none text-sidebar-foreground">
                {selectedModule.label}
              </div>
              <p className="mt-2 text-xs leading-5 text-sidebar-foreground/58">{selectedModule.summary}</p>
            </div>
            <button
              onClick={toggleDetail}
              className="rounded-xl p-2 text-sidebar-foreground/58 hover:bg-sidebar-accent hover:text-sidebar-foreground"
              aria-label="Collapse contract navigation"
            >
              <PanelLeftClose className="h-4 w-4" />
            </button>
          </div>
        </div>

        <nav className="flex-1 overflow-y-auto px-3 py-3">
          <div className="space-y-2">
            {selectedSurfaces.map((surface) => (
              <SurfaceLink
                key={surface.id}
                surface={surface}
                active={currentSurface.id === surface.id}
                hasUnread={surface.id === 'channels' && hasChannelUnread && currentSurface.id !== 'channels'}
              />
            ))}
          </div>

          {experimentalSurfacesEnabled ? (
            <div className="mt-4 rounded-2xl border border-sidebar-border bg-background px-3 py-3 text-xs leading-5 text-sidebar-foreground/64">
              Experimental registry surfaces are visible. Internal surfaces remain hidden unless promoted.
            </div>
          ) : (
            <div className="mt-4 rounded-2xl border border-sidebar-border bg-background px-3 py-3 text-xs leading-5 text-sidebar-foreground/64">
              Experimental registry surfaces are gated. Enable them explicitly to expose those routes.
            </div>
          )}
        </nav>

        <div className="border-t border-sidebar-border px-3 py-3">
          <div className="mb-3 flex items-center justify-between rounded-2xl border border-sidebar-border bg-background px-3 py-2 text-xs text-sidebar-foreground/64">
            <span>{isConnected ? 'Gateway online' : 'Gateway offline'}</span>
            {isRelay && <Badge className="rounded-full bg-primary/12 text-primary">Relay</Badge>}
          </div>
          <ShellUserMenu
            isRelay={isRelay}
            isRelayAuthenticated={isRelayAuthenticated}
            onSignOut={handleSignOut}
          />
        </div>
      </aside>

      {detailCollapsed && (
        <button
          onClick={toggleDetail}
          className="hidden w-9 flex-shrink-0 items-center justify-center border-r border-sidebar-border bg-sidebar text-sidebar-foreground/58 hover:bg-sidebar-accent hover:text-sidebar-foreground lg:flex"
          aria-label="Expand contract navigation"
        >
          <PanelLeftOpen className="h-4 w-4" />
        </button>
      )}

      <div className="min-w-0 flex-1 pt-header-mobile lg:pt-0">
        <div className="flex h-full flex-col p-2 lg:p-3">
          <div className="flex h-full min-h-0 flex-col overflow-hidden rounded-[1.35rem] border border-border bg-background/96 shadow-sm backdrop-blur">
            {startingServices.length > 0 && (
              <div role="alert" className="border-b border-amber-200 bg-amber-50 px-4 py-2 text-sm text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-300">
                {startingServices.join(', ')} starting up...
              </div>
            )}
            {downServices.length > 0 && (
              <div className="border-b border-red-200 bg-red-50 px-4 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/20 dark:text-red-300">
                {downServices.join(', ')} {downServices.length === 1 ? 'is' : 'are'} down
              </div>
            )}

            <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border bg-muted/20 px-4 py-3 md:px-6">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <Badge variant={tierVariant(currentSurface)} className="rounded-full px-2.5 py-0.5 text-[10px] uppercase tracking-[0.14em]">
                  {currentSurface.tier}
                </Badge>
                <span className="truncate text-sm font-medium text-foreground">{currentSurface.label}</span>
                <span className="hidden h-1 w-1 rounded-full bg-muted-foreground/35 sm:block" />
                <span className="font-mono text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
                  {currentSurface.tag}
                </span>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="outline" className="rounded-full">
                  {isConnected ? 'Live session' : 'Offline'}
                </Badge>
                <Badge variant="outline" className="rounded-full">
                  {visibleSurfaces.length} visible surfaces
                </Badge>
              </div>
            </header>

            <main className={cn('min-h-0 flex-1', isChannelsRoute ? 'overflow-hidden' : 'overflow-auto')}>
              <Outlet />
            </main>
          </div>
        </div>
      </div>
    </div>
  );
}
