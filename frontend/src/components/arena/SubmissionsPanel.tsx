import React from 'react';
import { SubmissionSnapshot } from '../../types/api';
import { CheckCircle2, XCircle, Clock, AlertTriangle, Cpu } from 'lucide-react';

interface SubmissionsPanelProps {
  submissions: SubmissionSnapshot[];
  totalTests: number;
}

export const SubmissionsPanel: React.FC<SubmissionsPanelProps> = ({
  submissions,
  totalTests,
}) => {
  const getVerdictBadge = (sub: SubmissionSnapshot) => {
    if (sub.status === 'pending' || sub.status === 'running') {
      return (
        <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full bg-block-cream text-neutral-800 text-xs font-mono font-medium border border-hairline">
          <svg className="animate-spin h-3.5 w-3.5 text-neutral-700" viewBox="0 0 24 24" fill="none">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
          </svg>
          Judging Sandbox...
        </span>
      );
    }

    switch (sub.verdict) {
      case 'pass':
        return (
          <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full bg-block-mint text-ink text-xs font-mono font-bold border border-semantic-success/30 shadow-sm">
            <CheckCircle2 className="w-3.5 h-3.5 text-semantic-success" />
            ACCEPTED ({sub.tests_passed}/{totalTests})
          </span>
        );
      case 'fail':
        return (
          <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full bg-block-pink text-red-900 text-xs font-mono font-bold border border-red-300">
            <XCircle className="w-3.5 h-3.5 text-red-600" />
            FAILED ({sub.tests_passed}/{totalTests})
          </span>
        );
      case 'timeout':
        return (
          <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full bg-block-pink text-red-900 text-xs font-mono font-bold border border-red-300">
            <Clock className="w-3.5 h-3.5 text-red-600" />
            TIME LIMIT EXCEEDED
          </span>
        );
      case 'error':
      default:
        return (
          <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full bg-block-pink text-red-900 text-xs font-mono font-bold border border-red-300">
            <AlertTriangle className="w-3.5 h-3.5 text-red-600" />
            ERROR
          </span>
        );
    }
  };

  const getFailureLabel = (kind: string | null) => {
    switch (kind) {
      case 'wrong_answer':
        return 'Wrong Answer: Output did not match expected solution.';
      case 'compile_error':
        return 'Compilation Error: Failed to compile in runner sandbox.';
      case 'runtime_error':
        return 'Runtime Exception / Non-zero exit code.';
      case 'output_limit':
        return 'Output limit exceeded (too much stdout produced).';
      case 'infrastructure_error':
        return 'Infrastructure / Sandbox execution fault.';
      default:
        return null;
    }
  };

  return (
    <div className="flex flex-col h-full rounded-[24px] overflow-hidden border border-hairline bg-canvas shadow-sm">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-hairline bg-surface-soft">
        <div className="flex items-center gap-2">
          <Cpu className="w-4 h-4 text-ink" />
          <h3 className="font-card-title text-base text-ink">
            Submission History
          </h3>
        </div>
        <span className="text-xs font-mono text-neutral-500">
          {submissions.length} attempt{submissions.length === 1 ? '' : 's'}
        </span>
      </div>

      {/* Submissions List */}
      <div className="flex-1 overflow-y-auto p-4 md:p-6">
        {submissions.length === 0 ? (
          <div className="h-48 flex flex-col items-center justify-center text-center p-6 border-2 border-dashed border-hairline rounded-[18px]">
            <Cpu className="w-8 h-8 text-neutral-300 mb-2" />
            <p className="font-mono text-xs uppercase tracking-caption text-neutral-400 font-medium">
              No submissions yet
            </p>
            <p className="font-sans text-xs text-neutral-500 mt-1 max-w-xs">
              Write your solution and press "Submit Solution" (or ⌘/Ctrl+Enter) to evaluate in the Docker sandbox.
            </p>
          </div>
        ) : (
          <div className="space-y-3">
            {submissions.map((sub, idx) => {
              const failureDesc = getFailureLabel(sub.failure_kind);
              const timeStr = new Date(sub.created_at).toLocaleTimeString([], {
                hour: '2-digit',
                minute: '2-digit',
                second: '2-digit',
              });

              return (
                <div
                  key={sub.id || idx}
                  className="p-4 rounded-[16px] bg-canvas border border-hairline hover:border-black/20 transition-all space-y-2 shadow-xs"
                >
                  <div className="flex items-center justify-between gap-3">
                    <div className="flex items-center gap-2">
                      <span className="px-2.5 py-0.5 rounded-full bg-surface-soft text-neutral-700 text-xs font-mono font-medium uppercase border border-hairline">
                        {sub.language}
                      </span>
                      <span className="text-xs font-mono text-neutral-400">
                        {timeStr}
                      </span>
                    </div>

                    <div>{getVerdictBadge(sub)}</div>
                  </div>

                  {failureDesc && (
                    <div className="p-2.5 rounded-lg bg-block-pink/50 text-red-900 text-xs font-mono flex items-start gap-2">
                      <AlertTriangle className="w-3.5 h-3.5 text-red-600 flex-shrink-0 mt-0.5" />
                      <span>{failureDesc}</span>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
};
