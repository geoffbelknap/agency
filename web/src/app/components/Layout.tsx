import { Link, Outlet, useLocation, useNavigate } from 'react-router';
import {
  Bot,
  Brain,
  Cable,
  LogOut,
  Menu,
  MessageSquare,
  Monitor,
  Moon,
  Package,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  Settings,
  Sun,
  Target,
  UserCircle,
  Users,
  X,
} from 'lucide-react';
import { useState, useEffect, useCallback } from 'react';
import { socket } from '../lib/ws';
import { api, ensureConfig, getVia, getAuthenticated, type RawInfraService } from '../lib/api';
import { experimentalSurfacesEnabled, featureEnabled, type FeatureId } from '../lib/features';
import { useVisualViewport } from '../hooks/useVisualViewport';
import { TextScaleControl } from './TextScaleControl';
import { useTheme, type Theme } from './ThemeProvider';

type NavItem = { name: string; path: string; icon: any; feature?: FeatureId; group: 'Work' | 'Knowledge' | 'Operations' };

const allNav: NavItem[] = [
  { name: 'Channels', path: '/channels', icon: MessageSquare, group: 'Work' },
  { name: 'Agents', path: '/agents', icon: Bot, group: 'Work' },
  { name: 'Missions', path: '/missions', icon: Target, feature: 'missions', group: 'Work' },
  { name: 'Teams', path: '/teams', icon: Users, feature: 'teams', group: 'Work' },
  { name: 'Knowledge', path: '/knowledge', icon: Brain, group: 'Knowledge' },
  { name: 'Profiles', path: '/profiles', icon: UserCircle, feature: 'profiles', group: 'Knowledge' },
  { name: 'Hub', path: '/admin/hub', icon: Package, feature: 'hub', group: 'Operations' },
  { name: 'Intake', path: '/admin/intake', icon: Cable, feature: 'intake', group: 'Operations' },
  { name: 'Admin', path: '/admin', icon: Settings, group: 'Operations' },
];

const navItems = allNav.filter((item) => !item.feature || featureEnabled(item.feature));
const COMPACT_STORAGE_KEY = 'agency-sidebar-compact';
const DESKTOP_CHROME_HEADER_HEIGHT = 58;

const shellHeaders = [
  { path: '/admin', eyebrow: 'Admin', title: 'Platform control' },
  { path: '/agents', eyebrow: 'Agents', title: 'Your agents' },
  { path: '/channels', eyebrow: 'Channels', title: 'Conversations' },
  { path: '/knowledge', eyebrow: 'Knowledge', title: 'Graph & sources' },
  { path: '/missions', eyebrow: 'Missions', title: 'Mission control' },
  { path: '/teams', eyebrow: 'Teams', title: 'Teams' },
  { path: '/profiles', eyebrow: 'Profiles', title: 'Profiles' },
  { path: '/overview', eyebrow: 'Today', title: 'Today' },
];

function infraDotClass(component: Pick<RawInfraService, 'state' | 'health'>) {
  const state = String(component.state || '').toLowerCase();
  const health = String(component.health || '').toLowerCase();
  if (health === 'healthy') return 'bg-primary';
  if (health === 'unhealthy') return 'bg-destructive';
  if (state === 'running' || state === 'starting' || state === 'created' || state === 'restarting') return 'bg-amber-500';
  if (state === 'missing' || state === 'stopped' || state === 'exited' || state === 'dead') return 'bg-destructive';
  return 'bg-muted-foreground/50';
}

function AgencyMark() {
  return (
    <svg width="20" height="20" viewBox="0 0 52 52" aria-hidden="true" className="flex-shrink-0">
      <rect x="0" y="0" width="22" height="22" rx="3" className="fill-primary" />
      <rect x="26" y="0" width="22" height="22" rx="3" className="fill-foreground" />
      <rect x="0" y="26" width="22" height="22" rx="3" className="fill-foreground" />
      <rect x="26" y="26" width="22" height="22" rx="3" className="fill-foreground" />
    </svg>
  );
}

