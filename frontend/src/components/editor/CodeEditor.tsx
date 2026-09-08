import React, { useState, useRef } from 'react';
import { Play, Code2, RotateCcw, AlertTriangle } from 'lucide-react';
import { SupportedLanguage } from '../../types/proto';
import { Button } from '../common/Button';

interface CodeEditorProps {
  language: SupportedLanguage;
  onLanguageChange: (lang: SupportedLanguage) => void;
  onSubmit: (code: string) => void;
  isSubmitting?: boolean;
  disabled?: boolean;
}

const STARTER_CODE: Record<SupportedLanguage, string> = {
  python: `import sys

def solve():
    # Read input from stdin
    lines = sys.stdin.read().split()
    if not lines:
        return
    n = int(lines[0])
    target = int(lines[1])
    nums = [int(x) for x in lines[2:2+n]]
    
    # Two Sum solution
    seen = {}
    for i, num in enumerate(nums):
        complement = target - num
        if complement in seen:
            print(f"{seen[complement]} {i}")
            return
        seen[num] = i

if __name__ == '__main__':
    solve()
`,
  cpp: `#include <iostream>
#include <vector>
#include <unordered_map>

using namespace std;

int main() {
    ios_base::sync_with_stdio(false);
    cin.tie(NULL);

    int n, target;
    if (!(cin >> n >> target)) return 0;

    vector<int> nums(n);
    for (int i = 0; i < n; ++i) {
        cin >> nums[i];
    }

    unordered_map<int, int> seen;
    for (int i = 0; i < n; ++i) {
        int complement = target - nums[i];
        if (seen.find(complement) != seen.end()) {
            cout << seen[complement] << " " << i << "\\n";
            return 0;
        }
        seen[nums[i]] = i;
    }

    return 0;
}
`,
  java: `import java.util.*;

public class Main {
    public static void main(String[] args) {
        Scanner scanner = new Scanner(System.in);
        if (!scanner.hasNextInt()) return;
        int n = scanner.nextInt();
        int target = scanner.nextInt();

        int[] nums = new int[n];
        for (int i = 0; i < n; i++) {
            nums[i] = scanner.nextInt();
        }

        Map<Integer, Integer> seen = new HashMap<>();
        for (int i = 0; i < n; i++) {
            int complement = target - nums[i];
            if (seen.containsKey(complement)) {
                System.out.println(seen.get(complement) + " " + i);
                return;
            }
            seen.put(nums[i], i);
        }
    }
}
`,
};

