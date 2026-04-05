import { Popover, PopoverContent, PopoverAnchor } from '../ui/popover';

interface ReactionPickerProps {
  onSelect: (emoji: string) => void;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: React.ReactNode;
}

const EMOJIS: Array<{ char: string; name: string }> = [
  { char: '\u{1F44D}', name: 'thumbs up' },
  { char: '\u{1F44E}', name: 'thumbs down' },
  { char: '\u{1F440}', name: 'eyes' },
  { char: '\u{2764}\u{FE0F}', name: 'heart' },
  { char: '\u{2705}', name: 'check mark' },
  { char: '\u{274C}', name: 'cross mark' },
  { char: '\u{1F680}', name: 'rocket' },
  { char: '\u{1F914}', name: 'thinking' },
];

export function ReactionPicker({ onSelect, open, onOpenChange, children }: ReactionPickerProps) {
  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverAnchor asChild>{children}</PopoverAnchor>
      <PopoverContent className="w-auto p-2" align="end" side="top">
        <div className="grid grid-cols-4 gap-1">
          {EMOJIS.map(({ char, name }) => (
            <button
              key={char}
              onClick={() => {
                onSelect(char);
                onOpenChange(false);
              }}
              aria-label={`React with ${name}`}
              className="w-8 h-8 flex items-center justify-center text-lg rounded hover:bg-accent transition-colors"
            >
              {char}
            </button>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}
