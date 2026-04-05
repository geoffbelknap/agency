import { Component, type ReactNode, type ErrorInfo } from 'react';
import { useRouteError, isRouteErrorResponse, useNavigate } from 'react-router';
import { useState } from 'react';
import { Button } from './ui/button';
import { AlertTriangle, RefreshCw, Home } from 'lucide-react';

interface AppErrorBoundaryProps {
  children: ReactNode;
}

interface AppErrorBoundaryState {
  error: Error | null;
}

export class AppErrorBoundary extends Component<AppErrorBoundaryProps, AppErrorBoundaryState> {
  constructor(props: AppErrorBoundaryProps) {
    super(props);
    this.state = { error: null };
  }

  static getDerivedStateFromError(error: Error): AppErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[AppErrorBoundary] Render error caught:', error, info.componentStack);
  }

  render() {
    const { error } = this.state;
    if (error) {
      return (
        <div className="h-screen flex items-center justify-center bg-background p-8">
          <div className="max-w-md text-center space-y-4">
            <AlertTriangle className="w-10 h-10 text-amber-500 mx-auto" />
            <h1 className="text-lg font-semibold text-foreground">Application Error</h1>
            <p className="text-sm text-muted-foreground leading-relaxed">{error.message}</p>
            <div className="flex gap-3 justify-center pt-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => window.location.reload()}
                aria-label="Reload page"
              >
                <RefreshCw className="w-3.5 h-3.5 mr-1" />
                Reload
              </Button>
            </div>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

export function RouteErrorBoundary() {
  const error = useRouteError();
  const navigate = useNavigate();
  const [reloading, setReloading] = useState(false);

  let title = 'Something went wrong';
  let detail = 'An unexpected error occurred.';

  if (isRouteErrorResponse(error)) {
    title = `${error.status} — ${error.statusText || 'Error'}`;
    detail = error.data?.message || error.data || `The server returned a ${error.status} response.`;
  } else if (error instanceof Error) {
    title = 'Application Error';
    detail = error.message;
  }

  return (
    <div className="h-full flex items-center justify-center bg-background p-8">
      <div className="max-w-md text-center space-y-4">
        <AlertTriangle className="w-10 h-10 text-amber-500 mx-auto" />
        <h1 className="text-lg font-semibold text-foreground">{title}</h1>
        <p className="text-sm text-muted-foreground leading-relaxed">{detail}</p>
        <div className="flex gap-3 justify-center pt-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              setReloading(true);
              window.location.reload();
            }}
            disabled={reloading}
            aria-label={reloading ? 'Reloading page' : 'Reload page'}
          >
            <RefreshCw className={`w-3.5 h-3.5 mr-1 ${reloading ? 'animate-spin' : ''}`} />
            {reloading ? 'Reloading...' : 'Reload'}
          </Button>
          <Button variant="outline" size="sm" onClick={() => navigate('/')}>
            <Home className="w-3.5 h-3.5 mr-1" />
            Overview
          </Button>
        </div>
      </div>
    </div>
  );
}
