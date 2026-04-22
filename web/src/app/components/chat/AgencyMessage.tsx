import { useState, useCallback } from 'react';
import { toast } from 'sonner';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Bot, FileText, Download, Terminal, ExternalLink } from 'lucide-react';
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

interface AgencyMessageAvatarProps {
  message: Message;
  agentStatus?: string;
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

function initials(message: Message): string {
  if (message.isSystem) return 'A';
  return (message.displayAuthor || message.author || '?').slice(0, 2).toUpperCase();
}

function avatarStyles(message: Message): React.CSSProperties {
  if (message.isSystem) {
    return { background: 'var(--ink)', color: 'var(--warm)' };
  }
  if (message.isAgent) {
    return { background: 'var(--warm-3)', color: 'var(--ink-muted)' };
  }
  return { background: 'var(--ink)', color: 'var(--warm)' };
}

function statusColor(status?: string): string {
  switch (status) {
    case 'running':
      return 'var(--teal)';
    case 'halted':
      return '#d64b4b';
    case 'idle':
      return '#e0a31a';
    default:
      return 'var(--ink-faint)';
  }
}

function splitInlineToolNoise(content: string): { cleanContent: string; toolCalls: any[] } {
  const toolCalls: any[] = [];
  const cleanContent = content
    .split('\n')
    .filter((line) => {
      const trimmed = line.trim();
      const search = trimmed.match(/^<search>\s*query:\s*(.*?)\s*<\/search>$/i);
      if (search) {
        toolCalls.push({ tool: 'web.search', input: { query: search[1] } });
        return false;
      }
      const tool = trimmed.match(/^<([a-z][a-z0-9_.-]*)>\s*(.*?)\s*<\/\1>$/i);
      if (tool) {
        toolCalls.push({ tool: tool[1], input: tool[2] });
        return false;
      }
      return true;
    })
    .join('\n')
    .trim();
  return { cleanContent, toolCalls };
}

function metadataLinks(metadata?: Record<string, any>): Array<{ label: string; url: string }> {
  if (!metadata) return [];
  const links: Array<{ label: string; url: string }> = [];
  const raw = Array.isArray(metadata.links) ? metadata.links : [];
  for (const item of raw) {
    if (typeof item === 'string') links.push({ label: item, url: item });
    else if (item?.url) links.push({ label: item.label || item.name || item.url, url: item.url });
  }
  const attachments = Array.isArray(metadata.attachments) ? metadata.attachments : [];
  for (const item of attachments) {
    const url = item?.url || item?.href || item?.file_url;
    if (url) links.push({ label: item.label || item.name || item.filename || url, url });
  }
  const singleUrl = metadata.url || metadata.href || metadata.file_url;
  if (singleUrl) links.push({ label: metadata.label || metadata.filename || metadata.name || 'Open link', url: singleUrl });
  return links;
}

export function AgencyMessageAvatar({ message, agentStatus, onAgentClick }: AgencyMessageAvatarProps) {
  return (
    <div className="relative shrink-0">
      <button
        type="button"
        className="mono flex h-8 w-8 items-center justify-center rounded-lg text-[11px] transition-colors"
        style={avatarStyles(message)}
        onClick={message.isAgent && onAgentClick ? () => onAgentClick(message.author) : undefined}
        aria-label={message.isAgent ? `View agent: ${message.author}` : `Avatar for ${message.displayAuthor}`}
        tabIndex={message.isAgent && onAgentClick ? 0 : -1}
      >
        {message.isAgent ? <Bot size={14} strokeWidth={1.7} /> : initials(message)}
      </button>
      {message.isAgent && (
        <span
          aria-hidden="true"
          className="absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full"
          style={{ background: statusColor(agentStatus), border: '2px solid var(--warm)' }}
        />
      )}
    </div>
  );
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
  const parsedContent = splitInlineToolNoise(message.content);

  const handleViewReport = useCallback(async () => {
    const artifactId = message.metadata?.task_id || message.metadata?.attachment_id;
    const agent = message.metadata?.agent;
    if (!artifactId || !agent) return;
    setReportOpen(true);
    if (reportContent !== null) return;
    setReportLoading(true);
    try {
      const resp = await authenticatedFetch(api.agents.resultUrl(agent, artifactId));
      const text = await resp.text();
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
  const toolCalls = [
    ...(Array.isArray(message.metadata?.tool_calls) ? message.metadata.tool_calls : []),
    ...parsedContent.toolCalls,
  ];
  const links = metadataLinks(message.metadata);
  const isToolOnly = message.metadata?.kind === 'tool' || message.content.startsWith('→ ');

  if (message.isError) {
    return (
      <div
        className="flex items-center gap-2 rounded-xl px-3 py-2 text-sm"
        style={{ border: '0.5px solid rgba(214, 75, 75, 0.28)', background: 'rgba(214, 75, 75, 0.06)', color: '#9b2c2c' }}
      >
        <span className="shrink-0">!</span>
        <span>{message.content}</span>
        <span className="ml-auto shrink-0 text-xs opacity-60">{message.timestamp}</span>
      </div>
    );
  }

  if (isToolOnly) {
    return (
      <div style={{ marginLeft: 44 }}>
        <div
          className="mono inline-flex items-center gap-2"
          style={{ padding: '5px 10px', background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 6, fontSize: 11, color: 'var(--ink-mid)' }}
        >
          <Terminal size={11} />
          <span>{message.content}</span>
        </div>
      </div>
    );
  }

  return (
    <div className={cn('group relative flex gap-3', message.id.startsWith('optimistic-') && 'opacity-60')}>
      <AgencyMessageAvatar message={message} agentStatus={agentStatus} onAgentClick={onAgentClick} />

      <div className="min-w-0 flex-1">
        {message.parentId && (
          <div className="mb-1 text-xs" style={{ color: 'var(--ink-faint)' }}>
            In thread
          </div>
        )}
        <div className="mb-[3px] flex flex-wrap items-baseline gap-2">
          {message.isAgent && onAgentClick ? (
            <button
              type="button"
              className="mono border-0 bg-transparent p-0 text-sm transition-colors"
              style={{ color: 'var(--ink)' }}
              onClick={() => onAgentClick(message.author)}
              aria-label={`View agent: ${message.author}`}
            >
              {message.displayAuthor}
            </button>
          ) : (
            <code className="mono text-sm" style={{ color: 'var(--ink)' }}>
              {message.displayAuthor}
            </code>
          )}
          <span className="mono text-[10px]" style={{ color: 'var(--ink-faint)' }}>{message.timestamp}</span>
          <MessageFlagBadge flag={message.flag} />
        </div>

        {editing ? (
          <EditableMessage
            message={message}
            onSave={handleSaveEdit}
            onCancel={() => setEditing(false)}
          />
        ) : message.isAgent ? (
          <StructuredOutput content={parsedContent.cleanContent || message.content} metadata={message.metadata} />
        ) : (
          <div className="text-sm leading-[1.55] text-foreground/90 prose prose-gray dark:prose-invert prose-sm max-w-none break-words prose-a:break-all prose-p:my-0 prose-table:text-xs prose-th:px-2 prose-th:py-1 prose-td:px-2 prose-td:py-1 prose-pre:bg-card prose-pre:text-xs">
            <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents} allowedElements={ALLOWED_ELEMENTS} unwrapDisallowed>{parsedContent.cleanContent || message.content}</ReactMarkdown>
          </div>
        )}

        {toolCalls.length > 0 && (
          <div className="mt-2 space-y-1">
            {toolCalls.map((call: any, i: number) => (
              <ToolCallCard key={i} call={call} agent={message.author} />
            ))}
          </div>
        )}

        {links.length > 0 && (
          <div className="mt-3 flex flex-wrap items-center gap-2">
            {links.map((link, i) => (
              <a
                key={`${link.url}-${i}`}
                href={link.url}
                target="_blank"
                rel="noreferrer"
                className="inline-flex max-w-full items-center gap-1.5 rounded-full px-3 py-1.5 text-xs transition-colors"
                style={{ border: '0.5px solid var(--ink-hairline)', background: 'var(--warm-2)', color: 'var(--ink)' }}
              >
                <ExternalLink className="h-3 w-3 shrink-0" />
                <span className="truncate">{link.label}</span>
              </a>
            ))}
          </div>
        )}

        {message.metadata?.has_artifact && message.metadata?.agent && (message.metadata?.task_id || message.metadata?.attachment_id) && (
          <div className="mt-3 flex items-center gap-2">
            <button
              onClick={handleViewReport}
              className="inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-xs transition-colors"
              style={{ border: '0.5px solid var(--ink-hairline)', background: 'var(--warm-2)', color: 'var(--ink)' }}
            >
              <FileText className="h-3 w-3" />
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
              className="inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-xs transition-colors"
              style={{ border: '0.5px solid var(--ink-hairline)', background: 'transparent', color: 'var(--ink-muted)' }}
            >
              <Download className="h-3 w-3" />
              Download .md
            </button>
            <Dialog open={reportOpen} onOpenChange={setReportOpen}>
              <DialogContent className="max-h-[80vh] max-w-2xl overflow-y-auto bg-card">
                <DialogHeader>
                  <DialogTitle className="text-sm font-medium">
                    Report - {message.metadata.agent}
                  </DialogTitle>
                </DialogHeader>
                {reportLoading ? (
                  <div className="py-8 text-center text-sm text-muted-foreground">Loading...</div>
                ) : (
                  <div className="prose prose-gray dark:prose-invert prose-sm max-w-none prose-p:my-1 prose-pre:bg-secondary prose-pre:text-xs">
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>{reportContent || ''}</ReactMarkdown>
                  </div>
                )}
              </DialogContent>
            </Dialog>
          </div>
        )}

        {groupedReactions.length > 0 && (
          <div className="mt-2 flex flex-wrap gap-1">
            {groupedReactions.map(({ emoji, count, hasReacted }) => (
              <button
                key={emoji}
                onClick={() => hasReacted ? onUnreact?.(message, emoji) : onReact?.(message, emoji)}
                aria-label={hasReacted ? `Remove ${emoji} reaction (${count})` : `Add ${emoji} reaction (${count})`}
                aria-pressed={hasReacted}
                className="inline-flex items-center gap-1 rounded-full border px-1.5 py-0.5 text-xs"
                style={{
                  borderColor: hasReacted ? 'rgba(0, 153, 121, 0.38)' : 'var(--ink-hairline)',
                  background: hasReacted ? 'rgba(0, 153, 121, 0.08)' : 'var(--warm-2)',
                }}
              >
                {emoji} <span>{count}</span>
              </button>
            ))}
          </div>
        )}
      </div>

      {!editing && showReplyButton && (
        <div className="absolute right-0 top-0 z-10 -translate-y-1/2 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100">
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
