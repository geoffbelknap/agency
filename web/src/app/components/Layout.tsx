import { Link, Outlet, useLocation, useNavigate } from 'react-router';
import { Bot, MessageSquare, Settings, Menu, X, Sun, Moon, Monitor, Pin, PinOff, Brain, Target, UserCircle } from 'lucide-react';
import { useState, useEffect, useCallback } from 'react';
import { socket } from '../lib/ws';
import { api, ensureConfig, getVia } from '../lib/api';
import { useTheme, type Theme } from './ThemeProvider';
import { useVisualViewport } from '../hooks/useVisualViewport';
import { TextScaleControl } from './TextScaleControl';

const primaryNav = [
  { name: 'Channels', path: '/channels', icon: MessageSquare },
  { name: 'Agents', path: '/agents', icon: Bot },
  { name: 'Missions', path: '/missions', icon: Target },
  { name: 'Knowledge', path: '/knowledge', icon: Brain },
  { name: 'Profiles', path: '/profiles', icon: UserCircle },
];

const secondaryNav = [
  { name: 'Admin', path: '/admin', icon: Settings },
];

export function Layout() {
  const location = useLocation();
  const navigate = useNavigate();
  const isChannelsRoute = location.pathname.startsWith('/channels');
  const [isConnected, setIsConnected] = useState(false);
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false);
  const [hasChannelUnread, setHasChannelUnread] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const [pinned, setPinned] = useState(() => localStorage.getItem('agency-sidebar-pinned') === 'true');
  const [setupChecked, setSetupChecked] = useState(false);

  const [isRelay, setIsRelay] = useState(false);

  const { theme, setTheme } = useTheme();
  useVisualViewport();

  // Relay connection metadata
  useEffect(() => {
    ensureConfig().then(() => {
      setIsRelay(getVia() === 'relay');
    });
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

  const togglePin = () => {
    const next = !pinned;
    setPinned(next);
    setExpanded(next);
    localStorage.setItem('agency-sidebar-pinned', String(next));
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

  // Keep expanded in sync with pinned on mount
  useEffect(() => {
    if (pinned) setExpanded(true);
  }, [pinned]);

  function renderNavItem(item: typeof primaryNav[number]) {
    const isActive = location.pathname === item.path || location.pathname.startsWith(item.path + '/');
    const Icon = item.icon;
    const showUnread = item.path === '/channels' && hasChannelUnread && !isActive;

    return (
      <Link
        key={item.path}
        to={item.path}
        className={`flex items-center gap-3 px-3 py-2 rounded-md transition-colors duration-150
          ${isActive
            ? 'bg-primary/10 text-primary'
            : 'text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground'
          }`}
      >
        <Icon className="w-4 h-4 flex-shrink-0" />
        <span className={`text-[13px] font-medium whitespace-nowrap transition-opacity duration-150 ${expanded ? 'opacity-100' : 'opacity-0'}`}>{item.name}</span>
        {showUnread && (
          <span className="relative flex h-2 w-2 flex-shrink-0">
            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-primary opacity-50" />
            <span className="relative inline-flex rounded-full h-2 w-2 bg-primary" />
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
        className={`flex items-center gap-3 px-3 py-2 rounded-md text-[13px] font-medium transition-all duration-150
          ${isActive
            ? 'bg-primary/10 text-primary'
            : 'text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground'
          }`}
      >
        <Icon className="w-4 h-4" />
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
      <div className="lg:hidden fixed top-0 left-0 right-0 bg-sidebar border-b border-sidebar-border z-50 px-4 py-3 safe-top flex items-center justify-between">
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
          className="p-2 hover:bg-sidebar-accent rounded transition-colors text-sidebar-foreground"
          aria-label={isMobileMenuOpen ? 'Close navigation menu' : 'Open navigation menu'}
          aria-expanded={isMobileMenuOpen}
        >
          {isMobileMenuOpen ? <X className="w-5 h-5" /> : <Menu className="w-5 h-5" />}
        </button>
      </div>

      {/* Mobile Sidebar */}
      <div className={`
        fixed lg:hidden inset-y-0 left-0 z-40
        w-52 bg-sidebar border-r border-sidebar-border flex flex-col
        transition-transform duration-300
        ${isMobileMenuOpen ? 'translate-x-0' : '-translate-x-full'}
      `}>
        <nav className="flex-1 px-3 py-4 space-y-0.5 overflow-y-auto mt-header-mobile">
          <div className="px-2 pb-2 mb-1">
            <span className="text-[9px] font-mono font-medium tracking-[0.15em] text-muted-foreground uppercase">Navigation</span>
          </div>
          {primaryNav.map((item) => renderMobileNavItem(item))}
          <div className="pt-4 px-2 pb-2 mb-1">
            <span className="text-[9px] font-mono font-medium tracking-[0.15em] text-muted-foreground uppercase">System</span>
          </div>
          {secondaryNav.map((item) => renderMobileNavItem(item))}
        </nav>

        {/* Mobile footer */}
        <div className="px-4 py-3 border-t border-sidebar-border">
          <button
            onClick={cycleTheme}
            className="flex items-center gap-2.5 w-full px-2 py-1.5 rounded-md text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground transition-colors"
          >
            <ThemeIcon className="w-4 h-4 flex-shrink-0" />
            <span className="text-[11px] font-medium capitalize">{theme}</span>
          </button>
          <div className="flex items-center gap-2.5 mt-2 px-2">
            <div className="relative">
              <div className={`w-2 h-2 rounded-full ${isConnected ? 'bg-emerald-500' : 'bg-red-500'}`} />
              {isConnected && (
                <div className="absolute inset-0 w-2 h-2 rounded-full bg-emerald-500 animate-ping opacity-30" />
              )}
            </div>
            <span className="text-[11px] font-medium text-muted-foreground">
              {isConnected ? 'Connected' : 'Disconnected'}
            </span>
            {isRelay && (
              <span className="text-[9px] font-mono px-1 py-0.5 rounded bg-primary/10 text-primary leading-none">relay</span>
            )}
          </div>
        </div>
      </div>

      {/* Mobile Overlay */}
      {isMobileMenuOpen && (
        <div
          className="lg:hidden fixed inset-0 bg-black/60 backdrop-blur-sm z-30"
          onClick={() => setIsMobileMenuOpen(false)}
        />
      )}

      {/* Desktop Sidebar — icon rail that expands */}
      <aside
        className={`hidden lg:flex flex-col border-r border-sidebar-border bg-sidebar
          transition-[width] duration-200 ease-in-out overflow-hidden flex-shrink-0
          ${expanded ? 'w-[200px]' : 'w-14'}`}
        onMouseEnter={() => { if (!pinned) setExpanded(true); }}
        onMouseLeave={() => { if (!pinned) setExpanded(false); }}
      >
        {/* Logo area — fixed height, icon always centered at same position */}
        <div className="flex items-center h-14 px-[11px] border-b border-sidebar-border">
          <svg width="22" height="22" viewBox="0 0 52 52" className="flex-shrink-0">
            <rect x="0" y="0" width="22" height="22" rx="3" className="fill-primary" />
            <rect x="26" y="0" width="22" height="22" rx="3" className="fill-foreground" />
            <rect x="0" y="26" width="22" height="22" rx="3" className="fill-foreground" />
            <rect x="26" y="26" width="22" height="22" rx="3" className="fill-foreground" />
          </svg>
          <div className={`ml-3 min-w-0 flex-1 transition-opacity duration-150 ${expanded ? 'opacity-100' : 'opacity-0'}`}>
            <span style={{ fontFamily: 'var(--font-display)', fontWeight: 300, fontSize: '18px' }} className="text-sidebar-foreground whitespace-nowrap">Agency</span>
          </div>
          <button
            onClick={togglePin}
            className={`p-1 rounded text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground transition-opacity duration-150 flex-shrink-0 ${expanded ? 'opacity-100' : 'opacity-0 pointer-events-none'}`}
            title={pinned ? 'Unpin sidebar' : 'Pin sidebar'}
            aria-label={pinned ? 'Unpin sidebar' : 'Pin sidebar'}
            tabIndex={expanded ? 0 : -1}
          >
            {pinned ? <PinOff className="w-3 h-3" /> : <Pin className="w-3 h-3" />}
          </button>
        </div>

        {/* Primary nav — consistent padding so icons don't shift */}
        <nav className="px-[11px] py-3 space-y-1">
          {primaryNav.map((item) => renderNavItem(item))}
        </nav>

        {/* Flex spacer */}
        <div className="flex-1" />

        {/* Secondary nav (Admin) */}
        <nav className="px-[11px] pb-1 space-y-1">
          {secondaryNav.map((item) => renderNavItem(item))}
        </nav>

        {/* Theme & text scale */}
        <div className="px-[11px] pb-1 space-y-0.5">
          <button
            onClick={cycleTheme}
            className="flex items-center gap-3 px-3 py-2 rounded-md transition-all duration-150 w-full
              text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
            title={`Theme: ${theme}`}
            aria-label={`Switch theme (currently ${theme})`}
          >
            <ThemeIcon className="w-4 h-4 flex-shrink-0" />
            <span className={`text-[13px] font-medium capitalize whitespace-nowrap transition-opacity duration-150 ${expanded ? 'opacity-100' : 'opacity-0'}`}>{theme}</span>
          </button>
          <div className="flex items-center gap-3 px-3 py-2 rounded-md text-muted-foreground">
            <div className="w-4 h-4 flex items-center justify-center flex-shrink-0">
              <TextScaleControl />
            </div>
            <span className={`text-[13px] font-medium whitespace-nowrap transition-opacity duration-150 ${expanded ? 'opacity-100' : 'opacity-0'}`}>Text size</span>
          </div>
        </div>

        {/* Connection indicator */}
        <div className="border-t border-sidebar-border px-[11px] py-3 space-y-2">
          <div className="flex items-center gap-2.5 px-3">
            <div className="relative flex-shrink-0">
              <div className={`w-2 h-2 rounded-full ${isConnected ? 'bg-emerald-500' : 'bg-red-500'}`} />
              {isConnected && (
                <div className="absolute inset-0 w-2 h-2 rounded-full bg-emerald-500 animate-ping opacity-30" />
              )}
            </div>
            <div className={`transition-opacity duration-150 ${expanded ? 'opacity-100' : 'sr-only'}`}>
              <div className="text-[11px] font-medium text-muted-foreground whitespace-nowrap flex items-center gap-1.5">
                {isConnected ? 'Connected' : 'Disconnected'}
                {isRelay && (
                  <span className="text-[9px] font-mono px-1 py-0.5 rounded bg-primary/10 text-primary leading-none">relay</span>
                )}
              </div>
              <div className="text-[9px] font-mono text-muted-foreground whitespace-nowrap">
                {isRelay ? 'via relay' : 'Gateway WS'}
              </div>
            </div>
          </div>
        </div>
      </aside>

      {/* Main Content */}
      <main className={`flex-1 min-w-0 mt-header-mobile lg:mt-0 ${isChannelsRoute ? 'overflow-hidden' : 'overflow-auto'}`}>
        {startingServices.length > 0 && (
          <div role="alert" className="flex items-center gap-2 px-4 py-2 text-xs text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-950/20 border-b border-amber-200 dark:border-amber-900/50">
            <div className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse flex-shrink-0" />
            {startingServices.join(', ')} starting up&hellip;
          </div>
        )}
        {downServices.length > 0 && (
          <div className="flex items-center gap-2 px-4 py-2 text-xs text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-950/20 border-b border-red-200 dark:border-red-900/50">
            <div className="w-1.5 h-1.5 rounded-full bg-red-500 flex-shrink-0" />
            {downServices.join(', ')} {downServices.length === 1 ? 'is' : 'are'} down
          </div>
        )}
        <Outlet />
      </main>
    </div>
  );
}
