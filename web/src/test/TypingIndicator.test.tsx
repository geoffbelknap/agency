// src/test/TypingIndicator.test.tsx
import { render, screen } from '@testing-library/react';
import { TypingIndicator } from '../app/components/chat/TypingIndicator';

describe('TypingIndicator', () => {
  it('renders nothing when no agents are typing', () => {
    const { container } = render(<TypingIndicator agents={[]} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('shows single agent thinking', () => {
    render(<TypingIndicator agents={['researcher']} />);
    expect(screen.getByText(/researcher is thinking/)).toBeInTheDocument();
  });

  it('shows multiple agents thinking', () => {
    render(<TypingIndicator agents={['researcher', 'engineer']} />);
    expect(screen.getByText(/researcher and engineer are thinking/)).toBeInTheDocument();
  });

  it('renders animated dots', () => {
    const { container } = render(<TypingIndicator agents={['researcher']} />);
    expect(container.querySelectorAll('.animate-bounce')).toHaveLength(3);
  });
});
