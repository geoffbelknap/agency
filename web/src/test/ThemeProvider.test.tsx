import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ThemeProvider, useTheme } from '../app/components/ThemeProvider';

function TestConsumer() {
  const { theme, setTheme } = useTheme();
  return (
    <div>
      <span data-testid="theme">{theme}</span>
      <button onClick={() => setTheme('light')}>Light</button>
      <button onClick={() => setTheme('dark')}>Dark</button>
      <button onClick={() => setTheme('system')}>System</button>
    </div>
  );
}

describe('ThemeProvider', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.classList.remove('dark');
  });

  it('defaults to system theme (resolves to light in jsdom where matchMedia returns false)', () => {
    render(<ThemeProvider><TestConsumer /></ThemeProvider>);
    expect(screen.getByTestId('theme').textContent).toBe('system');
    // In jsdom, matchMedia('(prefers-color-scheme: dark)') returns false,
    // so the system theme resolves to light — no dark class.
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });

  it('switches to light theme', async () => {
    render(<ThemeProvider><TestConsumer /></ThemeProvider>);
    await userEvent.click(screen.getByText('Light'));
    expect(screen.getByTestId('theme').textContent).toBe('light');
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });

  it('persists theme to localStorage', async () => {
    render(<ThemeProvider><TestConsumer /></ThemeProvider>);
    await userEvent.click(screen.getByText('Light'));
    expect(localStorage.getItem('agency-theme')).toBe('light');
  });

  it('reads persisted theme on mount', () => {
    localStorage.setItem('agency-theme', 'light');
    render(<ThemeProvider><TestConsumer /></ThemeProvider>);
    expect(screen.getByTestId('theme').textContent).toBe('light');
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });
});
