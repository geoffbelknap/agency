import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { AgentMemoryPanel } from '../app/screens/agents/AgentMemoryPanel';

describe('AgentMemoryPanel', () => {
  it('renders three section tabs', () => {
    render(<AgentMemoryPanel agentName="alice" />);
    expect(screen.getByText('Procedures')).toBeInTheDocument();
    expect(screen.getByText('Episodes')).toBeInTheDocument();
    expect(screen.getByText('Trajectory')).toBeInTheDocument();
  });

  it('loads and displays procedures', async () => {
    render(<AgentMemoryPanel agentName="alice" />);
    fireEvent.click(screen.getByText('Procedures'));
    await waitFor(() => {
      expect(screen.getByText('test-mission')).toBeInTheDocument();
      expect(screen.getByText('success')).toBeInTheDocument();
    });
  });

  it('loads and displays episodes with inline notable event', async () => {
    render(<AgentMemoryPanel agentName="alice" />);
    fireEvent.click(screen.getByText('Episodes'));
    await waitFor(() => {
      expect(screen.getByText('Completed research task successfully')).toBeInTheDocument();
      expect(screen.getByText(/Found critical bug/)).toBeInTheDocument();
    });
  });

  it('loads and displays trajectory with anomaly', async () => {
    render(<AgentMemoryPanel agentName="alice" />);
    fireEvent.click(screen.getByText('Trajectory'));
    await waitFor(() => {
      expect(screen.getByText('Trajectory Monitor')).toBeInTheDocument();
      expect(screen.getByText(/Repeated action 5 times/)).toBeInTheDocument();
      expect(screen.getByText('loop_detector')).toBeInTheDocument();
    });
  });

  it('shows outcome filter on procedures tab', async () => {
    render(<AgentMemoryPanel agentName="alice" />);
    fireEvent.click(screen.getByText('Procedures'));
    await waitFor(() => {
      expect(screen.getByDisplayValue('all')).toBeInTheDocument();
    });
  });
});
