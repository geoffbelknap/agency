import { useState, useEffect, useRef, useCallback } from 'react';

const DRAFT_KEY_PREFIX = 'agency:draft:';
const DEBOUNCE_MS = 500;

function getDraftKey(channelName: string): string {
  return `${DRAFT_KEY_PREFIX}${channelName}`;
}

function readDraft(channelName: string): string {
  try {
    return localStorage.getItem(getDraftKey(channelName)) ?? '';
  } catch {
    return '';
  }
}

/**
 * Persist per-channel message drafts in localStorage.
 *
 * Returns [draft, setDraft, clearDraft].
 * - setDraft(value): updates state immediately, debounces localStorage write by 500ms.
 * - clearDraft(): removes from localStorage and resets state to ''.
 * - When channelName changes, loads the draft for the new channel.
 */
export function useDraft(channelName: string): [string, (value: string) => void, () => void] {
  const [draft, setDraftState] = useState<string>(() => readDraft(channelName));

  // Load draft when channelName changes
  useEffect(() => {
    setDraftState(readDraft(channelName));
  }, [channelName]);

  // Keep a ref to the debounce timer
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Keep a ref to channelName so the debounce callback always uses the latest value
  const channelNameRef = useRef(channelName);
  channelNameRef.current = channelName;

  const setDraft = useCallback((value: string) => {
    setDraftState(value);

    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
    }

    timerRef.current = setTimeout(() => {
      try {
        localStorage.setItem(getDraftKey(channelNameRef.current), value);
      } catch {
        // localStorage may be unavailable (e.g. private browsing quota exceeded)
      }
      timerRef.current = null;
    }, DEBOUNCE_MS);
  }, []);

  const clearDraft = useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    try {
      localStorage.removeItem(getDraftKey(channelNameRef.current));
    } catch {
      // ignore
    }
    setDraftState('');
  }, []);

  // Clean up pending timer on unmount
  useEffect(() => {
    return () => {
      if (timerRef.current !== null) {
        clearTimeout(timerRef.current);
      }
    };
  }, []);

  return [draft, setDraft, clearDraft];
}
