import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { fireEvent } from '@testing-library/dom';
import { useKeyboardShortcuts } from '../app/hooks/useKeyboardShortcuts';

describe('useKeyboardShortcuts', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Restore activeElement to body between tests
    (document.body as HTMLElement).focus();
  });

  it('calls handler when matching key is pressed', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboardShortcuts([{ key: 'k', handler }])
    );
    fireEvent.keyDown(document, { key: 'k' });
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it('does not call handler when a different key is pressed', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboardShortcuts([{ key: 'k', handler }])
    );
    fireEvent.keyDown(document, { key: 'j' });
    expect(handler).not.toHaveBeenCalled();
  });

  it('calls handler when Ctrl+K is pressed (ctrlKey)', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboardShortcuts([{ key: 'k', ctrl: true, handler }])
    );
    fireEvent.keyDown(document, { key: 'k', ctrlKey: true });
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it('does not call Ctrl+K handler when only K is pressed (no ctrl)', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboardShortcuts([{ key: 'k', ctrl: true, handler }])
    );
    fireEvent.keyDown(document, { key: 'k', ctrlKey: false });
    expect(handler).not.toHaveBeenCalled();
  });

  it('matches Meta key (Cmd) when ctrl is set and platform is Mac', () => {
    // Simulate Mac platform
    Object.defineProperty(navigator, 'platform', {
      value: 'MacIntel',
      configurable: true,
    });

    const handler = vi.fn();
    renderHook(() =>
      useKeyboardShortcuts([{ key: 'k', ctrl: true, handler }])
    );
    fireEvent.keyDown(document, { key: 'k', metaKey: true });
    expect(handler).toHaveBeenCalledTimes(1);

    // Restore platform
    Object.defineProperty(navigator, 'platform', {
      value: '',
      configurable: true,
    });
  });

  it('matches Alt key when alt is set', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboardShortcuts([{ key: 'ArrowDown', alt: true, handler }])
    );
    fireEvent.keyDown(document, { key: 'ArrowDown', altKey: true });
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it('does not call handler when alt is required but not pressed', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboardShortcuts([{ key: 'ArrowDown', alt: true, handler }])
    );
    fireEvent.keyDown(document, { key: 'ArrowDown', altKey: false });
    expect(handler).not.toHaveBeenCalled();
  });

  it('matches Shift key when shift is set', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboardShortcuts([{ key: '?', shift: true, handler }])
    );
    fireEvent.keyDown(document, { key: '?', shiftKey: true });
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it('skips handler when activeElement is an input (ignoreWhenEditing default true)', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboardShortcuts([{ key: 'k', handler }])
    );

    const input = document.createElement('input');
    document.body.appendChild(input);
    input.focus();

    fireEvent.keyDown(document, { key: 'k' });
    expect(handler).not.toHaveBeenCalled();

    document.body.removeChild(input);
  });

  it('skips handler when activeElement is a textarea (ignoreWhenEditing default true)', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboardShortcuts([{ key: 'Escape', handler }])
    );

    const textarea = document.createElement('textarea');
    document.body.appendChild(textarea);
    textarea.focus();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(handler).not.toHaveBeenCalled();

    document.body.removeChild(textarea);
  });

  it('fires handler when activeElement is input but ignoreWhenEditing is false', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboardShortcuts([{ key: 'Escape', handler, ignoreWhenEditing: false }])
    );

    const input = document.createElement('input');
    document.body.appendChild(input);
    input.focus();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(handler).toHaveBeenCalledTimes(1);

    document.body.removeChild(input);
  });

  it('skips handler when activeElement has contenteditable', () => {
    const handler = vi.fn();
    renderHook(() =>
      useKeyboardShortcuts([{ key: 'k', handler }])
    );

    const div = document.createElement('div');
    div.setAttribute('contenteditable', 'true');
    document.body.appendChild(div);
    div.focus();

    fireEvent.keyDown(document, { key: 'k' });
    expect(handler).not.toHaveBeenCalled();

    document.body.removeChild(div);
  });

  it('cleans up listener on unmount', () => {
    const handler = vi.fn();
    const { unmount } = renderHook(() =>
      useKeyboardShortcuts([{ key: 'k', handler }])
    );

    unmount();
    fireEvent.keyDown(document, { key: 'k' });
    expect(handler).not.toHaveBeenCalled();
  });

  it('handles multiple shortcuts in a single hook call', () => {
    const handlerA = vi.fn();
    const handlerB = vi.fn();
    renderHook(() =>
      useKeyboardShortcuts([
        { key: 'a', handler: handlerA },
        { key: 'b', handler: handlerB },
      ])
    );

    fireEvent.keyDown(document, { key: 'a' });
    expect(handlerA).toHaveBeenCalledTimes(1);
    expect(handlerB).not.toHaveBeenCalled();

    fireEvent.keyDown(document, { key: 'b' });
    expect(handlerB).toHaveBeenCalledTimes(1);
  });

  it('updates handlers when shortcuts change (uses latest ref)', () => {
    const handler1 = vi.fn();
    const handler2 = vi.fn();
    let currentHandler = handler1;

    const { rerender } = renderHook(() =>
      useKeyboardShortcuts([{ key: 'k', handler: currentHandler }])
    );

    fireEvent.keyDown(document, { key: 'k' });
    expect(handler1).toHaveBeenCalledTimes(1);

    currentHandler = handler2;
    rerender();

    fireEvent.keyDown(document, { key: 'k' });
    expect(handler2).toHaveBeenCalledTimes(1);
  });
});