function matchPath(pathname: string, path: string) {
  return pathname === path || pathname.startsWith(path + '/');
}

function shellHeaderFor(pathname: string) {
  return shellHeaders.find((item) => matchPath(pathname, item.path)) ?? shellHeaders[shellHeaders.length - 1];
}

export function Layout() {
  const location = useLocation();
  const navigate = useNavigate();
  const isChannelsRoute = location.pathname.startsWith('/channels');
  const isFixedWorkspaceRoute = isChannelsRoute || location.pathname.startsWith('/knowledge') || location.pathname.startsWith('/admin');
  const suppressShellHeader = location.pathname.startsWith('/agents') || location.pathname.startsWith('/knowledge') || location.pathname.startsWith('/admin') || location.pathname.startsWith('/channels');
  const shellHeader = shellHeaderFor(location.pathname);
  const [isConnected, setIsConnected] = useState(false);
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false);
  const [hasChannelUnread, setHasChannelUnread] = useState(false);
  const [compactSidebar, setCompactSidebar] = useState(() => localStorage.getItem(COMPACT_STORAGE_KEY) === 'true');
  const [setupChecked, setSetupChecked] = useState(false);
  const [startingServices, setStartingServices] = useState<string[]>([]);
  const [downServices, setDownServices] = useState<string[]>([]);
  const [infraComponents, setInfraComponents] = useState<RawInfraService[]>([]);
  const [infraBuildId, setInfraBuildId] = useState('');
  const [isRelay, setIsRelay] = useState(false);
  const [isRelayAuthenticated, setIsRelayAuthenticated] = useState(false);
  const { theme, setTheme } = useTheme();

  useVisualViewport();

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
      // best-effort
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
      setInfraComponents(components);
      setInfraBuildId(infra.build_id || '');
      setStartingServices(
        components.filter((c: any) => c.state === 'running' && c.health !== 'healthy').map((c: any) => c.name),
      );
      setDownServices(
        components.filter((c: any) => c.state !== 'running' && c.state !== 'missing').map((c: any) => c.name),
      );
    } catch {
      setStartingServices([]);
      setDownServices([]);
      setInfraComponents([]);
      setInfraBuildId('');
    }
  }, []);

  useEffect(() => { loadInfraHealth(); }, [loadInfraHealth]);
  useEffect(() => {
    const unsub = socket.on('infra_status', () => loadInfraHealth());
    return () => { unsub(); };
  }, [loadInfraHealth]);

  useEffect(() => {
    socket.connect();
    const unsub = socket.onConnectionChange(setIsConnected);
    return () => { unsub(); };
  }, []);

  useEffect(() => {
    const unsub = socket.on('message', () => {
      if (!location.pathname.startsWith('/channels')) {
        setHasChannelUnread(true);
      }
    });
    return () => { unsub(); };
  }, [location.pathname]);

  useEffect(() => {
    if (location.pathname.startsWith('/channels')) {
      setHasChannelUnread(false);
    }
    setIsMobileMenuOpen(false);
  }, [location.pathname]);

  const toggleCompactSidebar = () => {
    setCompactSidebar((prev) => {
      const next = !prev;
      localStorage.setItem(COMPACT_STORAGE_KEY, String(next));
      return next;
    });
  };

  const cycleTheme = () => {
    const order: Theme[] = ['dark', 'light', 'system'];
    const next = order[(order.indexOf(theme) + 1) % order.length];
    setTheme(next);
  };

  const ThemeIcon = theme === 'light' ? Sun : theme === 'system' ? Monitor : Moon;
  const gatewayDetail = isRelay ? 'relay transport' : '127.0.0.1:8200';

  const groupedNav = {
    Work: navItems.filter((item) => item.group === 'Work'),
    Knowledge: navItems.filter((item) => item.group === 'Knowledge'),
    Operations: navItems.filter((item) => item.group === 'Operations'),
  };

  if (!setupChecked) {
    return <div className="min-h-screen bg-background" />;
  }

  const renderNavLink = (item: NavItem, mobile = false) => {
    const isActive = matchPath(location.pathname, item.path);
    const Icon = item.icon;
    const showUnread = item.path === '/channels' && hasChannelUnread && !isActive;
    const compact = compactSidebar && !mobile;

    return (
      <Link
        key={item.path}
        to={item.path === '/admin' ? '/admin/infrastructure' : item.path}
        title={compact ? item.name : undefined}
        className={[
          'group flex items-center transition-colors duration-150',
          mobile ? 'gap-3 rounded-xl px-3 py-2.5' : compact ? 'justify-center px-0 py-3' : 'gap-3 rounded-xl px-3 py-2.5',
          compact
            ? (isActive ? 'text-primary' : 'text-sidebar-foreground/62 hover:text-foreground')
            : (isActive
                ? 'bg-background text-foreground ring-[0.5px] ring-border'
                : 'text-sidebar-foreground/74 hover:bg-background/70 hover:text-foreground'),
        ].join(' ')}
      >
        <span className={[
          'flex flex-shrink-0 items-center justify-center',
          compact ? 'h-6 w-6 rounded-none' : 'h-8 w-8 rounded-lg',
          compact ? '' : (isActive ? 'bg-primary/12 text-primary' : 'text-sidebar-foreground/60'),
        ].join(' ')}>
          <Icon className="h-4 w-4" />
        </span>
        {(!compact || mobile) && (
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm font-medium">{item.name}</span>
            {item.feature && <span className="block text-[10px] uppercase tracking-[0.12em] text-sidebar-foreground/46">Experimental</span>}
          </span>
        )}
        {showUnread && <span className="h-2.5 w-2.5 rounded-full bg-primary" aria-hidden="true" />}
      </Link>
    );
  };

  return (
    <div className="flex h-dvh overflow-hidden border border-border bg-background text-foreground">
      <div className="fixed inset-x-0 top-0 z-50 border-b border-border bg-background/95 backdrop-blur lg:hidden">
        <div className="flex items-center justify-between px-4 py-3 safe-top">
          <Link to="/overview" className="flex items-center gap-3" style={{ textDecoration: 'none' }} aria-label="Go to Today">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-border bg-card">
              <AgencyMark />
            </div>
            <div>
              <div className="text-lg text-foreground" style={{ fontFamily: 'var(--font-display)', fontWeight: 400 }}>Agency</div>
              <div className="text-[10px] uppercase tracking-[0.14em] text-muted-foreground">Operator workspace</div>
            </div>
          </Link>
          <button
            onClick={() => setIsMobileMenuOpen((prev) => !prev)}
            className="rounded-lg border border-border bg-card p-2 text-foreground"
            aria-label={isMobileMenuOpen ? 'Close navigation menu' : 'Open navigation menu'}
            aria-expanded={isMobileMenuOpen}
          >
            {isMobileMenuOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
          </button>
        </div>
      </div>

      <div className={[
        'fixed inset-y-0 left-0 z-40 w-72 border-r border-border bg-sidebar transition-transform duration-300 lg:hidden',
        isMobileMenuOpen ? 'translate-x-0' : '-translate-x-full',
      ].join(' ')}>
        <div className="mt-header-mobile flex h-full flex-col">
          <div className="border-b border-border px-5 py-4">
            <button className="flex w-full items-center gap-3 rounded-xl border border-border bg-background px-3 py-2 text-left text-sm text-muted-foreground">
              <Search className="h-4 w-4" />
              Search...
            </button>
          </div>
          <nav className="flex-1 overflow-y-auto px-3 py-4">
            {Object.entries(groupedNav).map(([group, items]) => (
              <div key={group} className="mb-5">
                <div className="space-y-1">{items.map((item) => renderNavLink(item, true))}</div>
              </div>
            ))}
          </nav>
          <div className="border-t border-border p-4">
            <div className="rounded-2xl border border-border bg-background p-3">
              <div className="flex items-center gap-2.5 text-sm text-foreground">
                <span className={['h-2.5 w-2.5 rounded-full', isConnected ? 'bg-primary' : 'bg-destructive'].join(' ')} />
                {isConnected ? 'Gateway online' : 'Gateway offline'}
              </div>
              <div className="mt-1 text-xs text-muted-foreground">{gatewayDetail}</div>
            </div>
            {isRelay && isRelayAuthenticated && (
              <button
                onClick={handleSignOut}
                className="mt-2 flex w-full items-center gap-2 rounded-xl px-2 py-2 text-sm text-muted-foreground"
              >
                <LogOut className="h-4 w-4" />
                Sign out
              </button>
            )}
          </div>
        </div>
      </div>

      {isMobileMenuOpen && <div className="fixed inset-0 z-30 bg-black/40 lg:hidden" onClick={() => setIsMobileMenuOpen(false)} />}

      <aside className={[
        'hidden border-r border-border bg-sidebar lg:flex lg:flex-col',
        compactSidebar ? 'w-[68px]' : 'w-[232px]',
      ].join(' ')}>
        <div className={['relative border-b border-border', compactSidebar ? 'px-3' : 'px-5'].join(' ')} style={{ height: DESKTOP_CHROME_HEADER_HEIGHT }}>
          <div className={['flex h-full items-center', compactSidebar ? 'justify-center' : 'gap-3'].join(' ')}>
            <Link
              to="/overview"
              className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg border border-teal-border bg-background"
              aria-label="Go to Today"
              style={{ textDecoration: 'none' }}
            >
              <AgencyMark />
            </Link>
            {!compactSidebar && (
              <Link to="/overview" className="min-w-0 flex-1" style={{ textDecoration: 'none' }} aria-label="Go to Today">
                <div className="text-lg leading-none text-sidebar-foreground" style={{ fontFamily: 'var(--font-display)', fontWeight: 400 }}>Agency</div>
                <div className="mt-1 font-mono text-[9px] tracking-[0.1em] text-sidebar-foreground/42">v0.24.1 · local</div>
              </Link>
            )}
          </div>
        </div>

        <div className={['flex h-[58px] items-center border-b border-border', compactSidebar ? 'px-2' : 'px-4'].join(' ')}>
          <button className={[
            'flex w-full items-center text-sm text-muted-foreground',
            compactSidebar ? 'justify-center border-0 bg-transparent px-0 py-2' : 'h-9 gap-2 rounded-xl border border-border bg-background px-4',
          ].join(' ')}>
            <Search className="h-4 w-4" />
            {!compactSidebar && <><span className="flex-1 text-left">Search...</span><span className="text-[10px] uppercase tracking-[0.12em]">⌘K</span></>}
          </button>
        </div>

        <nav className={['flex-1 overflow-y-auto', compactSidebar ? 'px-2 py-4' : 'px-3 py-4'].join(' ')}>
          {Object.entries(groupedNav).map(([group, items], index) => (
            <div key={group} className={index === 0 ? '' : compactSidebar ? 'mt-5' : 'mt-2'}>
              <div className={compactSidebar ? 'space-y-3' : 'space-y-1'}>{items.map((item) => renderNavLink(item))}</div>
            </div>
          ))}

          {!compactSidebar && experimentalSurfacesEnabled && (
            <div className="mt-5 rounded-2xl border border-border bg-background px-3 py-3 text-xs text-muted-foreground">
              Experimental surfaces are enabled for this workspace.
            </div>
          )}
        </nav>

        <div className={[
          'flex shrink-0 border-t border-border',
          compactSidebar ? 'items-center justify-center px-2 py-3' : 'h-9 items-center px-4',
        ].join(' ')}>
          <div className={['flex items-center', compactSidebar ? 'flex-col gap-3' : 'w-full justify-evenly'].join(' ')}>
            <div className="flex items-center justify-center text-muted-foreground">
              <TextScaleControl />
            </div>
            <button
              type="button"
              onClick={cycleTheme}
              className="flex h-5 w-5 items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
              aria-label={`Theme: ${theme}`}
              title={`Theme: ${theme}`}
            >
              <ThemeIcon className="h-4 w-4" />
            </button>
            <button
              type="button"
              onClick={toggleCompactSidebar}
              className="flex h-5 w-5 items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
              aria-label={compactSidebar ? 'Expand sidebar' : 'Compact sidebar'}
              title={compactSidebar ? 'Expand sidebar' : 'Compact sidebar'}
            >
              {compactSidebar ? <PanelLeftOpen className="h-4 w-4" /> : <PanelLeftClose className="h-4 w-4" />}
            </button>
          </div>
        </div>
      </aside>

      <div className="flex min-h-0 min-w-0 flex-1 flex-col pt-header-mobile lg:pt-0">
        {!suppressShellHeader && <div className="hidden border-b border-border bg-background lg:block" style={{ height: DESKTOP_CHROME_HEADER_HEIGHT }}>
          <div className="flex h-full items-center justify-between gap-8 px-10 py-2">
            <div className="min-w-0">
              <div className="eyebrow" style={{ marginBottom: 4 }}>{shellHeader.eyebrow}</div>
              <h1 className="display" style={{ margin: 0, fontSize: 28, fontWeight: 300, lineHeight: 1, letterSpacing: '-0.025em', color: 'var(--ink)' }}>
                {shellHeader.title}
              </h1>
            </div>
            <div className="shrink-0 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Local workspace</div>
          </div>
        </div>}

        <main className={[
          'min-h-0 min-w-0 flex-1 bg-background',
          isFixedWorkspaceRoute ? 'overflow-hidden' : 'overflow-auto',
        ].join(' ')}>
          {startingServices.length > 0 && (
            <div role="alert" className="flex items-center gap-2 border-b border-amber-200 bg-amber-50 px-4 py-2 text-sm text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-300">
              <div className="h-1.5 w-1.5 animate-pulse rounded-full bg-amber-500 flex-shrink-0" />
              {startingServices.join(', ')} starting up&hellip;
            </div>
          )}
          {downServices.length > 0 && (
            <div className="flex items-center gap-2 border-b border-red-200 bg-red-50 px-4 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/20 dark:text-red-300">
              <div className="h-1.5 w-1.5 rounded-full bg-red-500 flex-shrink-0" />
              {downServices.join(', ')} {downServices.length === 1 ? 'is' : 'are'} down
            </div>
          )}
          <Outlet />
        </main>
        <div className="hidden h-9 shrink-0 items-center border-t border-border bg-background lg:flex">
          <div className="flex min-w-0 flex-1 items-center overflow-hidden">
            <div className="flex min-w-0 items-center gap-5 overflow-x-auto overflow-y-hidden whitespace-nowrap px-4 py-2 scrollbar-none">
              <div className="shrink-0 font-mono text-[10px] uppercase tracking-[0.18em] text-primary">Infrastructure</div>
              {infraComponents.length === 0 ? (
                <span className="font-mono text-[11px] text-muted-foreground">no services reported</span>
              ) : infraComponents.map((component) => (
                <div key={component.name} className="inline-flex shrink-0 items-center gap-2" title={`${component.name}: ${component.state}${component.health ? ` / ${component.health}` : ''}`}>
                  <span className={`h-1.5 w-1.5 rounded-full ${infraDotClass(component)}`} />
                  <span className="font-mono text-[11px] text-muted-foreground">{component.name}</span>
                </div>
              ))}
              <div className="inline-flex shrink-0 items-center gap-2" title={gatewayDetail}>
                <span className={['relative h-2 w-2 rounded-full', isConnected ? 'bg-primary' : 'bg-destructive'].join(' ')}>
                  {isConnected && <span className="absolute inset-0 animate-ping rounded-full bg-primary/40" />}
                </span>
                <span className="font-mono text-[11px] text-muted-foreground">{isConnected ? 'gateway online' : 'gateway offline'}</span>
                <span className="font-mono text-[11px] text-muted-foreground/70">{gatewayDetail}</span>
              </div>
            </div>
          </div>
          <div className="shrink-0 border-l border-border bg-background px-4 py-2 font-mono text-[10px] text-muted-foreground">{infraBuildId || 'local'} build</div>
        </div>
      </div>
    </div>
  );
}
