import { useState, useEffect, useRef, useCallback } from 'react';
import { Search, X } from 'lucide-react';
import { Button } from '../ui/button';
import { Input } from '../ui/input';
import { ScrollArea } from '../ui/scroll-area';
import { Badge } from '../ui/badge';
import { Sheet, SheetContent } from '../ui/sheet';
import { useIsMobile } from '../ui/use-mobile';
import { api } from '../../lib/api';

export interface SearchPanelProps {
  onClose: () => void;
  onJumpToMessage: (channelName: string, messageId: string) => void;
}

interface SearchResult {
  id: string;
  channel: string;
  author: string;
  timestamp: string;
  content: string;
  flags: Record<string, boolean>;
  metadata: Record<string, unknown>;
}

function formatTimestamp(ts: string): string {
  try {
    const d = new Date(ts);
    return d.toLocaleString(undefined, {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  } catch {
    return ts.slice(0, 16).replace('T', ' ');
  }
}

function highlightQuery(text: string, query: string): React.ReactNode {
  if (!query.trim()) return text;
  const idx = text.toLowerCase().indexOf(query.toLowerCase());
  if (idx === -1) return text;
  return (
    <>
      {text.slice(0, idx)}
      <mark className="bg-yellow-400/30 text-inherit rounded-sm px-0.5">
        {text.slice(idx, idx + query.length)}
      </mark>
      {text.slice(idx + query.length)}
    </>
  );
}

function SearchPanelContent({ onClose, onJumpToMessage, query, setQuery, results, loading, searched }: SearchPanelProps & { query: string; setQuery: (q: string) => void; results: SearchResult[]; loading: boolean; searched: boolean }) {
  return (
    <>
      {/* Header */}
      <div className="p-4 border-b border-border flex items-center justify-between">
        <h3 className="font-semibold text-foreground">Search</h3>
        <Button
          variant="ghost"
          size="icon"
          onClick={onClose}
          aria-label="Close search"
          className="h-9 w-9 text-muted-foreground hover:text-foreground"
        >
          <X className="w-5 h-5" />
        </Button>
      </div>

      {/* Search input */}
      <div className="p-3 border-b border-border">
        <div className="relative">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search messages..."
            className="pl-8 bg-card border-border text-foreground placeholder:text-muted-foreground"
            autoFocus
          />
        </div>
      </div>

      {/* Results area */}
      <ScrollArea className="flex-1">
        <div className="p-2">
          {loading ? (
            <div className="flex items-center justify-center h-16 text-sm text-muted-foreground">
              Searching...
            </div>
          ) : !query.trim() ? (
            <div className="flex items-center justify-center h-16 text-sm text-muted-foreground">
              Search messages...
            </div>
          ) : searched && results.length === 0 ? (
            <div className="flex items-center justify-center h-16 text-sm text-muted-foreground">
              No results
            </div>
          ) : (
            results.map((result) => (
              <button
                key={result.id}
                onClick={() => onJumpToMessage(result.channel, result.id)}
                className="w-full text-left p-3 rounded-md hover:bg-accent transition-colors mb-1 group"
              >
                <div className="flex items-center gap-2 mb-1">
                  <Badge
                    variant="secondary"
                    className="text-xs px-1.5 py-0 bg-border text-foreground/80 font-normal"
                  >
                    #{result.channel}
                  </Badge>
                  <span className="text-xs font-medium text-foreground/80">{result.author}</span>
                  <span className="text-xs text-muted-foreground ml-auto">
                    {formatTimestamp(result.timestamp)}
                  </span>
                </div>
                <p className="text-sm text-muted-foreground line-clamp-2 group-hover:text-foreground transition-colors">
                  {highlightQuery(result.content, query)}
                </p>
              </button>
            ))
          )}
        </div>
      </ScrollArea>
    </>
  );
}

export function SearchPanel({ onClose, onJumpToMessage }: SearchPanelProps) {
  const isMobile = useIsMobile();
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<SearchResult[]>([]);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const runSearch = useCallback(async (q: string) => {
    if (!q.trim()) {
      setResults([]);
      setSearched(false);
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const data = await api.channels.search(q);
      setResults(data as SearchResult[]);
      setSearched(true);
    } catch (err) {
      console.error('SearchPanel search error:', err);
      setResults([]);
      setSearched(true);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (!query.trim()) {
      setResults([]);
      setSearched(false);
      setLoading(false);
      return;
    }
    debounceRef.current = setTimeout(() => {
      runSearch(query);
    }, 300);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [query, runSearch]);

  const contentProps = { onClose, onJumpToMessage, query, setQuery, results, loading, searched };

  if (isMobile) {
    return (
      <Sheet open onOpenChange={(open) => { if (!open) onClose(); }}>
        <SheetContent side="right" hideClose className="p-0 w-full sm:max-w-full bg-background border-border flex flex-col">
          <SearchPanelContent {...contentProps} />
        </SheetContent>
      </Sheet>
    );
  }

  return (
    <div className="w-96 border-l border-border bg-background flex flex-col h-full">
      <SearchPanelContent {...contentProps} />
    </div>
  );
}
