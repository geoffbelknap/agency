import { Link, Outlet, useLocation, useNavigate } from 'react-router';
import { Bot, MessageSquare, Settings, Menu, X, Sun, Moon, Monitor, PanelLeftClose, PanelLeftOpen, Brain, Target, UserCircle, LogOut, Package, Cable, Users } from 'lucide-react';
import { useState, useEffect, useCallback } from 'react';
import { socket } from '../lib/ws';
import { api, ensureConfig, getVia, getAuthenticated } from '../lib/api';
import { useTheme, type Theme } from './ThemeProvider';
import { useVisualViewport } from '../hooks/useVisualViewport';
import { TextScaleControl } from './TextScaleControl';

const primaryNav = [
  { name: 'Channels', path: '/channels', icon: MessageSquare },
  { name: 'Agents', path: '/agents', icon: Bot },
  { name: 'Missions', path: '/missions', icon: Target },
  { name: 'Teams', path: '/teams', icon: Users },
  { name: 'Knowledge', path: '/knowledge', icon: Brain },
  { name: 'Profiles', path: '/profiles', icon: UserCircle },
  { name: 'Hub', path: '/admin/hub', icon: Package },
  { name: 'Intake', path: '/admin/intake', icon: Cable },
];

const secondaryNav = [
  { name: 'Admin', path: '/admin', icon: Settings },
];

const COMPACT_STORAGE_KEY = 'agency-sidebar-compact';

