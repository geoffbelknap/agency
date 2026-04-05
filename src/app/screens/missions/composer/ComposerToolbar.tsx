import { ArrowLeft, Save, CheckCircle, Rocket } from 'lucide-react';
import { useNavigate } from 'react-router';
import { Button } from '@/app/components/ui/button';
import type { ValidationError } from './canvasTypes';

interface ComposerToolbarProps {
  missionName: string;
  onSave: () => void;
  onValidate: () => ValidationError[];
  onDeploy: () => void;
  saving: boolean;
  dirty: boolean;
}

export function ComposerToolbar({ missionName, onSave, onValidate, onDeploy, saving, dirty }: ComposerToolbarProps) {
  const navigate = useNavigate();

  const handleValidate = () => {
    const errors = onValidate();
    if (errors.length === 0) {
      // Toast success handled by caller
    }
  };

  return (
    <div className="flex items-center justify-between px-4 py-2 border-b border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-950">
      <div className="flex items-center gap-3">
        <button onClick={() => navigate(missionName ? `/missions/${missionName}` : '/missions')} className="text-zinc-400 hover:text-zinc-600">
          <ArrowLeft size={18} />
        </button>
        <span className="text-sm font-medium dark:text-zinc-100">
          {missionName ? `Mission: ${missionName}` : 'New Mission'}
        </span>
        {dirty && <span className="text-[10px] text-zinc-400">Unsaved changes</span>}
      </div>
      <div className="flex items-center gap-2">
        <Button variant="outline" size="sm" onClick={onSave} disabled={saving}>
          <Save size={14} className="mr-1" />
          {saving ? 'Saving...' : 'Save'}
        </Button>
        <Button variant="outline" size="sm" onClick={handleValidate}>
          <CheckCircle size={14} className="mr-1" />
          Validate
        </Button>
        <Button size="sm" onClick={onDeploy}>
          <Rocket size={14} className="mr-1" />
          Deploy
        </Button>
      </div>
    </div>
  );
}