export const CodeEditor: React.FC<CodeEditorProps> = ({
  language,
  onLanguageChange,
  onSubmit,
  isSubmitting = false,
  disabled = false,
}) => {
  const [drafts, setDrafts] = useState<Record<SupportedLanguage, string>>(() => ({ ...STARTER_CODE }));
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const lineNumbersRef = useRef<HTMLDivElement>(null);

  const code = drafts[language];

  const lineCount = code.split('\n').length;
  const lineNumbers = Array.from({ length: Math.max(lineCount, 1) }, (_, i) => i + 1);
  const byteLength = new Blob([code]).size;
  const maxBytes = 64 * 1024; // 64KB max backend limit
  const canSubmit = !disabled && !isSubmitting && byteLength <= maxBytes && Boolean(code.trim());

  const setCode = (nextCode: string) => {
    setDrafts((current) => ({ ...current, [language]: nextCode }));
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // Cmd+Enter or Ctrl+Enter to submit
    if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
      e.preventDefault();
      if (canSubmit) {
        onSubmit(code);
      }
      return;
    }

    // Tab key inserts 4 spaces
    if (e.key === 'Tab') {
      e.preventDefault();
      const target = e.currentTarget;
      const start = target.selectionStart;
      const end = target.selectionEnd;
      const spaces = '    ';
      const newCode = code.substring(0, start) + spaces + code.substring(end);
      setCode(newCode);

      // Restore cursor position
      setTimeout(() => {
        target.selectionStart = target.selectionEnd = start + spaces.length;
      }, 0);
    }
  };

  const handleScroll = () => {
    if (textareaRef.current && lineNumbersRef.current) {
      lineNumbersRef.current.scrollTop = textareaRef.current.scrollTop;
    }
  };

  const resetTemplate = () => {
    setCode(STARTER_CODE[language]);
  };

  return (
    <div className="flex flex-col h-full rounded-[24px] overflow-hidden border border-hairline bg-canvas shadow-sm">
      {/* Editor Top Bar: Language Tabs and Controls */}
      <div className="flex flex-wrap items-center justify-between px-4 py-3 bg-surface-soft border-b border-hairline gap-3">
        {/* Language Tabs - pill design matching DESIGN.md pricing tabs */}
        <div className="flex items-center gap-1.5 p-1 bg-canvas rounded-full border border-hairline">
          {(['python', 'cpp', 'java'] as SupportedLanguage[]).map((lang) => {
            const isSelected = language === lang;
            const labels: Record<SupportedLanguage, string> = {
              python: 'Python 3.13',
              cpp: 'C++ (GCC 14)',
              java: 'Java 21',
            };
            return (
              <button
                key={lang}
                onClick={() => onLanguageChange(lang)}
                className={`px-3 py-1 text-xs font-mono rounded-full font-medium transition-all ${
                  isSelected
                    ? 'bg-primary text-on-primary shadow-sm'
                    : 'text-neutral-700 hover:text-ink hover:bg-surface-soft'
                }`}
              >
                {labels[lang]}
              </button>
            );
          })}
        </div>

        {/* Action controls */}
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={resetTemplate}
            title="Reset to starter template"
            className="flex items-center gap-1 text-xs font-mono text-neutral-500 hover:text-ink transition-colors px-2 py-1 rounded-md"
          >
            <RotateCcw className="w-3.5 h-3.5" />
            <span className="hidden sm:inline">Reset Code</span>
          </button>

          <span className="text-xs font-mono text-neutral-400">|</span>

          {/* Byte counter */}
          <div
            className={`text-xs font-mono ${
              byteLength > maxBytes
                ? 'text-red-600 font-bold flex items-center gap-1'
                : 'text-neutral-500'
            }`}
          >
            {byteLength > maxBytes && <AlertTriangle className="w-3.5 h-3.5" />}
            <span>
              {(byteLength / 1024).toFixed(1)} / 64 KB
            </span>
          </div>
        </div>
      </div>

      {/* Code Text Area with Line Numbers */}
      <div className="relative flex-1 flex bg-[#1e1e2e] text-[#cdd6f4] font-mono text-sm overflow-hidden min-h-[350px]">
        {/* Line Numbers */}
        <div
          ref={lineNumbersRef}
          className="w-12 py-4 select-none text-right pr-3 text-[#6c7086] border-r border-[#313244] bg-[#181825] overflow-hidden"
        >
          {lineNumbers.map((num) => (
            <div key={num} className="leading-6 text-xs">
              {num}
            </div>
          ))}
        </div>

        {/* Editable Area */}
        <textarea
          ref={textareaRef}
          value={code}
          onChange={(e) => setCode(e.target.value)}
          onKeyDown={handleKeyDown}
          onScroll={handleScroll}
          spellCheck={false}
          autoCapitalize="none"
          autoComplete="off"
          disabled={disabled}
          placeholder="Write your solution here..."
          className="flex-1 w-full h-full py-4 px-4 bg-transparent resize-none leading-6 outline-none font-mono text-sm text-[#cdd6f4] selection:bg-[#45475a] tab-4 focus:ring-0 border-none"
        />
      </div>

      {/* Editor Bottom Bar: Submit Action & Shortcuts */}
      <div className="flex items-center justify-between px-6 py-3 bg-surface-soft border-t border-hairline">
        <div className="flex items-center gap-2 text-xs font-mono text-neutral-500">
          <Code2 className="w-4 h-4 text-neutral-400" />
          <span className="hidden sm:inline">Shortcut:</span>
          <kbd className="px-1.5 py-0.5 rounded bg-canvas border border-hairline text-neutral-700 text-[10px]">
            ⌘/Ctrl + Enter
          </kbd>
        </div>

        <Button
          variant="primary"
          onClick={() => onSubmit(code)}
          isLoading={isSubmitting}
          disabled={!canSubmit}
          icon={<Play className="w-4 h-4 fill-current" />}
          className="px-6 py-2.5 text-sm"
        >
          {isSubmitting ? 'Evaluating...' : 'Submit Solution'}
        </Button>
      </div>
    </div>
  );
};
