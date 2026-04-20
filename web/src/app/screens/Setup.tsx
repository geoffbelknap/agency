import { useReducer, useCallback } from 'react';
import { useNavigate } from 'react-router';
import { Check } from 'lucide-react';
import { PlatformReadyStep } from './setup/PlatformReadyStep';
import { ProvidersStep } from './setup/ProvidersStep';
import { AgentStep } from './setup/AgentStep';
import { StartingAgentStep } from './setup/StartingAgentStep';
import { ChatStep } from './setup/ChatStep';

type WizardStep = 'platform-ready' | 'providers' | 'agent' | 'chat';

const STEPS: WizardStep[] = ['platform-ready', 'providers', 'agent', 'chat'];
const STEP_META: Record<WizardStep, { label: string; title: string; headline: JSX.Element; description: string }> = {
  'platform-ready': {
    label: 'Platform',
    title: 'Prepare the workspace',
    headline: <>Verify the local <em style={{ color: 'var(--teal-dark)' }}>platform</em>.</>,
    description: 'Confirm the gateway, runtime, and routing surface are reachable before setup changes state.',
  },
  providers: {
    label: 'Providers',
    title: 'Connect providers',
    headline: <>Connect a model <em style={{ color: 'var(--teal-dark)' }}>provider</em>.</>,
    description: 'Verify at least one provider credential. Routing stays best-effort by default.',
  },
  agent: {
    label: 'Agent',
    title: 'Name your agent',
    headline: <>Name your <em style={{ color: 'var(--teal-dark)' }}>agent</em>.</>,
    description: 'Setup will use safe defaults: general assistant behavior, Agency platform knowledge, standard capabilities, and provider web tools.',
  },
  chat: {
    label: 'Chat',
    title: 'Open the first conversation',
    headline: <>Test the first <em style={{ color: 'var(--teal-dark)' }}>conversation</em>.</>,
    description: 'Confirm the agent is reachable and continue into the direct-message workflow.',
  },
};

interface WizardState {
  step: WizardStep;
  providers: Record<string, { configured: boolean; validated: boolean }>;
  agentName: string;
  agentPreset: string;
  agentStarting: boolean;
  agentReady: boolean;
  platformPrepared: boolean;
}

type WizardAction =
  | { type: 'SET_STEP'; step: WizardStep }
  | { type: 'SET_PROVIDER'; name: string; configured: boolean; validated: boolean }
  | { type: 'SET_AGENT'; name: string; preset: string }
  | { type: 'SET_AGENT_STARTING'; starting: boolean }
  | { type: 'SET_AGENT_READY'; ready: boolean }
  | { type: 'PLATFORM_PREPARED' };

function wizardReducer(state: WizardState, action: WizardAction): WizardState {
  switch (action.type) {
    case 'SET_STEP': return { ...state, step: action.step, agentStarting: action.step === 'agent' ? state.agentStarting : false };
    case 'SET_PROVIDER':
      return { ...state, providers: { ...state.providers, [action.name]: { configured: action.configured, validated: action.validated } } };
    case 'SET_AGENT': return { ...state, agentName: action.name, agentPreset: action.preset };
    case 'SET_AGENT_STARTING': return { ...state, agentStarting: action.starting, agentReady: action.starting ? false : state.agentReady };
    case 'SET_AGENT_READY': return { ...state, agentReady: action.ready };
    case 'PLATFORM_PREPARED': return { ...state, platformPrepared: true };
    default: return state;
  }
}

const initialState: WizardState = {
  step: 'platform-ready',
  providers: {},
  agentName: 'henry',
  agentPreset: 'platform-expert',
  agentStarting: false,
  agentReady: false,
  platformPrepared: false,
};

function AgencyMark() {
  return (
    <div style={{ width: 32, height: 32, borderRadius: 8, background: 'var(--warm-2)', border: '0.5px solid var(--teal-border)', display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 2, padding: 4 }}>
      <div style={{ background: 'var(--teal)', borderRadius: 2 }} />
      <div style={{ background: 'var(--ink)', borderRadius: 2 }} />
      <div style={{ background: 'var(--ink)', borderRadius: 2 }} />
      <div style={{ background: 'var(--ink)', borderRadius: 2 }} />
    </div>
  );
}

