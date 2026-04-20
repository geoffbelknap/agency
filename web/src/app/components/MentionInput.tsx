import { useState, useEffect, useRef, useCallback, useId } from 'react';
import { Input } from './ui/input';
import { api } from '../lib/api';

interface MentionTarget {
  name: string;
  type: 'agent' | 'operator';
}

// Module-level cache so all MentionInput instances share one fetch
let cachedTargets: MentionTarget[] | null = null;
let fetchPromise: Promise<MentionTarget[]> | null = null;

function fetchMentionTargets(): Promise<MentionTarget[]> {
  if (cachedTargets) return Promise.resolve(cachedTargets);
  if (fetchPromise) return fetchPromise;
  fetchPromise = api.agents.list().then((agents: any[]) => {
    const items: MentionTarget[] = [
      ...agents.map((a: any) => ({
        name: a.name,
        type: 'agent' as const,
      })),
    ];
    cachedTargets = items;
    // Expire cache after 30s so new agents are picked up
    setTimeout(() => { cachedTargets = null; fetchPromise = null; }, 30_000);
    return items;
  }).catch(() => {
    fetchPromise = null;
    return [] as MentionTarget[];
  });
  return fetchPromise;
}

interface MentionInputProps {
  value: string;
  onChange: (value: string) => void;
  onSubmit: () => void;
  placeholder?: string;
  className?: string;
  id?: string;
  name?: string;
}

export function MentionInput({ value, onChange, onSubmit, placeholder, className, id, name = 'message' }: MentionInputProps) {
  const [targets, setTargets] = useState<MentionTarget[]>([]);
  const [showMenu, setShowMenu] = useState(false);
  const [menuFilter, setMenuFilter] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [mentionStart, setMentionStart] = useState(-1);
  const inputRef = useRef<HTMLInputElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const blurTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const generatedId = useId();
  const inputId = id ?? `mention-input-${generatedId}`;

  const loadTargets = useCallback(() => {
    fetchMentionTargets().then(setTargets);
  }, []);

  useEffect(() => {
    return () => {
      if (blurTimeoutRef.current !== null) {
        clearTimeout(blurTimeoutRef.current);
      }
    };
  }, []);

  const filtered = targets.filter((t) =>
    t.name.toLowerCase().startsWith(menuFilter.toLowerCase())
  );

  const completeMention = useCallback((target: MentionTarget) => {
    if (mentionStart < 0) return;
    const before = value.slice(0, mentionStart);
    const after = value.slice(mentionStart + menuFilter.length + 1); // +1 for @
    const completed = `${before}@${target.name} ${after}`;
    onChange(completed);
    setShowMenu(false);
    setMenuFilter('');
    setMentionStart(-1);
    // Restore focus after React re-render
    requestAnimationFrame(() => {
      if (inputRef.current) {
        inputRef.current.focus();
        const cursor = before.length + target.name.length + 2; // @name + space
        inputRef.current.setSelectionRange(cursor, cursor);
      }
    });
  }, [value, mentionStart, menuFilter, onChange]);

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const newValue = e.target.value;
    const cursor = e.target.selectionStart ?? newValue.length;
    onChange(newValue);

    // Find @ trigger: look backward from cursor for an unescaped @
    const textBeforeCursor = newValue.slice(0, cursor);
    const atIndex = textBeforeCursor.lastIndexOf('@');

    if (atIndex >= 0) {
      // @ must be at start or preceded by a space
      const charBefore = atIndex > 0 ? newValue[atIndex - 1] : ' ';
      const fragment = textBeforeCursor.slice(atIndex + 1);
      // Only trigger if fragment has no spaces (still typing the mention)
      if ((charBefore === ' ' || atIndex === 0) && !/\s/.test(fragment)) {
        if (targets.length === 0) loadTargets();
        setShowMenu(true);
        setMenuFilter(fragment);
        setMentionStart(atIndex);
        setSelectedIndex(0);
        return;
      }
    }

    setShowMenu(false);
    setMenuFilter('');
    setMentionStart(-1);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (showMenu && filtered.length > 0) {
      if (e.key === 'Tab' || (e.key === 'Enter' && showMenu)) {
        e.preventDefault();
        completeMention(filtered[selectedIndex]);
        return;
      }
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setSelectedIndex((i) => (i + 1) % filtered.length);
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        setSelectedIndex((i) => (i - 1 + filtered.length) % filtered.length);
        return;
      }
      if (e.key === 'Escape') {
        e.preventDefault();
        setShowMenu(false);
        return;
      }
    }

    // Normal Enter (no menu) = send message
    if (e.key === 'Enter' && !e.shiftKey && !showMenu) {
      e.preventDefault();
      onSubmit();
    }
  };

  const handleBlur = () => {
    if (blurTimeoutRef.current !== null) {
      clearTimeout(blurTimeoutRef.current);
    }
    // Delay close so click on menu item registers.
    blurTimeoutRef.current = setTimeout(() => {
      setShowMenu(false);
      blurTimeoutRef.current = null;
    }, 150);
  };

  return (
    <div className="relative flex-1">
      <Input
        ref={inputRef}
        id={inputId}
        name={name}
        value={value}
        onChange={handleChange}
        onKeyDown={handleKeyDown}
        onBlur={handleBlur}
        placeholder={placeholder}
        className={className}
        aria-autocomplete="list"
        aria-expanded={showMenu && filtered.length > 0}
        aria-activedescendant={showMenu && filtered.length > 0 ? `mention-opt-${selectedIndex}` : undefined}
      />
      {showMenu && filtered.length > 0 && (
        <div
          ref={menuRef}
          className="absolute bottom-full mb-1 left-0 w-64 bg-card border border-border rounded-md shadow-lg overflow-hidden z-50"
        >
          <div className="px-2 py-1.5 text-xs text-muted-foreground border-b border-border">
            Mentions
          </div>
          <div role="listbox" aria-label="Mentions" className="max-h-48 overflow-y-auto py-1">
            {filtered.map((target, i) => (
              <button
                key={target.name}
                id={`mention-opt-${i}`}
                role="option"
                aria-selected={i === selectedIndex}
                className={`w-full px-3 py-1.5 flex items-center gap-2 text-sm text-left transition-colors ${
                  i === selectedIndex
                    ? 'bg-primary/30 text-primary/80'
                    : 'text-foreground/80 hover:bg-accent'
                }`}
                onMouseDown={(e) => {
                  e.preventDefault(); // Prevent blur
                  completeMention(target);
                }}
                onMouseEnter={() => setSelectedIndex(i)}
              >
                <span
                  className={`w-5 h-5 rounded flex items-center justify-center text-xs font-semibold flex-shrink-0 ${
                    target.type === 'agent' ? 'bg-primary text-white' : 'bg-border text-foreground'
                  }`}
                >
                  {target.name.charAt(0).toUpperCase()}
                </span>
                <span className="flex-1">{target.name}</span>
                {target.type === 'operator' && (
                  <span className="text-xs text-muted-foreground">you</span>
                )}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
