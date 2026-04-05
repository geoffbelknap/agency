import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeEach } from 'vitest';
import { TextScaleControl } from '../app/components/TextScaleControl';

const STORAGE_KEY = 'agency-text-scale';

describe('TextScaleControl', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.style.removeProperty('--font-size');
  });

  it('renders the Aa trigger button', () => {
    render(<TextScaleControl />);
    expect(screen.getByRole('button', { name: /text size/i })).toBeInTheDocument();
  });

  it('shows scale options when clicked', () => {
    render(<TextScaleControl />);
    fireEvent.click(screen.getByRole('button', { name: /text size/i }));
    expect(screen.getByText('M')).toBeInTheDocument();
    expect(screen.getByText('XS')).toBeInTheDocument();
    expect(screen.getByText('XXL')).toBeInTheDocument();
  });

  it('applies selected scale to document and localStorage', () => {
    render(<TextScaleControl />);
    fireEvent.click(screen.getByRole('button', { name: /text size/i }));
    fireEvent.click(screen.getByText('L'));
    expect(document.documentElement.style.getPropertyValue('--font-size')).toBe('17px');
    expect(localStorage.getItem(STORAGE_KEY)).toBe('17');
  });

  it('highlights the current scale', () => {
    localStorage.setItem(STORAGE_KEY, '19');
    render(<TextScaleControl />);
    fireEvent.click(screen.getByRole('button', { name: /text size/i }));
    const xlButton = screen.getByText('XL');
    expect(xlButton.closest('button')).toHaveClass('bg-primary');
  });

  it('defaults to 15px when no preference is set', () => {
    render(<TextScaleControl />);
    fireEvent.click(screen.getByRole('button', { name: /text size/i }));
    const mButton = screen.getByText('M');
    expect(mButton.closest('button')).toHaveClass('bg-primary');
  });
});