export function Setup() {
  const navigate = useNavigate();
  const [state, dispatch] = useReducer(wizardReducer, initialState);

  const currentIdx = STEPS.indexOf(state.step);
  const activeMeta = STEP_META[state.step];
  const currentTitle = state.step === 'agent' && state.agentStarting ? 'Starting your agent' : activeMeta.title;

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
    <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', background: 'var(--warm)' }}>
      <div style={{ maxWidth: 960, margin: '0 auto', padding: '44px 36px 72px' }}>
        <header style={{ marginBottom: 34 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 20 }}>
            <AgencyMark />
            <span className="display" style={{ fontSize: 20, color: 'var(--ink)' }}>Agency</span>
          </div>
          <div className="eyebrow" style={{ marginBottom: 8 }}>Step {String(currentIdx + 1).padStart(2, '0')} of {String(STEPS.length).padStart(2, '0')}</div>
          <h1 className="display" style={{ margin: 0, fontSize: 52, fontWeight: 300, letterSpacing: '-0.035em', lineHeight: 1.05, color: 'var(--ink)' }}>
            {activeMeta.headline}
          </h1>
          <p style={{ color: 'var(--ink-mid)', fontSize: 15, maxWidth: 650, margin: '14px 0 0', lineHeight: 1.55 }}>
            {activeMeta.description}
          </p>
        </header>

        <nav style={{ display: 'flex', alignItems: 'center', gap: 0, marginBottom: 32, overflowX: 'auto', paddingBottom: 2 }}>
          {STEPS.map((step, i) => {
            const isComplete = i < currentIdx;
            const isCurrent = i === currentIdx;
            return (
              <div key={step} style={{ display: 'flex', alignItems: 'center', flex: i < STEPS.length - 1 ? '1 0 auto' : '0 0 auto' }}>
                <button
                  type="button"
                  onClick={() => dispatch({ type: 'SET_STEP', step })}
                  style={{ display: 'flex', alignItems: 'center', gap: 8, border: 0, background: 'transparent', padding: 0, cursor: 'pointer' }}
                >
                  <span style={{
                    width: 22,
                    height: 22,
                    borderRadius: '50%',
                    background: isCurrent ? 'var(--ink)' : isComplete ? 'var(--teal)' : 'var(--warm-2)',
                    border: `0.5px solid ${isCurrent ? 'var(--ink)' : 'var(--ink-hairline-strong)'}`,
                    color: isComplete || isCurrent ? 'var(--warm)' : 'var(--ink-mid)',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    fontFamily: 'var(--mono)',
                    fontSize: 10,
                  }}>
                    {isComplete ? <Check size={11} /> : i + 1}
                  </span>
                  <span className="mono" style={{ fontSize: 11, color: isCurrent ? 'var(--ink)' : 'var(--ink-mid)', whiteSpace: 'nowrap' }}>
                    {STEP_META[step].label}
                  </span>
                </button>
                {i < STEPS.length - 1 && (
                  <div style={{ flex: '1 1 42px', minWidth: 38, height: 0.5, background: isComplete ? 'var(--teal)' : 'var(--ink-hairline)', margin: '0 12px' }} />
                )}
              </div>
            );
          })}
        </nav>

        <section style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 14, background: 'var(--warm-2)', overflow: 'hidden' }}>
          <div style={{ borderBottom: '0.5px solid var(--ink-hairline)' }}>
            <div style={{ padding: '22px 28px' }}>
              <div className="eyebrow" style={{ fontSize: 9, marginBottom: 8 }}>Current task</div>
              <h2 style={{ margin: 0, color: 'var(--ink)', fontSize: 28, fontWeight: 400, letterSpacing: '-0.03em' }}>{currentTitle}</h2>
            </div>
          </div>

          <div style={{ padding: '30px 32px 28px', background: 'var(--warm)' }}>
            {state.step === 'platform-ready' && (
              <PlatformReadyStep onComplete={() => { dispatch({ type: 'PLATFORM_PREPARED' }); goNext(); }} />
            )}
            {state.step === 'providers' && (
              <ProvidersStep
                providers={state.providers}
                onProviderUpdate={(name, configured, validated) =>
                  dispatch({ type: 'SET_PROVIDER', name, configured, validated })
                }
                onNext={goNext}
                onBack={goBack}
              />
            )}
            {state.step === 'agent' && (
              state.agentStarting ? (
                <StartingAgentStep
                  agentName={state.agentName}
                  onReady={() => {
                    dispatch({ type: 'SET_AGENT_STARTING', starting: false });
                    dispatch({ type: 'SET_AGENT_READY', ready: true });
                    dispatch({ type: 'SET_STEP', step: 'chat' });
                  }}
                  onBack={() => dispatch({ type: 'SET_AGENT_STARTING', starting: false })}
                />
              ) : (
                <AgentStep
                  agentName={state.agentName}
                  onUpdate={(name, preset) => dispatch({ type: 'SET_AGENT', name, preset })}
                  onNext={() => dispatch({ type: 'SET_AGENT_STARTING', starting: true })}
                  onBack={goBack}
                />
              )
            )}
            {state.step === 'chat' && (
              <ChatStep
                agentName={state.agentName}
                initialAgentReady={state.agentReady}
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
