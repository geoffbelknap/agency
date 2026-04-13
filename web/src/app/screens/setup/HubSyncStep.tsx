import { useState, useEffect } from 'react';
import { RefreshCw } from 'lucide-react';
import { api } from '../../lib/api';
import { Button } from '../../components/ui/button';

interface HubSyncStepProps {
  onComplete: () => void;
}

export function HubSyncStep({ onComplete }: HubSyncStepProps) {
  const [status, setStatus] = useState<'syncing' | 'error' | 'done'>('syncing');
  const [error, setError] = useState('');
  const [phase, setPhase] = useState<'update' | 'upgrade'>('update');
  const [showSlowHint, setShowSlowHint] = useState(false);
  const [allowContinue, setAllowContinue] = useState(false);

  const runSync = async () => {
    setStatus('syncing');
    setError('');
    setShowSlowHint(false);
    setAllowContinue(false);
    try {
      setPhase('update');
      await api.hub.update();
      setPhase('upgrade');
      await api.hub.upgrade();
      setStatus('done');
      setTimeout(onComplete, 600);
    } catch (e: any) {
      setStatus('error');
      setError(e.message || 'Failed to sync hub');
    }
  };

  useEffect(() => {
    runSync();
  }, []);

  useEffect(() => {
    if (status !== 'syncing') return;
    const hintTimer = window.setTimeout(() => setShowSlowHint(true), 3000);
    const continueTimer = window.setTimeout(() => setAllowContinue(true), 8000);
    return () => {
      window.clearTimeout(hintTimer);
      window.clearTimeout(continueTimer);
    };
  }, [status]);

  return (
    <div className="text-center space-y-6">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold text-foreground">
          {status === 'error' ? 'Hub sync failed' : 'Preparing your platform...'}
        </h2>
        <p className="text-muted-foreground text-sm">
          {status === 'syncing' && phase === 'update' && 'Updating hub sources...'}
          {status === 'syncing' && phase === 'upgrade' && 'Installing components...'}
          {status === 'done' && 'Ready to go'}
          {status === 'error' && 'Could not reach the hub registry'}
        </p>
      </div>

      {status === 'syncing' && (
        <div className="space-y-4">
          <RefreshCw className="w-6 h-6 text-muted-foreground animate-spin mx-auto" />
          {showSlowHint && (
            <div className="space-y-3">
              <p className="text-xs text-muted-foreground max-w-sm mx-auto">
                This can take a little while on a fresh machine while Agency refreshes hub sources and checks installed components.
              </p>
              {allowContinue && (
                <div className="flex justify-center">
                  <Button variant="ghost" size="sm" onClick={onComplete} className="text-muted-foreground">
                    Continue without sync
                  </Button>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {status === 'error' && (
        <div className="space-y-4">
          <p className="text-sm text-red-400 bg-red-950/30 border border-red-900/50 rounded px-4 py-2">
            {error}
          </p>
          <div className="flex gap-3 justify-center">
            <Button variant="outline" size="sm" onClick={runSync}>
              <RefreshCw className="w-3 h-3 mr-1.5" />
              Retry
            </Button>
            <Button variant="ghost" size="sm" onClick={onComplete} className="text-muted-foreground">
              Continue anyway
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
