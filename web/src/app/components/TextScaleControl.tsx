import { useState, useEffect, useCallback } from 'react';

const STORAGE_KEY = 'agency-text-scale';
const DEFAULT_SIZE = 15;

const SCALES = [
  { label: 'XS', size: 12 },
  { label: 'S', size: 13 },
  { label: 'M', size: 15 },
  { label: 'L', size: 17 },
  { label: 'XL', size: 19 },
  { label: 'XXL', size: 22 },
] as const;

function getStoredSize(): number {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored) return parseInt(stored, 10);
  if (window.innerWidth < 640) return 17;
  return DEFAULT_SIZE;
}

function applySize(size: number) {
  document.documentElement.style.setProperty('--font-size', `${size}px`);
}

export function TextScaleControl() {
  const [open, setOpen] = useState(false);
  const [currentSize, setCurrentSize] = useState(getStoredSize);

  const select = useCallback((size: number) => {
    setCurrentSize(size);
    applySize(size);
    localStorage.setItem(STORAGE_KEY, String(size));
  }, []);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      if (!target.closest('[data-text-scale]')) setOpen(false);
    };
    document.addEventListener('click', handler);
    return () => document.removeEventListener('click', handler);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [open]);

  return (
    <div className="relative" data-text-scale>
      <button
        aria-label="Text size"
        onClick={() => setOpen(!open)}
        className="flex items-center justify-center w-4 h-4 rounded text-muted-foreground hover:text-foreground transition-colors text-[10px] font-semibold focus-visible:ring-2 focus-visible:ring-primary/50"
      >
        Aa
      </button>
      {open && (
        <div className="absolute bottom-10 left-0 bg-popover border border-border rounded-lg shadow-xl p-2 flex gap-1 z-50">
          {SCALES.map(({ label, size }) => (
            <button
              key={label}
              onClick={() => select(size)}
              aria-pressed={currentSize === size}
              className={`px-2 py-1 rounded text-xs font-medium transition-colors focus-visible:ring-2 focus-visible:ring-primary/50 ${
                currentSize === size
                  ? 'bg-primary text-primary-foreground'
                  : 'text-muted-foreground hover:bg-secondary'
              }`}
            >
              {label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
