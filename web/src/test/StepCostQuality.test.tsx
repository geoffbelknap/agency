import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { StepCostQuality } from '../app/screens/missions/StepCostQuality';
import { emptyWizardState } from '../app/screens/missions/serialize';

describe('StepCostQuality', () => {
  it('renders three cost mode cards', () => {
    const state = emptyWizardState();
    render(<StepCostQuality state={state} onChange={vi.fn()} />);
    expect(screen.getByText('Frugal')).toBeInTheDocument();
    expect(screen.getByText('Balanced')).toBeInTheDocument();
    expect(screen.getByText('Thorough')).toBeInTheDocument();
  });

  it('selects a cost mode and calls onChange with preset defaults', () => {
    const state = emptyWizardState();
    const onChange = vi.fn();
    render(<StepCostQuality state={state} onChange={onChange} />);
    fireEvent.click(screen.getByText('Thorough'));
    expect(onChange).toHaveBeenCalled();
    const updated = onChange.mock.calls[0][0];
    expect(updated.cost_mode).toBe('thorough');
    expect(updated.reflection.enabled).toBe(true);
    expect(updated.reflection.max_rounds).toBe(5);
  });

  it('highlights the selected cost mode card', () => {
    const state = { ...emptyWizardState(), cost_mode: 'balanced' as const };
    render(<StepCostQuality state={state} onChange={vi.fn()} />);
    const balancedCard = screen.getByText('Balanced').closest('[data-cost-card]');
    expect(balancedCard?.className).toMatch(/border-primary/);
  });

  it('shows Advanced section when expanded', () => {
    const state = { ...emptyWizardState(), cost_mode: 'balanced' as const };
    render(<StepCostQuality state={state} onChange={vi.fn()} />);
    fireEvent.click(screen.getByText(/Advanced/));
    expect(screen.getByText('Reflection')).toBeInTheDocument();
    expect(screen.getByText('Success Criteria')).toBeInTheDocument();
  });
});
