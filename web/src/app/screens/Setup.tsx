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
const STEP_META: Record<WizardStep, { title: string; description: string }> = {
  'hub-sync': {
    title: 'Prepare the workspace',
    description: 'Check local package state and make sure Agency has the basics it needs before you configure anything else.',
  },
  welcome: {
    title: 'Name the operator',
    description: 'Establish the operator identity Agency will use in chat, audit trails, and first-run guidance.',
  },
  providers: {
    title: 'Connect model providers',
    description: 'Add at least one verified model backend so the first agent can actually run useful work.',
  },
  agent: {
    title: 'Create the first agent',
    description: 'Start with one agent and one clear role instead of configuring the whole system up front.',
  },
  capabilities: {
    title: 'Choose capabilities',
    description: 'Grant only the capabilities you want available now. You can adjust this safely later in Admin.',
  },
  chat: {
    title: 'Open the first conversation',
    description: 'Confirm the agent is reachable, ask a grounded first question, and continue into Channels when ready.',
  },
};

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
  const activeMeta = STEP_META[state.step];

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

  const finish = useCallback((channelName?: string) => {
    navigate(channelName ? `/channels/${encodeURIComponent(channelName)}` : '/overview', { replace: true });
  }, [navigate]);

  return (
    <div className="min-h-screen bg-background px-4 py-8 md:px-6 md:py-12">
      <div className="mx-auto grid w-full max-w-6xl gap-8 lg:grid-cols-[minmax(18rem,24rem)_minmax(0,34rem)] lg:items-start">
        <section className="rounded-[2rem] border border-border bg-surface-alt/75 px-6 py-6 md:px-7 md:py-7">
          <p className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
            Setup
          </p>
          <h1 className="mt-3 text-3xl text-foreground">Bring Agency online with a calmer first run.</h1>
          <p className="mt-3 text-sm leading-6 text-muted-foreground">
            Setup should feel guided, progressive, and easy to recover from. Each step does one job, and nothing here changes destructive behavior.
          </p>

          <div className="mt-8 space-y-3">
            {STEPS.map((step, i) => {
              const isComplete = i < currentIdx;
              const isCurrent = i === currentIdx;
              return (
                <div
                  key={step}
                  className={`rounded-2xl border px-4 py-3 transition-colors ${
                    isCurrent
                      ? 'border-primary/35 bg-card'
                      : isComplete
                        ? 'border-border bg-card/70'
                        : 'border-border/70 bg-transparent'
                  }`}
                >
                  <div className="flex items-start gap-3">
                    <div className={`mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-sm font-medium ${
                      isCurrent ? 'bg-primary text-primary-foreground' : isComplete ? 'bg-foreground text-background' : 'bg-muted text-muted-foreground'
                    }`}>
                      {i + 1}
                    </div>
                    <div className="min-w-0">
                      <div className={`text-sm font-medium ${isCurrent || isComplete ? 'text-foreground' : 'text-muted-foreground'}`}>
                        {STEP_META[step].title}
                      </div>
                      <div className="mt-1 text-sm text-muted-foreground">
                        {STEP_META[step].description}
                      </div>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </section>

        <section className="rounded-[2rem] border border-border bg-card px-5 py-6 md:px-8 md:py-8">
          <div className="mb-8 flex items-center gap-2">
            {STEPS.map((s, i) => (
              <div
                key={s}
                className={`h-2 flex-1 rounded-full transition-colors ${
                  i < currentIdx ? 'bg-primary' :
                  i === currentIdx ? 'bg-foreground' :
                  'bg-muted'
                }`}
              />
            ))}
          </div>

          <div className="mb-8 max-w-2xl">
            <p className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
              Step {currentIdx + 1} of {STEPS.length}
            </p>
            <h2 className="mt-2 text-2xl text-foreground">{activeMeta.title}</h2>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">
              {activeMeta.description}
            </p>
          </div>

          <div className="w-full">
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
        </section>
      </div>
    </div>
  );
}