export function Layout() {
  const location = useLocation();
  const navigate = useNavigate();
  const isChannelsRoute = location.pathname.startsWith('/channels');
  const [isConnected, setIsConnected] = useState(false);
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false);
  const [hasChannelUnread, setHasChannelUnread] = useState(false);
  const [compactSidebar, setCompactSidebar] = useState(() => localStorage.getItem(COMPACT_STORAGE_KEY) === 'true');
  const [setupChecked, setSetupChecked] = useState(false);

  const [isRelay, setIsRelay] = useState(false);
  const [isRelayAuthenticated, setIsRelayAuthenticated] = useState(false);

  const { theme, setTheme } = useTheme();
  useVisualViewport();

  // Relay connection metadata
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

  // First-launch detection: redirect to /setup if no providers configured
  useEffect(() => {
    api.routing.config().then((cfg: any) => {
      if (cfg.configured === false) {
        navigate('/setup', { replace: true });
      } else {
        setSetupChecked(true);
      }
    }).catch(() => {
      setSetupChecked(true); // can't check — proceed normally
    });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Infrastructure health banner
  const [startingServices, setStartingServices] = useState<string[]>([]);
  const [downServices, setDownServices] = useState<string[]>([]);

  const loadInfraHealth = useCallback(async () => {
    try {
      const infra = await api.infra.status();
      const components = infra.components ?? [];
      setStartingServices(
        components.filter((c: any) => c.state === 'running' && c.health !== 'healthy').map((c: any) => c.name),
      );
      setDownServices(
        components.filter((c: any) => c.state !== 'running' && c.state !== 'missing').map((c: any) => c.name),
      );
    } catch {
      setStartingServices([]);
      setDownServices([]);
    }
  }, []);

  useEffect(() => { loadInfraHealth(); }, [loadInfraHealth]);
  useEffect(() => {
    const unsub = socket.on('infra_status', () => loadInfraHealth());
    return () => { unsub(); };
  }, [loadInfraHealth]);

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

  useEffect(() => {
    socket.connect();
    const unsub = socket.onConnectionChange(setIsConnected);
    return () => { unsub(); };
  }, []);

  // Track new messages arriving while not on the channels route
  useEffect(() => {
    const unsub = socket.on('message', () => {
      if (!location.pathname.startsWith('/channels')) {
        setHasChannelUnread(true);
      }
    });
    return () => { unsub(); };
  }, [location.pathname]);

  // Clear unread dot when navigating to channels
  useEffect(() => {
    if (location.pathname.startsWith('/channels')) {
      setHasChannelUnread(false);
    }
  }, [location.pathname]);

  // Close mobile menu when route changes
  useEffect(() => {
    setIsMobileMenuOpen(false);
  }, [location.pathname]);

  function renderNavItem(item: typeof primaryNav[number]) {
    const isActive = location.pathname === item.path || location.pathname.startsWith(item.path + '/');
    const Icon = item.icon;
    const showUnread = item.path === '/channels' && hasChannelUnread && !isActive;
    const navLabel = item.name;

    return (
      <Link
        key={item.path}
        to={item.path}
        title={compactSidebar ? navLabel : undefined}
        className={`group flex items-center gap-3 rounded-2xl px-3 py-2.5 transition-colors duration-150
          ${isActive
            ? 'bg-sidebar-accent text-sidebar-foreground'
            : 'text-sidebar-foreground/72 hover:bg-sidebar-accent/70 hover:text-sidebar-foreground'
          }`}
      >
        <div className={`flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-xl ${
          isActive ? 'bg-primary/12 text-primary' : 'text-sidebar-foreground/62 group-hover:text-sidebar-foreground'
        }`}>
          <Icon className="h-4 w-4" />
        </div>
        {!compactSidebar && (
          <div className="min-w-0 flex-1">
            <span className="block whitespace-nowrap text-sm font-medium">
              {navLabel}
            </span>
            {isActive && (
              <span className="block whitespace-nowrap text-[11px] text-sidebar-foreground/56">
                {item.path.startsWith('/admin') ? 'Administrative surface' : 'Workspace surface'}
              </span>
            )}
          </div>
        )}
        {showUnread && (
          <span className={`relative flex h-2.5 w-2.5 flex-shrink-0 ${compactSidebar ? 'ml-auto' : ''}`}>
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-primary/55" />
            <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-primary" />
          </span>
        )}
      </Link>
    );
  }

  function renderMobileNavItem(item: typeof primaryNav[number]) {
    const isActive = location.pathname === item.path || location.pathname.startsWith(item.path + '/');
    const Icon = item.icon;
    const showUnread = item.path === '/channels' && hasChannelUnread && !isActive;

    return (
      <Link
        key={item.path}
        to={item.path}
        className={`flex items-center gap-3 rounded-2xl px-3 py-2.5 text-sm font-medium transition-colors duration-150
          ${isActive
            ? 'bg-sidebar-accent text-sidebar-foreground'
            : 'text-sidebar-foreground/78 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground'
          }`}
      >
        <div className={`flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-xl ${
          isActive ? 'bg-primary/12 text-primary' : 'text-sidebar-foreground/62'
        }`}>
          <Icon className="w-4 h-4" />
        </div>
        <span className="flex-1">{item.name}</span>
        {showUnread && (
          <span className="relative flex h-2 w-2">
            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-primary opacity-50" />
            <span className="relative inline-flex rounded-full h-2 w-2 bg-primary" />
          </span>
        )}
      </Link>
    );
  }

  if (!setupChecked) {
    return <div className="min-h-screen bg-background" />;
  }

  return (
    <div className="flex h-dvh overflow-hidden bg-background text-foreground">
      {/* Mobile Header */}
      <div className="fixed left-0 right-0 top-0 z-50 flex items-center justify-between border-b border-sidebar-border bg-sidebar px-4 py-3 safe-top lg:hidden">
        <div className="flex items-center gap-2.5">
          <svg width="22" height="22" viewBox="0 0 52 52" className="flex-shrink-0">
            <rect x="0" y="0" width="22" height="22" rx="3" className="fill-primary" />
            <rect x="26" y="0" width="22" height="22" rx="3" className="fill-foreground" />
            <rect x="0" y="26" width="22" height="22" rx="3" className="fill-foreground" />
            <rect x="26" y="26" width="22" height="22" rx="3" className="fill-foreground" />
          </svg>
          <span style={{ fontFamily: 'var(--font-display)', fontWeight: 300, fontSize: '18px' }} className="text-sidebar-foreground">Agency</span>
        </div>
        <button
          onClick={() => setIsMobileMenuOpen(!isMobileMenuOpen)}
          className="rounded-lg p-2 text-sidebar-foreground transition-colors hover:bg-sidebar-accent"
          aria-label={isMobileMenuOpen ? 'Close navigation menu' : 'Open navigation menu'}
          aria-expanded={isMobileMenuOpen}
        >
          {isMobileMenuOpen ? <X className="w-5 h-5" /> : <Menu className="w-5 h-5" />}
        </button>
      </div>

      {/* Mobile Sidebar */}
      <div className={`
        fixed lg:hidden inset-y-0 left-0 z-40
        w-64 bg-sidebar border-r border-sidebar-border flex flex-col
        transition-transform duration-300
        ${isMobileMenuOpen ? 'translate-x-0' : '-translate-x-full'}
      `}>
        <nav className="mt-header-mobile flex-1 overflow-y-auto px-3 py-4">
          <div className="mb-6 px-2">
            <p className="text-[11px] font-medium uppercase tracking-[0.14em] text-sidebar-foreground/55">Workspace</p>
            <p className="mt-1 max-w-[14rem] text-sm text-sidebar-foreground/72">
              Direct work, runtime state, and operator controls.
            </p>
          </div>
          {primaryNav.map((item) => renderMobileNavItem(item))}
          <div className="mb-2 mt-6 px-2 pt-2">
            <span className="text-[11px] font-medium uppercase tracking-[0.14em] text-sidebar-foreground/55">Administration</span>
          </div>
          {secondaryNav.map((item) => renderMobileNavItem(item))}
        </nav>

        {/* Mobile footer */}
        <div className="border-t border-sidebar-border px-4 py-4">
          <button
            onClick={cycleTheme}
            className="flex w-full items-center gap-2.5 rounded-xl px-2.5 py-2 text-sidebar-foreground/72 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
          >
            <ThemeIcon className="w-4 h-4 flex-shrink-0" />
            <span className="text-sm font-medium capitalize">{theme}</span>
          </button>
          <div className="mt-3 flex items-center gap-2.5 px-2.5">
            <div className="relative">
              <div className={`w-2 h-2 rounded-full ${isConnected ? 'bg-emerald-500' : 'bg-red-500'}`} />
              {isConnected && (
                <div className="absolute inset-0 w-2 h-2 rounded-full bg-emerald-500 animate-ping opacity-30" />
              )}
            </div>
            <span className="text-sm font-medium text-sidebar-foreground/72">
              {isConnected ? 'Connected' : 'Disconnected'}
            </span>
            {isRelay && (
              <span className="rounded-full bg-primary/12 px-2 py-0.5 text-[11px] font-medium leading-none text-primary">Relay</span>
            )}
          </div>
          {isRelay && isRelayAuthenticated && (
            <button
              onClick={handleSignOut}
              className="mt-3 flex w-full items-center gap-2.5 rounded-xl px-2.5 py-2 text-sidebar-foreground/72 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
            >
              <LogOut className="w-3.5 h-3.5 flex-shrink-0" />
              <span className="text-sm font-medium">Sign out</span>
            </button>
          )}
        </div>
      </div>

      {/* Mobile Overlay */}
      {isMobileMenuOpen && (
        <div
          className="lg:hidden fixed inset-0 bg-black/60 backdrop-blur-sm z-30"
          onClick={() => setIsMobileMenuOpen(false)}
        />
      )}

      {/* Desktop Sidebar */}
      <aside
        className={`hidden flex-shrink-0 flex-col border-r border-sidebar-border bg-sidebar lg:flex ${
          compactSidebar ? 'w-20' : 'w-60'
        }`}
      >
        <div className={`border-b border-sidebar-border ${compactSidebar ? 'px-3 py-4' : 'px-4 py-4.5'}`}>
          <div className={`flex items-center ${compactSidebar ? 'justify-center' : 'gap-3'}`}>
            <div className="flex h-11 w-11 flex-shrink-0 items-center justify-center rounded-2xl bg-primary/10 ring-1 ring-primary/10">
              <svg width="22" height="22" viewBox="0 0 52 52" className="flex-shrink-0">
                <rect x="0" y="0" width="22" height="22" rx="3" className="fill-primary" />
                <rect x="26" y="0" width="22" height="22" rx="3" className="fill-foreground" />
                <rect x="0" y="26" width="22" height="22" rx="3" className="fill-foreground" />
                <rect x="26" y="26" width="22" height="22" rx="3" className="fill-foreground" />
              </svg>
            </div>
            {!compactSidebar && (
              <div className="min-w-0 flex-1">
                <span style={{ fontFamily: 'var(--font-display)', fontWeight: 300, fontSize: '20px' }} className="block text-sidebar-foreground">
                  Agency
                </span>
                <p className="text-sm text-sidebar-foreground/60">Operator workspace</p>
              </div>
            )}
            <button
              onClick={toggleCompactSidebar}
              className={`rounded-lg p-2 text-sidebar-foreground/65 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground ${compactSidebar ? 'absolute sr-only' : ''}`}
              title={compactSidebar ? 'Expand sidebar' : 'Compact sidebar'}
              aria-label={compactSidebar ? 'Expand sidebar' : 'Compact sidebar'}
            >
              {compactSidebar ? <PanelLeftOpen className="h-4 w-4" /> : <PanelLeftClose className="h-4 w-4" />}
            </button>
          </div>
          {compactSidebar && (
            <div className="mt-3 flex justify-center">
              <button
                onClick={toggleCompactSidebar}
                className="rounded-lg p-2 text-sidebar-foreground/65 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
                title="Expand sidebar"
                aria-label="Expand sidebar"
              >
                <PanelLeftOpen className="h-4 w-4" />
              </button>
            </div>
          )}
        </div>

        <div className={`flex-1 overflow-y-auto ${compactSidebar ? 'px-2 py-4' : 'px-3 py-4'}`}>
          {!compactSidebar && (
            <div className="mb-2 px-3">
              <span className="text-[11px] font-medium uppercase tracking-[0.14em] text-sidebar-foreground/42">Workspace</span>
            </div>
          )}
          <nav className="space-y-1">
            {primaryNav.map((item) => renderNavItem(item))}
          </nav>

          {!compactSidebar && (
            <div className="mb-2 mt-7 px-3">
              <span className="text-[11px] font-medium uppercase tracking-[0.14em] text-sidebar-foreground/42">Control</span>
            </div>
          )}
          <nav className={`space-y-1 ${compactSidebar ? 'mt-6' : ''}`}>
            {secondaryNav.map((item) => renderNavItem(item))}
          </nav>
        </div>

        <div className={`border-t border-sidebar-border ${compactSidebar ? 'px-2 py-3' : 'px-3 py-3.5'}`}>
          <div className="space-y-1">
            <button
              onClick={cycleTheme}
              className={`flex w-full items-center gap-3 rounded-2xl px-3 py-2.5 text-sidebar-foreground/68 transition-colors hover:bg-sidebar-accent hover:text-sidebar-foreground ${compactSidebar ? 'justify-center' : ''}`}
              title={`Theme: ${theme}`}
              aria-label={`Switch theme (currently ${theme})`}
            >
              <ThemeIcon className="h-4 w-4 flex-shrink-0" />
              {!compactSidebar && <span className="text-sm font-medium capitalize">{theme}</span>}
            </button>
            <div className={`flex items-center gap-3 rounded-2xl px-3 py-2.5 text-sidebar-foreground/68 ${compactSidebar ? 'justify-center' : ''}`}>
              <div className="flex h-4 w-4 items-center justify-center">
                <TextScaleControl />
              </div>
              {!compactSidebar && <span className="text-sm font-medium">Text size</span>}
            </div>
          </div>

          <div className={`mt-4 rounded-[1.35rem] border border-sidebar-border/80 bg-sidebar-accent/38 ${compactSidebar ? 'p-3' : 'px-3 py-3.5'}`}>
            <div className={`flex items-center gap-2.5 ${compactSidebar ? 'justify-center' : ''}`}>
              <div className="relative flex-shrink-0">
                <div className={`h-2.5 w-2.5 rounded-full ${isConnected ? 'bg-emerald-500' : 'bg-red-500'}`} />
                {isConnected && (
                  <div className="absolute inset-0 h-2.5 w-2.5 rounded-full bg-emerald-500 animate-ping opacity-25" />
                )}
              </div>
              {!compactSidebar && (
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-sidebar-foreground">
                      {isConnected ? 'Gateway online' : 'Gateway offline'}
                    </span>
                    {isRelay && (
                      <span className="rounded-full bg-primary/12 px-2 py-0.5 text-[11px] font-medium text-primary">
                        Relay
                      </span>
                    )}
                  </div>
                  <div className="text-xs text-sidebar-foreground/56">
                    {isRelay ? 'Signed in through relay transport' : 'Live WebSocket session'}
                  </div>
                </div>
              )}
            </div>
            {!compactSidebar && isRelay && isRelayAuthenticated && (
              <button
                onClick={handleSignOut}
                className="mt-3 flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-sm font-medium text-sidebar-foreground/72 transition-colors hover:bg-sidebar hover:text-sidebar-foreground"
                aria-label="Sign out of relay session"
              >
                <LogOut className="h-3.5 w-3.5 flex-shrink-0" />
                <span>Sign out</span>
              </button>
            )}
          </div>
        </div>
      </aside>

      {/* Main Content */}
      <main className={`min-w-0 flex-1 bg-background ${isChannelsRoute ? 'overflow-hidden' : 'overflow-auto'} mt-header-mobile lg:mt-0`}>
        {startingServices.length > 0 && (
          <div role="alert" className="flex items-center gap-2 border-b border-amber-200 bg-amber-50 px-4 py-2 text-sm text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-300">
            <div className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse flex-shrink-0" />
            {startingServices.join(', ')} starting up&hellip;
          </div>
        )}
        {downServices.length > 0 && (
          <div className="flex items-center gap-2 border-b border-red-200 bg-red-50 px-4 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/20 dark:text-red-300">
            <div className="w-1.5 h-1.5 rounded-full bg-red-500 flex-shrink-0" />
            {downServices.join(', ')} {downServices.length === 1 ? 'is' : 'are'} down
          </div>
        )}
        <Outlet />
      </main>
    </div>
  );
}
