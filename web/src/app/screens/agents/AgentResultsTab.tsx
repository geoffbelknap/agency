import { Download, FileText, RefreshCw } from 'lucide-react';
import { api, type RawAgentResult } from '../../lib/api';
import { PactStatusBadge, extractPactMetadata } from '../../components/PactStatusBadge';
import { ResultReportDialog, useResultReport } from './ResultReportDialog';

interface Props {
  agentName: string;
  results: RawAgentResult[];
  refreshingResults: boolean;
  refreshResults: (agentName: string) => Promise<void>;
}

function resultTimestamp(result: RawAgentResult): string {
  const raw = result.metadata?.timestamp;
  return typeof raw === 'string' && raw.trim() ? raw : '';
}

export function AgentResultsTab({ agentName, results, refreshingResults, refreshResults }: Props) {
  const report = useResultReport(agentName);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div style={{ background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 10, padding: 20 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12 }}>
          <div>
            <div className="eyebrow">Results</div>
            <div style={{ marginTop: 4, fontSize: 12, color: 'var(--ink-mid)' }}>Saved work artifacts and PACT verdicts</div>
          </div>
          <span className="font-mono" style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--ink-faint)' }}>{results.length} artifacts</span>
          <button
            type="button"
            onClick={() => void refreshResults(agentName)}
            disabled={refreshingResults}
            aria-label={refreshingResults ? 'Refreshing results' : 'Refresh results'}
            style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 30, height: 30, borderRadius: 999, border: '0.5px solid var(--ink-hairline-strong)', background: 'var(--warm)', color: 'var(--ink)', opacity: refreshingResults ? 0.55 : 1 }}
          >
            <RefreshCw size={13} className={refreshingResults ? 'animate-spin' : ''} />
          </button>
        </div>

        <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 8, overflow: 'hidden', background: 'var(--warm)' }}>
          {results.length === 0 ? (
            <div style={{ padding: 16, fontSize: 13, color: 'var(--ink-faint)' }}>No saved result artifacts for this agent yet.</div>
          ) : (
            results.map((result, index) => {
              const pact = extractPactMetadata(result as unknown as Record<string, unknown>);
              const timestamp = resultTimestamp(result);
              return (
                <div key={result.task_id} style={{ display: 'grid', gridTemplateColumns: 'minmax(150px, 1fr) minmax(130px, auto) auto', gap: 12, padding: 14, borderTop: index === 0 ? 0 : '0.5px solid var(--ink-hairline)', alignItems: 'center' }}>
                  <div style={{ minWidth: 0 }}>
                    <div className="font-mono" style={{ fontSize: 12, color: 'var(--ink)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{result.task_id}</div>
                    {timestamp && <div className="font-mono" style={{ marginTop: 4, fontSize: 10, color: 'var(--ink-faint)' }}>{timestamp}</div>}
                  </div>
                  <div style={{ display: 'flex', justifyContent: 'flex-start' }}>
                    <PactStatusBadge pact={pact} metadataError={result.metadata_error} />
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 6 }}>
                    <button
                      type="button"
                      onClick={() => void report.openReport(result.task_id)}
                      className="inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-xs"
                      style={{ border: '0.5px solid var(--ink-hairline)', background: 'var(--warm-2)', color: 'var(--ink)' }}
                    >
                      <FileText className="h-3 w-3" />
                      View
                    </button>
                    <a
                      href={api.agents.resultDownloadUrl(agentName, result.task_id)}
                      className="inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-xs"
                      style={{ border: '0.5px solid var(--ink-hairline)', background: 'transparent', color: 'var(--ink-muted)', textDecoration: 'none' }}
                    >
                      <Download className="h-3 w-3" />
                      Download
                    </a>
                  </div>
                </div>
              );
            })
          )}
        </div>
      </div>

      <ResultReportDialog
        openTask={report.openTask}
        reportContent={report.reportContent}
        reportLoading={report.reportLoading}
        onClose={report.closeReport}
      />
    </div>
  );
}
