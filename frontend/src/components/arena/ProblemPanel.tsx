import React, { useState } from 'react';
import { ProblemSnapshot } from '../../types/api';
import { Copy, Check, FileText, Terminal } from 'lucide-react';

interface ProblemPanelProps {
  problem: ProblemSnapshot;
}

export const ProblemPanel: React.FC<ProblemPanelProps> = ({ problem }) => {
  const [copiedIdx, setCopiedIdx] = useState<number | null>(null);

  // Sample test cases for the problem (derived from problem seed)
  const sampleCases = [
    {
      input: `4 9\n2 7 11 15`,
      output: `0 1`,
      explanation: 'nums[0] + nums[1] = 2 + 7 = 9, so return 0 1',
    },
    {
      input: `3 6\n3 2 4`,
      output: `1 2`,
      explanation: 'nums[1] + nums[2] = 2 + 4 = 6, so return 1 2',
    },
    {
      input: `2 6\n3 3`,
      output: `0 1`,
      explanation: 'nums[0] + nums[1] = 3 + 3 = 6, so return 0 1',
    },
  ];

  const handleCopy = (text: string, idx: number) => {
    navigator.clipboard.writeText(text);
    setCopiedIdx(idx);
    setTimeout(() => setCopiedIdx(null), 2000);
  };

  return (
    <div className="flex flex-col h-full rounded-[24px] overflow-hidden border border-hairline bg-canvas shadow-sm">
      {/* Title Bar */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-hairline bg-surface-soft">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 rounded-full bg-block-cream border border-black/10 flex items-center justify-center">
            <FileText className="w-4 h-4 text-ink" />
          </div>
          <h2 className="font-card-title text-xl text-ink">
            {problem.title}
          </h2>
        </div>

        <div className="flex items-center gap-2">
          <span className="px-3 py-1 rounded-full bg-block-mint/70 border border-semantic-success/20 text-xs font-mono font-medium text-ink">
            {problem.total_tests} Total Tests
          </span>
          <span className="px-3 py-1 rounded-full bg-canvas border border-hairline text-xs font-mono text-neutral-600">
            LeetCode Classic
          </span>
        </div>
      </div>

      {/* Scrollable Problem Body */}
      <div className="flex-1 overflow-y-auto p-6 md:p-8 space-y-6">
        {/* Warm Editorial Sticky Note for Statement */}
        <div className="rounded-[18px] bg-block-cream p-6 border border-black/5">
          <div className="font-mono text-xs uppercase tracking-caption font-bold text-neutral-700 mb-2">
            Problem Description
          </div>
          <p className="font-sans text-base text-ink font-normal leading-relaxed whitespace-pre-line">
            {problem.statement}
          </p>
        </div>

        {/* Input/Output Specifications */}
        <div className="space-y-4">
          <div className="flex items-center gap-2 font-mono text-xs uppercase tracking-caption font-bold text-neutral-800">
            <Terminal className="w-4 h-4 text-ink" />
            Standard I/O Contract (Docker Sandbox)
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="p-4 rounded-[14px] bg-surface-soft border border-hairline">
              <span className="font-mono text-xs font-semibold text-neutral-800 block mb-1">
                Standard Input (stdin):
              </span>
              <ul className="text-xs font-mono text-neutral-600 space-y-1">
                <li>• Line 1: <code className="bg-canvas px-1 py-0.5 rounded border border-hairline">n target</code></li>
                <li>• Line 2: <code className="bg-canvas px-1 py-0.5 rounded border border-hairline">n space-separated integers</code></li>
              </ul>
            </div>

            <div className="p-4 rounded-[14px] bg-surface-soft border border-hairline">
              <span className="font-mono text-xs font-semibold text-neutral-800 block mb-1">
                Standard Output (stdout):
              </span>
              <ul className="text-xs font-mono text-neutral-600 space-y-1">
                <li>• Two space-separated indices in ascending order (e.g. <code className="bg-canvas px-1 py-0.5 rounded border border-hairline">0 1</code>)</li>
              </ul>
            </div>
          </div>
        </div>

        {/* Sample Test Cases */}
        <div className="space-y-4 pt-2">
          <div className="font-mono text-xs uppercase tracking-caption font-bold text-neutral-800">
            Sample Cases
          </div>

          <div className="space-y-3">
            {sampleCases.map((sc, idx) => (
              <div
                key={idx}
                className="p-4 rounded-[14px] bg-canvas border border-hairline space-y-3"
              >
                <div className="flex items-center justify-between">
                  <span className="text-xs font-mono font-bold text-neutral-700">
                    Example {idx + 1}
                  </span>
                  <button
                    onClick={() => handleCopy(sc.input, idx)}
                    className="flex items-center gap-1.5 text-xs font-mono text-neutral-500 hover:text-ink transition-colors px-2 py-1 rounded-md hover:bg-surface-soft"
                    title="Copy sample input"
                  >
                    {copiedIdx === idx ? (
                      <>
                        <Check className="w-3.5 h-3.5 text-semantic-success" />
                        <span className="text-semantic-success">Copied!</span>
                      </>
                    ) : (
                      <>
                        <Copy className="w-3.5 h-3.5" />
                        <span>Copy Input</span>
                      </>
                    )}
                  </button>
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs font-mono">
                  <div className="p-2.5 rounded-lg bg-surface-soft border border-hairline-soft">
                    <span className="text-[10px] text-neutral-400 block mb-1 uppercase tracking-caption">
                      Input:
                    </span>
                    <pre className="text-ink font-semibold whitespace-pre-line">{sc.input}</pre>
                  </div>
                  <div className="p-2.5 rounded-lg bg-surface-soft border border-hairline-soft">
                    <span className="text-[10px] text-neutral-400 block mb-1 uppercase tracking-caption">
                      Expected Output:
                    </span>
                    <pre className="text-ink font-semibold">{sc.output}</pre>
                  </div>
                </div>

                {sc.explanation && (
                  <p className="text-xs font-sans text-neutral-500 italic">
                    {sc.explanation}
                  </p>
                )}
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
};
