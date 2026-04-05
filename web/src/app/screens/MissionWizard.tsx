import { useState, useCallback, useEffect } from 'react';
import { useNavigate } from 'react-router';
import { toast } from 'sonner';
import { X } from 'lucide-react';
import { Button } from '../components/ui/button';
import { api, type RawMission } from '../lib/api';
import { type WizardState, emptyWizardState, serializeToYaml, parseFromRaw } from './missions/serialize';
import { StepBasics, StepInstructions, StepTriggers, StepRequirements, StepReview } from './missions/WizardSteps';
import { StepCostQuality } from './missions/StepCostQuality';

interface MissionWizardProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editMission?: RawMission;
  onComplete: () => void;
}

const STEPS = ['Basics', 'Instructions', 'Triggers', 'Requirements', 'Cost & Quality', 'Review'];

export function MissionWizard({ open, onOpenChange, editMission, onComplete }: MissionWizardProps) {
  const isEdit = !!editMission;
  const navigate = useNavigate();
  const [step, setStep] = useState(0);
  const [state, setState] = useState<WizardState>(() =>
    editMission ? parseFromRaw(editMission) : emptyWizardState(),
  );
  const [submitting, setSubmitting] = useState(false);

  // Reset state when dialog opens/closes or editMission changes
  useEffect(() => {
    if (open) {
      setState(editMission ? parseFromRaw(editMission) : emptyWizardState());
      setStep(0);
      setSubmitting(false);
    }
  }, [open, editMission]);

  const handleChange = useCallback((updates: Partial<WizardState>) => {
    setState((prev) => ({ ...prev, ...updates }));
  }, []);

  const handleSubmit = async () => {
    setSubmitting(true);
    try {
      const yaml = serializeToYaml(state);
      if (isEdit) {
        await api.missions.update(state.name, yaml);
      } else {
        await api.missions.create(yaml);
      }
      if (state.assignTarget.trim()) {
        await api.missions.assign(state.name, state.assignTarget.trim(), state.assignType);
      }
      toast.success(isEdit ? `Mission "${state.name}" updated` : `Mission "${state.name}" created`);
      onComplete();
      onOpenChange(false);
    } catch (e: any) {
      toast.error(e.message || 'Failed to save mission');
    } finally {
      setSubmitting(false);
    }
  };

  if (!open) return null;

  const yamlPreview = serializeToYaml(state);

  const canAdvance = (() => {
    switch (step) {
      case 0:
        return state.name.trim() !== '' && state.description.trim() !== '';
      case 1:
        return state.instructions.trim() !== '';
      case 2:
      case 3:
      case 4:
        return true;
      case 5:
        return state.name.trim() !== '' && state.description.trim() !== '' && state.instructions.trim() !== '';
      default:
        return true;
    }
  })();

  const isLastStep = step === STEPS.length - 1;

  const stepContent = [
    <StepBasics key="basics" state={state} onChange={handleChange} />,
    <StepInstructions key="instructions" state={state} onChange={handleChange} />,
    <StepTriggers key="triggers" state={state} onChange={handleChange} />,
    <StepRequirements key="requirements" state={state} onChange={handleChange} />,
    <StepCostQuality key="cost-quality" state={state} onChange={(s) => setState(s)} />,
    <StepReview
      key="review"
      state={state}
      onChange={handleChange}
      onSubmit={handleSubmit}
      onGoToStep={setStep}
      isEdit={isEdit}
      yamlPreview={yamlPreview}
    />,
  ];

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black/60 z-50"
        onClick={() => onOpenChange(false)}
      />

      {/* Dialog panel */}
      <div className="fixed inset-0 z-50 flex items-center justify-center p-4 pointer-events-none">
        <div
          className="bg-card border border-border rounded-lg shadow-xl max-w-2xl w-full max-h-[90vh] flex flex-col overflow-hidden pointer-events-auto"
          onClick={(e) => e.stopPropagation()}
        >
          {/* Header */}
          <div className="flex items-center justify-between px-6 pt-5 pb-2">
            <div className="flex items-center gap-3">
              <h2 className="text-lg font-semibold">{isEdit ? 'Edit Mission' : 'New Mission'}</h2>
              {!isEdit && (
                <Button variant="link" size="sm" className="text-muted-foreground h-auto p-0 text-xs" onClick={() => { onOpenChange(false); navigate('/missions/new/composer'); }}>
                  or use Visual Editor
                </Button>
              )}
            </div>
            <button
              type="button"
              onClick={() => onOpenChange(false)}
              className="text-muted-foreground hover:text-foreground transition-colors"
            >
              <X className="h-5 w-5" />
            </button>
          </div>

          {/* Stepper progress bar */}
          <div className="flex gap-1 px-6 pt-4">
            {STEPS.map((label, i) => (
              <button
                key={i}
                type="button"
                onClick={() => setStep(i)}
                className="flex-1 flex flex-col items-center gap-1"
              >
                <div
                  className={`h-1 w-full rounded-full ${i <= step ? 'bg-primary' : 'bg-border'}`}
                />
                <span
                  className={`text-[10px] ${i === step ? 'text-primary font-medium' : 'text-muted-foreground'}`}
                >
                  {label}
                </span>
              </button>
            ))}
          </div>

          {/* Step content */}
          <div className="flex-1 overflow-auto p-6">{stepContent[step]}</div>

          {/* Footer */}
          <div className="border-t border-border flex items-center justify-between p-4">
            <div>
              {step > 0 && (
                <Button variant="outline" onClick={() => setStep(step - 1)}>
                  Back
                </Button>
              )}
            </div>
            <Button
              disabled={!canAdvance || (isLastStep && submitting)}
              onClick={() => {
                if (isLastStep) {
                  handleSubmit();
                } else {
                  setStep(step + 1);
                }
              }}
            >
              {isLastStep
                ? submitting
                  ? 'Saving...'
                  : isEdit
                    ? 'Save Changes'
                    : 'Create Mission'
                : 'Next'}
            </Button>
          </div>
        </div>
      </div>
    </>
  );
}
