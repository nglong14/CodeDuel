import React, { useState } from 'react';
import { ExternalLink, Users, Terminal, Copy, Check, Sparkles } from 'lucide-react';
import { useAuth } from '../../context/AuthContext';
import { Button } from '../common/Button';

export const DualPlaySimulator: React.FC = () => {
  const { user, quickLoginDemo } = useAuth();
  const [copied, setCopied] = useState(false);

  const cliCommand = 'make run-cli USER_ID=22222222-2222-2222-2222-222222222222';

  const handleCopyCLI = () => {
    navigator.clipboard.writeText(cliCommand);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const openOpponentWindow = () => {
    // Open a new browser window/tab
    window.open(window.location.origin + '?player=bob', '_blank');
  };

  return (
    <div className="rounded-[24px] border border-hairline bg-surface-soft p-6 md:p-8 space-y-6">
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-full bg-block-mint border border-semantic-success/30 flex items-center justify-center">
            <Users className="w-5 h-5 text-ink" />
          </div>
          <div>
            <h3 className="font-card-title text-lg text-ink">
              Multiplayer Local Testing Tools
            </h3>
            <p className="font-sans text-xs text-neutral-600">
              Need a second player to test the 1v1 duel match flow?
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <span className="px-3 py-1 rounded-full bg-canvas text-xs font-mono text-neutral-600 border border-hairline">
            Active: {user?.display_name || 'Guest'}
          </span>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* Method 1: Dual Browser Tabs */}
        <div className="p-5 rounded-[18px] bg-canvas border border-hairline space-y-3">
          <div className="flex items-center gap-2 font-mono text-xs uppercase tracking-caption font-bold text-neutral-800">
            <Sparkles className="w-4 h-4 text-accent-magenta" />
            Method 1: Two Browser Tabs
          </div>
          <p className="font-sans text-xs text-neutral-600 leading-relaxed">
            Open CodeDuel in a new tab. Sign in as <strong>Player 2 (Bob)</strong>, then hit "Find a 1v1 Match" on both tabs to watch the pair match instantly!
          </p>
          <div className="pt-1 flex gap-2">
            <Button
              variant="secondary"
              onClick={openOpponentWindow}
              icon={<ExternalLink className="w-3.5 h-3.5" />}
              className="text-xs py-2 h-9"
            >
              Open Second Window
            </Button>
            <Button
              variant="secondary"
              onClick={() => quickLoginDemo('player2')}
              className="text-xs py-2 h-9"
            >
              Switch to Bob Here
            </Button>
          </div>
        </div>

        {/* Method 2: CLI Terminal Client */}
        <div className="p-5 rounded-[18px] bg-canvas border border-hairline space-y-3">
          <div className="flex items-center gap-2 font-mono text-xs uppercase tracking-caption font-bold text-neutral-800">
            <Terminal className="w-4 h-4 text-ink" />
            Method 2: DuelCLI Terminal Client
          </div>
          <p className="font-sans text-xs text-neutral-600 leading-relaxed">
            Connect Bob through the built-in Go terminal client in your repo:
          </p>
          <div className="flex items-center justify-between p-2.5 rounded-lg bg-surface-soft border border-hairline-soft font-mono text-xs text-neutral-800">
            <code className="truncate mr-2">{cliCommand}</code>
            <button
              onClick={handleCopyCLI}
              className="p-1 hover:bg-hairline rounded transition-colors text-neutral-500 hover:text-ink flex-shrink-0"
              title="Copy command"
            >
              {copied ? <Check className="w-4 h-4 text-semantic-success" /> : <Copy className="w-4 h-4" />}
            </button>
          </div>
          <p className="text-[11px] font-sans text-neutral-500">
            Once connected in terminal, type <code className="font-mono bg-surface-soft px-1 py-0.5 rounded">join</code> to match against your browser!
          </p>
        </div>
      </div>
    </div>
  );
};
