import { useState, useCallback } from 'react';
import { toast } from 'sonner';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { FileText, Download } from 'lucide-react';
import type { Message } from '../../types';
import { api, authenticatedFetch } from '../../lib/api';
import { cn } from '../ui/utils';
import { MessageFlagBadge } from './MessageFlagBadge';
import { StructuredOutput, ALLOWED_ELEMENTS, markdownComponents } from './StructuredOutput';
import { ToolCallCard } from './ToolCallCard';
import { MessageActions } from './MessageActions';
import { ReactionPicker } from './ReactionPicker';
import { EditableMessage } from './EditableMessage';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '../ui/dialog';

interface AgencyMessageProps {
  message: Message;
  agentStatus?: string;
  onReply?: (message: Message) => void;
  showReplyButton?: boolean;
  onEdit?: (message: Message, newContent: string) => void;
  onDelete?: (message: Message) => void;
  onReact?: (message: Message, emoji: string) => void;
  onUnreact?: (message: Message, emoji: string) => void;
  onAgentClick?: (agentName: string) => void;
}

interface GroupedReaction {
  emoji: string;
  count: number;
  hasReacted: boolean;
}

function groupReactions(reactions: Array<{ emoji: string; author: string }> | undefined): GroupedReaction[] {
  if (!reactions || reactions.length === 0) return [];
  const map = new Map<string, { count: number; hasReacted: boolean }>();
  for (const r of reactions) {
    const existing = map.get(r.emoji) || { count: 0, hasReacted: false };
    existing.count++;
    if (r.author === 'operator') existing.hasReacted = true;
    map.set(r.emoji, existing);
  }
  return Array.from(map.entries()).map(([emoji, { count, hasReacted }]) => ({
    emoji,
    count,
    hasReacted,
  }));
}

