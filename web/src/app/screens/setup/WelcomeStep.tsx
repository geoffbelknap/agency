import { useState } from 'react';
import { api } from '../../lib/api';
import { Input } from '../../components/ui/input';
import { Button } from '../../components/ui/button';

interface WelcomeStepProps {
  operatorName: string;
  onUpdate: (name: string) => void;
  onNext: () => void;
  onSkip: () => void;
  isReSetup: boolean;
}

const NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9-]*$/;

export function WelcomeStep({ operatorName, onUpdate, onNext, onSkip, isReSetup }: WelcomeStepProps) {
  const [name, setName] = useState(operatorName);
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const isValid = name.length >= 1 && name.length <= 64 && NAME_PATTERN.test(name);

  const handleSubmit = async () => {
    if (!isValid) return;
    setSubmitting(true);
    setError('');
    try {
      await api.init({ operator: name, force: isReSetup });
      onUpdate(name);
      onNext();
    } catch (e: any) {
      setError(e.message || 'Initialization failed');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="text-center space-y-8">
      <div className="space-y-3">
        <h2 className="text-2xl font-semibold text-foreground">
          {isReSetup ? 'Re-configure Agency' : 'Welcome to Agency'}
        </h2>
        <p className="text-muted-foreground text-sm max-w-sm mx-auto">
          {isReSetup
            ? "Let's walk through your configuration. You can update anything or skip ahead."
            : "Let's get your platform set up. This will take a few minutes."}
        </p>
      </div>

      <div className="space-y-3 max-w-xs mx-auto">
        <label className="text-sm text-muted-foreground text-left block">Your name</label>
        <Input
          value={name}
          onChange={(e) => setName(e.target.value.replace(/[^a-zA-Z0-9-]/g, ''))}
          placeholder="operator"
          maxLength={64}
          className="text-center bg-card border-border"
          onKeyDown={(e) => e.key === 'Enter' && isValid && handleSubmit()}
        />
        {error && <p className="text-xs text-red-400">{error}</p>}
      </div>

      <div className="space-y-3">
        <Button
          onClick={handleSubmit}
          disabled={!isValid || submitting}
          className="w-48"
        >
          {submitting ? 'Initializing...' : 'Continue'}
        </Button>

        {isReSetup && (
          <div>
            <button onClick={onSkip} className="text-xs text-muted-foreground hover:text-foreground transition-colors">
              Skip setup
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
