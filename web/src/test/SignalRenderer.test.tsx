import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { SignalRenderer } from '../app/screens/agents/SignalRenderer';

describe('SignalRenderer', () => {
  it('renders reflection_cycle with approved verdict', () => {
    render(<SignalRenderer signal={{ type: 'reflection_cycle', data: { round: 2, verdict: 'approved', issues: [] } }} />);
    expect(screen.getByText(/Reflection round 2: approved/)).toBeInTheDocument();
  });

  it('renders reflection_cycle with revision-needed and issues', () => {
    render(<SignalRenderer signal={{ type: 'reflection_cycle', data: { round: 1, verdict: 'revision-needed', issues: ['Missing error handling', 'No tests'] } }} />);
    expect(screen.getByText(/revision-needed/)).toBeInTheDocument();
    expect(screen.getByText('Missing error handling')).toBeInTheDocument();
    expect(screen.getByText('No tests')).toBeInTheDocument();
  });

  it('renders fallback_activated with policy steps', () => {
    render(<SignalRenderer signal={{ type: 'fallback_activated', data: { trigger: 'tool_error', tool: 'web_search', policy_steps: ['retry', 'degrade', 'escalate'] } }} />);
    expect(screen.getByText(/Fallback: tool_error on web_search/)).toBeInTheDocument();
    expect(screen.getByText('retry')).toBeInTheDocument();
    expect(screen.getByText('escalate')).toBeInTheDocument();
  });

  it('renders trajectory_anomaly with warning severity', () => {
    render(<SignalRenderer signal={{ type: 'trajectory_anomaly', data: { detector: 'loop_detector', detail: 'Repeated action 5 times', severity: 'warning' } }} />);
    expect(screen.getByText(/Trajectory: Repeated action 5 times/)).toBeInTheDocument();
    expect(screen.getByText('loop_detector')).toBeInTheDocument();
  });

  it('renders trajectory_anomaly with critical severity using red styling', () => {
    const { container } = render(<SignalRenderer signal={{ type: 'trajectory_anomaly', data: { detector: 'drift_detector', detail: 'Agent drifted from objective', severity: 'critical' } }} />);
    const wrapper = container.firstChild as HTMLElement;
    expect(wrapper.className).toMatch(/red|destructive/);
  });

  it('renders task_complete with reflection rounds', () => {
    render(<SignalRenderer signal={{ type: 'task_complete', data: { reflection_rounds: 3, reflection_forced: false, tier: 'standard' } }} />);
    expect(screen.getByText(/reflected 3x/)).toBeInTheDocument();
    expect(screen.getByText('standard')).toBeInTheDocument();
  });

  it('renders task_complete with failed evaluation', () => {
    render(<SignalRenderer signal={{ type: 'task_complete', data: { evaluation: { passed: false, mode: 'llm', criteria_results: [{ passed: false }, { passed: false }] }, tier: 'full' } }} />);
    expect(screen.getByText(/2 criteria failed/)).toBeInTheDocument();
  });

  it('renders task_complete with passed evaluation', () => {
    render(<SignalRenderer signal={{ type: 'task_complete', data: { evaluation: { passed: true }, tier: 'minimal' } }} />);
    expect(screen.getByText(/evaluation: passed/)).toBeInTheDocument();
  });

  it('renders unknown signal type as raw text', () => {
    render(<SignalRenderer signal={{ type: 'agent_status', data: { status: 'running' } }} />);
    expect(document.querySelector('[data-signal]')).toBeInTheDocument();
  });
});
