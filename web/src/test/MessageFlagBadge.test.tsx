import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { MessageFlagBadge } from '../app/components/chat/MessageFlagBadge';

describe('MessageFlagBadge', () => {
  it('renders DECISION badge with green styling', () => {
    const { container, getByText } = render(<MessageFlagBadge flag="DECISION" />);
    const badge = container.firstChild as HTMLElement;
    expect(badge).toBeInTheDocument();
    expect(getByText('DECISION')).toBeInTheDocument();
    expect(badge.className).toMatch(/bg-green-950/);
    expect(badge.className).toMatch(/text-green-400/);
  });

  it('renders BLOCKER badge with red styling', () => {
    const { container, getByText } = render(<MessageFlagBadge flag="BLOCKER" />);
    const badge = container.firstChild as HTMLElement;
    expect(badge).toBeInTheDocument();
    expect(getByText('BLOCKER')).toBeInTheDocument();
    expect(badge.className).toMatch(/bg-red-950/);
    expect(badge.className).toMatch(/text-red-400/);
  });

  it('renders QUESTION badge with amber styling', () => {
    const { container, getByText } = render(<MessageFlagBadge flag="QUESTION" />);
    const badge = container.firstChild as HTMLElement;
    expect(badge).toBeInTheDocument();
    expect(getByText('QUESTION')).toBeInTheDocument();
    expect(badge.className).toMatch(/bg-amber-950/);
    expect(badge.className).toMatch(/text-amber-400/);
  });

  it('renders nothing when flag is null', () => {
    const { container } = render(<MessageFlagBadge flag={null} />);
    expect(container.firstChild).toBeNull();
  });

  it('renders nothing when flag is undefined', () => {
    const { container } = render(<MessageFlagBadge flag={undefined} />);
    expect(container.firstChild).toBeNull();
  });
});
