import { useState, useEffect } from 'react';
import { CheckCircle2, RefreshCw } from 'lucide-react';
import { api } from '../../lib/api';
import { Button } from '../../components/ui/button';

interface PlatformReadyStepProps {
  onComplete: () => void;
}

export function PlatformReadyStep({ onComplete }: PlatformReadyStepProps) {
  const [status, setStatus] = useState<'checking' | 'error' | 'done'>('checking');
  const [error, setError] = useState('');
  const [phase, setPhase] = useState<'gateway' | 'platform'>('gateway');
  const [showSlowHint, setShowSlowHint] = useState(false);
  const [allowContinue, setAllowContinue] = useState(false);

  const withTimeout = async <T,>(promise: Promise<T>, ms: number, label: string): Promise<T> => {
    let timer: number | undefined;
    try {
      return await Promise.race([
        promise,
        new Promise<T>((_, reject) => {
          timer = window.setTimeout(() => reject(new Error(`${label} timed out`)), ms);
        }),
      ]);
    } finally {
      if (timer) window.clearTimeout(timer);
    }
  };

  const runChecks = async () => {
    setStatus('checking');
    setError('');
    setShowSlowHint(false);
    setAllowContinue(false);
    try {
      setPhase('gateway');
      await withTimeout(api.infra.status(), 6000, 'Gateway check');
      setPhase('platform');
      await withTimeout(api.routing.config(), 4000, 'Platform check').catch(() => ({ configured: false }));
      setStatus('done');
      window.setTimeout(onComplete, 600);
    } catch (e: any) {
      setStatus('error');
      setAllowContinue(true);
      setError(e.message || 'Failed to verify local platform state');
    }
  };

  useEffect(() => {
    runChecks();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (status !== 'checking') return;
    const hintTimer = window.setTimeout(() => setShowSlowHint(true), 3000);
    const continueTimer = window.setTimeout(() => setAllowContinue(true), 8000);
    return () => {
      window.clearTimeout(hintTimer);
      window.clearTimeout(continueTimer);
    };
  }, [status]);

  return (
    <div className="space-y-6 text-center">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold text-foreground">
          {status === 'error' ? 'Platform preparation failed' : 'Preparing your platform...'}
        </h2>
        <p className="text-sm text-muted-foreground">
          {status === 'checking' && phase === 'gateway' && 'Checking the local gateway and API surface...'}
          {status === 'checking' && phase === 'platform' && 'Confirming the core runtime is ready for setup...'}
          {status === 'done' && 'Core platform checks look good'}
          {status === 'error' && 'Could not finish the local preparation step'}
        </p>
      </div>

      {status === 'checking' && (
        <div className="space-y-4">
          <RefreshCw className="mx-auto h-6 w-6 animate-spin text-muted-foreground" />
          {showSlowHint && (
            <div className="space-y-3">
              <p className="mx-auto max-w-sm text-xs text-muted-foreground">
                This can take a little while on a fresh machine while Agency verifies the local runtime and checks the current core configuration.
              </p>
              {allowContinue && (
                <div className="flex justify-center">
                  <Button variant="ghost" size="sm" onClick={onComplete} className="text-muted-foreground">
                    Continue without waiting
                  </Button>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {status === 'done' && (
        <div className="space-y-3">
          <CheckCircle2 className="mx-auto h-6 w-6 text-emerald-500" />
          <p className="text-xs text-muted-foreground">
            Agency can reach the local runtime and continue into the supported core setup path.
          </p>
        </div>
      )}

      {status === 'error' && (
        <div className="space-y-4">
          <p className="rounded border border-red-900/50 bg-red-950/30 px-4 py-2 text-sm text-red-400">
            {error}
          </p>
          <div className="flex justify-center gap-3">
            <Button variant="outline" size="sm" onClick={runChecks}>
              <RefreshCw className="mr-1.5 h-3 w-3" />
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
