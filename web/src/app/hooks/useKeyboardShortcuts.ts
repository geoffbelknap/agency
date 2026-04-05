import { useEffect, useRef } from 'react';

export interface ShortcutConfig {
  key: string;           // e.g., 'k', 'Escape', 'ArrowUp', '/'
  ctrl?: boolean;        // Ctrl on Win/Linux, Cmd on Mac
  alt?: boolean;
  shift?: boolean;
  handler: () => void;
  ignoreWhenEditing?: boolean;  // default true — skip when focus is in input/textarea
}

function isMac(): boolean {
  return /mac/i.test(navigator.platform) || /mac/i.test(navigator.userAgent);
}

function isEditingElement(el: Element | null): boolean {
  if (!el) return false;
  const tag = el.tagName.toLowerCase();
  if (tag === 'input' || tag === 'textarea') return true;
  if (el.getAttribute('contenteditable') != null &&
      el.getAttribute('contenteditable') !== 'false') return true;
  return false;
}

export function useKeyboardShortcuts(shortcuts: ShortcutConfig[]): void {
  // Keep a stable ref so listener doesn't need to be re-registered on every render
  const shortcutsRef = useRef<ShortcutConfig[]>(shortcuts);
  shortcutsRef.current = shortcuts;

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const mac = isMac();

      for (const shortcut of shortcutsRef.current) {
        const ignoreWhenEditing = shortcut.ignoreWhenEditing !== false;

        if (ignoreWhenEditing && isEditingElement(document.activeElement)) {
          continue;
        }

        // Key match (case-sensitive as spec'd, e.g. 'k' vs 'K')
        if (e.key !== shortcut.key) continue;

        // Ctrl modifier: Ctrl on Win/Linux, Meta (Cmd) on Mac
        if (shortcut.ctrl) {
          const ctrlPressed = mac ? e.metaKey : e.ctrlKey;
          if (!ctrlPressed) continue;
        }

        // Alt modifier
        if (shortcut.alt) {
          if (!e.altKey) continue;
        }

        // Shift modifier
        if (shortcut.shift) {
          if (!e.shiftKey) continue;
        }

        e.preventDefault();
        shortcut.handler();
      }
    };

    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, []); // Empty deps — always use latest via ref
}
