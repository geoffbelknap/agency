import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useDraft } from '../app/hooks/useDraft';

describe('useDraft', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    localStorage.clear();
  });

  it('returns empty string when no draft stored', () => {
    const { result } = renderHook(() => useDraft('general'));
    const [draft] = result.current;
    expect(draft).toBe('');
  });

  it('loads stored draft on mount', () => {
    localStorage.setItem('agency:draft:general', 'hello from storage');
    const { result } = renderHook(() => useDraft('general'));
    const [draft] = result.current;
    expect(draft).toBe('hello from storage');
  });

  it('saves draft to localStorage after debounce (500ms)', () => {
    const { result } = renderHook(() => useDraft('general'));
    const [, setDraft] = result.current;

    act(() => {
      setDraft('typing something');
    });

    // Not yet saved before debounce
    expect(localStorage.getItem('agency:draft:general')).toBeNull();

    act(() => {
      vi.advanceTimersByTime(500);
    });

    expect(localStorage.getItem('agency:draft:general')).toBe('typing something');
  });

  it('does not save to localStorage until debounce fires', () => {
    const { result } = renderHook(() => useDraft('general'));
    const [, setDraft] = result.current;

    act(() => {
      setDraft('partial');
    });

    act(() => {
      vi.advanceTimersByTime(499);
    });

    expect(localStorage.getItem('agency:draft:general')).toBeNull();
  });

  it('debounce resets on each setDraft call', () => {
    const { result } = renderHook(() => useDraft('general'));
    const [, setDraft] = result.current;

    act(() => {
      setDraft('first');
    });

    act(() => {
      vi.advanceTimersByTime(300);
      setDraft('second');
    });

    act(() => {
      vi.advanceTimersByTime(300);
    });

    // Only 300ms after 'second', so debounce not fired yet
    expect(localStorage.getItem('agency:draft:general')).toBeNull();

    act(() => {
      vi.advanceTimersByTime(200);
    });

    expect(localStorage.getItem('agency:draft:general')).toBe('second');
  });

  it('clears draft on clearDraft()', () => {
    localStorage.setItem('agency:draft:general', 'saved draft');
    const { result } = renderHook(() => useDraft('general'));

    act(() => {
      const [, , clearDraft] = result.current;
      clearDraft();
    });

    const [draft] = result.current;
    expect(draft).toBe('');
    expect(localStorage.getItem('agency:draft:general')).toBeNull();
  });

  it('each channel has its own draft key', () => {
    localStorage.setItem('agency:draft:general', 'general draft');
    localStorage.setItem('agency:draft:ops', 'ops draft');

    const { result: generalResult } = renderHook(() => useDraft('general'));
    const { result: opsResult } = renderHook(() => useDraft('ops'));

    expect(generalResult.current[0]).toBe('general draft');
    expect(opsResult.current[0]).toBe('ops draft');
  });

  it('switches draft when channelName changes', () => {
    localStorage.setItem('agency:draft:general', 'general draft');
    localStorage.setItem('agency:draft:ops', 'ops draft');

    const { result, rerender } = renderHook(
      ({ channel }: { channel: string }) => useDraft(channel),
      { initialProps: { channel: 'general' } }
    );

    expect(result.current[0]).toBe('general draft');

    rerender({ channel: 'ops' });

    expect(result.current[0]).toBe('ops draft');
  });

  it('switches to empty string when new channel has no draft', () => {
    localStorage.setItem('agency:draft:general', 'general draft');

    const { result, rerender } = renderHook(
      ({ channel }: { channel: string }) => useDraft(channel),
      { initialProps: { channel: 'general' } }
    );

    expect(result.current[0]).toBe('general draft');

    rerender({ channel: 'new-channel' });

    expect(result.current[0]).toBe('');
  });

  it('uses correct localStorage key format agency:draft:{channelName}', () => {
    const { result } = renderHook(() => useDraft('my-channel'));
    const [, setDraft] = result.current;

    act(() => {
      setDraft('test');
    });

    act(() => {
      vi.advanceTimersByTime(500);
    });

    expect(localStorage.getItem('agency:draft:my-channel')).toBe('test');
    // No other keys should be set
    expect(localStorage.getItem('agency:draft:general')).toBeNull();
  });
});