export function AgencyMessage({
  message,
  agentStatus,
  onReply,
  showReplyButton = true,
  onEdit,
  onDelete,
  onReact,
  onUnreact,
  onAgentClick,
}: AgencyMessageProps) {
  const [editing, setEditing] = useState(false);
  const [reactionPickerOpen, setReactionPickerOpen] = useState(false);
  const [reportOpen, setReportOpen] = useState(false);
  const [reportContent, setReportContent] = useState<string | null>(null);
  const [reportLoading, setReportLoading] = useState(false);

  const groupedReactions = groupReactions(message.metadata?.reactions);

  const handleViewReport = useCallback(async () => {
    const artifactId = message.metadata?.task_id || message.metadata?.attachment_id;
    const agent = message.metadata?.agent;
    if (!artifactId || !agent) return;
    setReportOpen(true);
    if (reportContent !== null) return; // already loaded
    setReportLoading(true);
    try {
      const resp = await authenticatedFetch(api.agents.resultUrl(agent, artifactId));
      const text = await resp.text();
      // Strip YAML frontmatter (---...---) from the artifact
      const stripped = text.replace(/^---[\s\S]*?---\s*/, '');
      setReportContent(stripped);
    } catch {
      setReportContent('_Failed to load report._');
    } finally {
      setReportLoading(false);
    }
  }, [message.metadata, reportContent]);

  const handleSaveEdit = (newContent: string) => {
    onEdit?.(message, newContent);
    setEditing(false);
  };

  const handleReactFromPicker = (emoji: string) => {
    onReact?.(message, emoji);
  };

  if (message.isError) {
    return (
      <div className="flex items-center gap-2 py-1.5 px-3 mx-2 my-1 rounded bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-900/50 text-amber-700 dark:text-amber-300 text-sm">
        <span className="flex-shrink-0">⚠</span>
        <span>{message.content}</span>
        <span className="text-xs text-amber-500/70 ml-auto flex-shrink-0">{message.timestamp}</span>
      </div>
    );
  }

  return (
    <div className={`group flex gap-3 py-1.5 relative ${message.id.startsWith('optimistic-') ? 'opacity-60' : ''}`}>
      {/* Avatar */}
      <div className="relative flex-shrink-0">
        <button
          type="button"
          className={`w-8 h-8 rounded flex items-center justify-center border-0 ${
            message.isSystem ? 'bg-cyan-800' : message.isAgent && onAgentClick ? 'bg-primary cursor-pointer hover:bg-primary/80 transition-colors' : message.isAgent ? 'bg-primary' : 'bg-border'
          }`}
          onClick={message.isAgent && onAgentClick ? () => onAgentClick(message.author) : undefined}
          aria-label={message.isAgent ? `View agent: ${message.author}` : `Avatar for ${message.displayAuthor}`}
          tabIndex={message.isAgent && onAgentClick ? 0 : -1}
        >
          <span className="text-xs font-semibold text-white uppercase">
            {message.isSystem ? 'A' : message.displayAuthor.charAt(0)}
          </span>
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 min-w-0">
        {message.parentId && (
          <div className="text-xs text-muted-foreground mb-0.5">
            ↩ In thread
          </div>
        )}
        <div className="flex items-center gap-2 mb-0.5">
          {message.isAgent && onAgentClick ? (
            <button
              type="button"
              className="text-sm font-medium text-primary hover:text-primary/80 cursor-pointer transition-colors font-mono bg-transparent border-0 p-0"
              onClick={() => onAgentClick(message.author)}
              aria-label={`View agent: ${message.author}`}
            >
              {message.displayAuthor}
            </button>
          ) : (
            <code className={`text-sm font-medium ${
              message.isSystem ? 'text-cyan-400' : 'text-foreground'
            }`}>
              {message.displayAuthor}
            </code>
          )}
          {message.isSystem && (
            <span className="text-xs bg-cyan-50 dark:bg-cyan-950 text-cyan-700 dark:text-cyan-400 px-1.5 py-0.5 rounded">
              SYSTEM
            </span>
          )}
          {message.isAgent && (
            <span className="text-xs bg-accent text-accent-foreground px-1.5 py-0.5 rounded">
              AGENT
            </span>
          )}
          <span className="text-xs text-muted-foreground">{message.timestamp}</span>
          <MessageFlagBadge flag={message.flag} />
        </div>

        {editing ? (
          <EditableMessage
            message={message}
            onSave={handleSaveEdit}
            onCancel={() => setEditing(false)}
          />
        ) : (
          <>
            {message.isAgent ? (
              <StructuredOutput content={message.content} metadata={message.metadata} />
            ) : (
              <div className="text-sm text-foreground/80 prose prose-gray dark:prose-invert prose-sm max-w-none prose-p:my-1 prose-table:text-xs prose-th:px-2 prose-th:py-1 prose-td:px-2 prose-td:py-1 prose-pre:bg-card prose-pre:text-xs">
                <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents} allowedElements={ALLOWED_ELEMENTS} unwrapDisallowed>{message.content}</ReactMarkdown>
              </div>
            )}
          </>
        )}

        {Array.isArray(message.metadata?.tool_calls) && message.metadata.tool_calls.length > 0 && (
          <div className="mt-2 space-y-1">
            {message.metadata.tool_calls.map((call: any, i: number) => (
              <ToolCallCard key={i} call={call} agent={message.author} />
            ))}
          </div>
        )}

        {message.metadata?.has_artifact && message.metadata?.agent && (message.metadata?.task_id || message.metadata?.attachment_id) && (
          <div className="mt-2 flex items-center gap-2">
            <button
              onClick={handleViewReport}
              className="inline-flex items-center gap-1.5 text-xs text-primary hover:text-primary/80 bg-accent border border-border rounded px-2.5 py-1.5 transition-colors"
            >
              <FileText className="w-3 h-3" />
              View full report
            </button>
            <button
              onClick={async () => {
                try {
                  const resp = await authenticatedFetch(api.agents.resultDownloadUrl(message.metadata!.agent, message.metadata!.task_id || message.metadata!.attachment_id));
                  const blob = await resp.blob();
                  const url = URL.createObjectURL(blob);
                  const a = document.createElement('a');
                  a.href = url;
                  a.download = `${message.metadata!.agent}-report.md`;
                  a.click();
                  URL.revokeObjectURL(url);
                } catch { toast.error('Failed to download report'); }
              }}
              className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground/80 bg-secondary border border-border rounded px-2.5 py-1.5 transition-colors"
            >
              <Download className="w-3 h-3" />
              Download .md
            </button>
            <Dialog open={reportOpen} onOpenChange={setReportOpen}>
              <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto bg-card">
                <DialogHeader>
                  <DialogTitle className="text-sm font-medium">
                    Report — {message.metadata.agent}
                  </DialogTitle>
                </DialogHeader>
                {reportLoading ? (
                  <div className="text-sm text-muted-foreground py-8 text-center">Loading…</div>
                ) : (
                  <div className="prose prose-gray dark:prose-invert prose-sm max-w-none prose-p:my-1 prose-pre:bg-secondary prose-pre:text-xs">
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>{reportContent || ''}</ReactMarkdown>
                  </div>
                )}
              </DialogContent>
            </Dialog>
          </div>
        )}

        {/* Reaction badges */}
        {groupedReactions.length > 0 && (
          <div className="flex gap-1 mt-1 flex-wrap">
            {groupedReactions.map(({ emoji, count, hasReacted }) => (
              <button
                key={emoji}
                onClick={() => hasReacted ? onUnreact?.(message, emoji) : onReact?.(message, emoji)}
                aria-label={hasReacted ? `Remove ${emoji} reaction (${count})` : `Add ${emoji} reaction (${count})`}
                aria-pressed={hasReacted}
                className={cn(
                  "inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded-full border",
                  hasReacted ? "bg-accent border-primary/40" : "bg-secondary border-border"
                )}
              >
                {emoji} <span>{count}</span>
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Message actions toolbar — visible on hover */}
      {!editing && (
        <div className="absolute top-0 right-0 -translate-y-1/2 opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 transition-opacity z-10">
          <ReactionPicker
            open={reactionPickerOpen}
            onOpenChange={setReactionPickerOpen}
            onSelect={handleReactFromPicker}
          >
            <div>
              <MessageActions
                message={message}
                onReply={() => onReply?.(message)}
                onReact={() => setReactionPickerOpen(true)}
                onEdit={() => setEditing(true)}
                onDelete={() => onDelete?.(message)}
              />
            </div>
          </ReactionPicker>
        </div>
      )}
    </div>
  );
}
