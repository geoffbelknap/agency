import { useReducer, useCallback } from 'react';
import { useNavigate } from 'react-router';
import { HubSyncStep } from './setup/HubSyncStep';
import { WelcomeStep } from './setup/WelcomeStep';
import { ProvidersStep } from './setup/ProvidersStep';
import { AgentStep } from './setup/AgentStep';
import { CapabilitiesStep } from './setup/CapabilitiesStep';
import { ChatStep } from './setup/ChatStep';

type WizardStep = 'hub-sync' | 'welcome' | 'providers' | 'agent' | 'capabilities' | 'chat';

const STEPS: WizardStep[] = ['hub-sync', 'welcome', 'providers', 'agent', 'capabilities', 'chat'];

interface WizardState {
  step: WizardStep;
  operatorName: string;
  providers: Record<string, { configured: boolean; validated: boolean }>;
  tierStrategy: 'strict' | 'best_effort' | 'catch_all';
  agentName: string;
  agentPreset: string;
  platformExpert: boolean;
  capabilities: string[];
  hubSynced: boolean;
}

type WizardAction =
  | { type: 'SET_STEP'; step: WizardStep }
  | { type: 'SET_OPERATOR'; name: string }
  | { type: 'SET_PROVIDER'; name: string; configured: boolean; validated: boolean }
  | { type: 'SET_TIER_STRATEGY'; strategy: WizardState['tierStrategy'] }
  | { type: 'SET_AGENT'; name: string; preset: string }
  | { type: 'SET_PLATFORM_EXPERT'; enabled: boolean }
  | { type: 'SET_CAPABILITIES'; capabilities: string[] }
  | { type: 'HUB_SYNCED' };

function wizardReducer(state: WizardState, action: WizardAction): WizardState {
  switch (action.type) {
    case 'SET_STEP': return { ...state, step: action.step };
    case 'SET_OPERATOR': return { ...state, operatorName: action.name };
    case 'SET_PROVIDER':
      return { ...state, providers: { ...state.providers, [action.name]: { configured: action.configured, validated: action.validated } } };
    case 'SET_TIER_STRATEGY': return { ...state, tierStrategy: action.strategy };
    case 'SET_AGENT': return { ...state, agentName: action.name, agentPreset: action.preset };
    case 'SET_PLATFORM_EXPERT': return { ...state, platformExpert: action.enabled };
    case 'SET_CAPABILITIES': return { ...state, capabilities: action.capabilities };
    case 'HUB_SYNCED': return { ...state, hubSynced: true };
    default: return state;
  }
}

const initialState: WizardState = {
  step: 'hub-sync',
  operatorName: '',
  providers: {},
  tierStrategy: 'best_effort',
  agentName: 'henry',
  agentPreset: 'platform-expert',
  platformExpert: true,
  capabilities: [],
  hubSynced: false,
};

export function Setup() {
  const navigate = useNavigate();
  const [state, dispatch] = useReducer(wizardReducer, initialState);

  const currentIdx = STEPS.indexOf(state.step);

  const goNext = useCallback(() => {
    const nextIdx = currentIdx + 1;
    if (nextIdx < STEPS.length) {
      dispatch({ type: 'SET_STEP', step: STEPS[nextIdx] });
    }
  }, [currentIdx]);

  const goBack = useCallback(() => {
    const prevIdx = currentIdx - 1;
    if (prevIdx >= 0) {
      dispatch({ type: 'SET_STEP', step: STEPS[prevIdx] });
    }
  }, [currentIdx]);

  const finish = useCallback(() => {
    navigate('/channels', { replace: true });
  }, [navigate]);

  return (
    <div className="min-h-screen bg-background flex flex-col items-center justify-center px-4">
      {/* Progress dots */}
      <div className="flex gap-2 mb-12">
        {STEPS.map((s, i) => (
          <div
            key={s}
            className={`w-2 h-2 rounded-full transition-colors ${
              i < currentIdx ? 'bg-emerald-500' :
              i === currentIdx ? 'bg-foreground' :
              'bg-muted-foreground/30'
            }`}
          />
        ))}
      </div>

      {/* Step content */}
      <div className="w-full max-w-lg">
        {state.step === 'hub-sync' && (
          <HubSyncStep onComplete={() => { dispatch({ type: 'HUB_SYNCED' }); goNext(); }} />
        )}
        {state.step === 'welcome' && (
          <WelcomeStep
            operatorName={state.operatorName}
            onUpdate={(name) => dispatch({ type: 'SET_OPERATOR', name })}
            onNext={goNext}
            onSkip={finish}
            isReSetup={Object.keys(state.providers).length > 0}
          />
        )}
        {state.step === 'providers' && (
          <ProvidersStep
            providers={state.providers}
            tierStrategy={state.tierStrategy}
            onProviderUpdate={(name, configured, validated) =>
              dispatch({ type: 'SET_PROVIDER', name, configured, validated })
            }
            onTierStrategyUpdate={(strategy) =>
              dispatch({ type: 'SET_TIER_STRATEGY', strategy })
            }
            onNext={goNext}
            onBack={goBack}
          />
        )}
        {state.step === 'agent' && (
          <AgentStep
            agentName={state.agentName}
            agentPreset={state.agentPreset}
            platformExpert={state.platformExpert}
            onUpdate={(name, preset) => dispatch({ type: 'SET_AGENT', name, preset })}
            onPlatformExpertToggle={(enabled) => dispatch({ type: 'SET_PLATFORM_EXPERT', enabled })}
            onNext={goNext}
            onBack={goBack}
          />
        )}
        {state.step === 'capabilities' && (
          <CapabilitiesStep
            capabilities={state.capabilities}
            onUpdate={(capabilities) => dispatch({ type: 'SET_CAPABILITIES', capabilities })}
            onNext={goNext}
            onBack={goBack}
          />
        )}
        {state.step === 'chat' && (
          <ChatStep
            agentName={state.agentName}
            operatorName={state.operatorName}
            onFinish={finish}
            onBack={goBack}
          />
        )}
      </div>
    </div>
  );
}

