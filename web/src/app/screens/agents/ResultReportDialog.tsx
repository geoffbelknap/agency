import { useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { api, authenticatedFetch } from '../../lib/api';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '../../components/ui/dialog';

function stripFrontmatter(markdown: string): string {
  return markdown.replace(/^---[\s\S]*?---\s*/, '');
}

export function useResultReport(agentName: string) {
  const [openTask, setOpenTask] = useState('');
  const [reportContent, setReportContent] = useState('');
  const [reportLoading, setReportLoading] = useState(false);

  async function openReport(taskID: string) {
    setOpenTask(taskID);
    setReportContent('');
    setReportLoading(true);
    try {
      const resp = await authenticatedFetch(api.agents.resultUrl(agentName, taskID));
      const text = await resp.text();
      setReportContent(stripFrontmatter(text));
    } catch {
      setReportContent('_Failed to load report._');
    } finally {
      setReportLoading(false);
    }
  }

  return {
    openTask,
    reportContent,
    reportLoading,
    openReport,
    closeReport: () => setOpenTask(''),
  };
}

export function ResultReportDialog({
  openTask,
  reportContent,
  reportLoading,
  onClose,
}: {
  openTask: string;
  reportContent: string;
  reportLoading: boolean;
  onClose: () => void;
}) {
  return (
    <Dialog open={!!openTask} onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="max-h-[80vh] max-w-2xl overflow-y-auto bg-card">
        <DialogHeader>
          <DialogTitle className="text-sm font-medium">Result - {openTask}</DialogTitle>
        </DialogHeader>
        {reportLoading ? (
          <div className="py-8 text-center text-sm text-muted-foreground">Loading...</div>
        ) : (
          <div className="prose prose-gray dark:prose-invert prose-sm max-w-none prose-p:my-1 prose-pre:bg-secondary prose-pre:text-xs">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{reportContent}</ReactMarkdown>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
