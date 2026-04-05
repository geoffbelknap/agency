import { Agent } from '../../types';
import { Button } from '../../components/ui/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '../../components/ui/select';
import { JsonView } from '../../components/JsonView';

interface PolicyTabProps {
  agents: Agent[];
  policyAgent: string;
  onPolicyAgentChange: (agent: string) => void;
  policyData: any;
  policyLoading: boolean;
  policyError: string | null;
  onValidate: () => void;
  validating: boolean;
}

export function PolicyTab({
  agents,
  policyAgent,
  onPolicyAgentChange,
  policyData,
  policyLoading,
  policyError,
  onValidate,
  validating,
}: PolicyTabProps) {
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3 md:gap-4">
        <Select value={policyAgent} onValueChange={onPolicyAgentChange}>
          <SelectTrigger className="w-full sm:w-64 bg-card border-border">
            <SelectValue placeholder="Select agent..." />
          </SelectTrigger>
          <SelectContent>
            {agents.map((agent) => (
              <SelectItem key={agent.id} value={agent.name}>
                {agent.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          variant="outline"
          size="sm"
          onClick={onValidate}
          disabled={!policyAgent || validating}
        >
          {validating ? 'Validating...' : 'Validate'}
        </Button>
      </div>

      {policyError && (
        <div className="text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
          {policyError}
        </div>
      )}

      {policyLoading ? (
        <div className="text-sm text-muted-foreground text-center py-8">Loading policy...</div>
      ) : !policyAgent ? (
        <div className="text-sm text-muted-foreground text-center py-8">Select an agent to view policy</div>
      ) : !policyData ? (
        <div className="text-sm text-muted-foreground text-center py-8">No policy data</div>
      ) : (
        <JsonView data={policyData} />
      )}
    </div>
  );
}
