import { useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Copy, Check, ChevronRight } from 'lucide-react';
import { Button } from '../ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../ui/collapsible';
import { cn } from '../ui/utils';

interface StructuredOutputProps {
  content: string;
  metadata?: Record<string, any>;
}

function CodeBlock({ language, code }: { language: string | null; code: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="my-2 rounded overflow-hidden border border-border">
      <div className="flex items-center justify-between px-3 py-1 bg-secondary border-b border-border">
        {language ? (
          <span className="text-xs text-muted-foreground">{language}</span>
        ) : (
          <span className="text-xs text-muted-foreground/70">code</span>
        )}
        <Button
          variant="ghost"
          size="sm"
          className="h-6 px-2 text-xs text-muted-foreground hover:text-foreground"
          onClick={handleCopy}
          aria-label="Copy code"
        >
          {copied ? (
            <Check className="w-3 h-3 mr-1" />
          ) : (
            <Copy className="w-3 h-3 mr-1" />
          )}
          {copied ? 'Copied' : 'Copy'}
        </Button>
      </div>
      <pre className="bg-card text-xs p-3 overflow-x-auto">
        <code>{code}</code>
      </pre>
    </div>
  );
}

function DetailsBlock({ summary, children }: { summary: string; children: React.ReactNode }) {
  const [open, setOpen] = useState(false);

  return (
    <Collapsible open={open} onOpenChange={setOpen} className="my-2 border border-border rounded">
      <CollapsibleTrigger className="flex items-center gap-1.5 w-full px-3 py-2 text-sm text-foreground/80 hover:text-foreground hover:bg-secondary/50 rounded-t transition-colors">
        <ChevronRight
          className={cn('w-3.5 h-3.5 text-muted-foreground transition-transform duration-200', {
            'rotate-90': open,
          })}
        />
        {summary}
      </CollapsibleTrigger>
      <CollapsibleContent className="px-3 py-2 text-sm text-foreground/80 border-t border-border">
        {children}
      </CollapsibleContent>
    </Collapsible>
  );
}

/**
 * Allowlist of HTML elements permitted in markdown rendering.
 * SECURITY: Do NOT add 'script', 'iframe', 'object', 'embed', 'form', or 'input'.
 * Do NOT add rehype-raw to the plugin list — it would bypass this allowlist.
 */
export const ALLOWED_ELEMENTS = [
  'p', 'strong', 'em', 'del', 'code', 'pre',
  'ul', 'ol', 'li', 'blockquote',
  'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
  'table', 'thead', 'tbody', 'tr', 'th', 'td',
  'a', 'br', 'hr', 'img',
];

export function StructuredOutput({ content, metadata: _metadata }: StructuredOutputProps) {
  const parts = parseDetailsBlocks(content);

  return (
    <div className="text-sm text-foreground/80 prose prose-gray dark:prose-invert prose-sm max-w-none break-words prose-p:my-1 prose-a:break-all">
      {parts.map((part, idx) => {
        if (part.type === 'details') {
          return (
            <DetailsBlock key={idx} summary={part.summary}>
              <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents} allowedElements={ALLOWED_ELEMENTS} unwrapDisallowed>
                {part.content}
              </ReactMarkdown>
            </DetailsBlock>
          );
        }
        return (
          <ReactMarkdown key={idx} remarkPlugins={[remarkGfm]} components={markdownComponents} allowedElements={ALLOWED_ELEMENTS} unwrapDisallowed>
            {part.text}
          </ReactMarkdown>
        );
      })}
    </div>
  );
}

type ContentPart =
  | { type: 'text'; text: string }
  | { type: 'details'; summary: string; content: string };

function parseDetailsBlocks(content: string): ContentPart[] {
  const parts: ContentPart[] = [];
  const detailsRegex = /<details>([\s\S]*?)<\/details>/gi;
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = detailsRegex.exec(content)) !== null) {
    if (match.index > lastIndex) {
      const text = content.slice(lastIndex, match.index);
      if (text.trim()) {
        parts.push({ type: 'text', text });
      }
    }

    const inner = match[1];
    const summaryMatch = inner.match(/<summary>([\s\S]*?)<\/summary>/i);
    const summary = summaryMatch ? summaryMatch[1].trim().replace(/<[^>]*>/g, '') : '';
    const innerContent = summaryMatch
      ? inner.replace(/<summary>[\s\S]*?<\/summary>/i, '').trim()
      : inner.trim();

    parts.push({ type: 'details', summary, content: innerContent });
    lastIndex = match.index + match[0].length;
  }

  if (lastIndex < content.length) {
    const text = content.slice(lastIndex);
    if (text.trim()) {
      parts.push({ type: 'text', text });
    }
  }

  if (parts.length === 0) {
    parts.push({ type: 'text', text: content });
  }

  return parts;
}

export const markdownComponents: React.ComponentProps<typeof ReactMarkdown>['components'] = {
  a({ href, children }) {
    if (href && (href.startsWith('http://') || href.startsWith('https://'))) {
      return (
        <a href={href} target="_blank" rel="noopener noreferrer">
          {children}
        </a>
      );
    }
    return <>{children}</>;
  },
  table({ children }) {
    return (
      <div className="overflow-x-auto my-2">
        <table className="text-xs border-collapse w-full border border-border">
          {children}
        </table>
      </div>
    );
  },
  th({ children }) {
    return (
      <th className="px-2 py-1 border border-border bg-secondary text-foreground text-left font-medium">
        {children}
      </th>
    );
  },
  td({ children }) {
    return (
      <td className="px-2 py-1 border border-border text-foreground/80">
        {children}
      </td>
    );
  },
  // Inline code — only `code` not inside a `pre` reaches this renderer when
  // we handle `pre` separately below.
  code({ className, children }: any) {
    return (
      <code className="bg-secondary px-1 py-0.5 rounded text-xs" data-inline="true">
        {children}
      </code>
    );
  },
  // Fenced code blocks: react-markdown wraps them in <pre><code>. We intercept
  // at the `pre` level so we can render the full CodeBlock card.
  pre({ children }: any) {
    // children is the <code> element rendered by the `code` renderer above.
    // Extract className and text from it.
    const child = Array.isArray(children) ? children[0] : children;
    if (child && child.props) {
      const className: string = child.props.className || '';
      const match = /language-(\w+)/.exec(className);
      const language = match ? match[1] : null;
      // child.props.children may be a string or array
      const codeText = String(child.props.children).replace(/\n$/, '');
      return <CodeBlock language={language} code={codeText} />;
    }
    // Fallback: just render a plain pre
    return <pre className="bg-card text-xs p-3 overflow-x-auto">{children}</pre>;
  },
};
