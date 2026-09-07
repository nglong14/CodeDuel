import React, { useEffect, useState } from 'react';
import { Swords, ExternalLink, ShieldAlert, Sparkles, Terminal, CheckCircle2, X } from 'lucide-react';
import { useAuth } from '../../context/AuthContext';
import { useWS } from '../../context/WebSocketContext';
import { Button } from '../common/Button';

interface MatchmakingModalProps {
  isOpen: boolean;
  onClose: () => void;
  onMatchFound: (matchId: string) => void;
}

export const MatchmakingModal: React.FC<MatchmakingModalProps> = ({
  isOpen,
  onClose,
  onMatchFound,
}) => {
  const { user } = useAuth();
  const { isQueued, latestMatchStart, latestError, clearQueueState } = useWS();
  const [elapsed, setElapsed] = useState(0);
  const [matchFoundAnim, setMatchFoundAnim] = useState(false);

  useEffect(() => {
    let timer: ReturnType<typeof setInterval> | null = null;
    if (isOpen) {
      setElapsed(0);
      timer = setInterval(() => {
        setElapsed((prev) => prev + 1);
      }, 1000);
    } else {
      setElapsed(0);
    }
    return () => {
      if (timer) clearInterval(timer);
    };
  }, [isOpen]);

  // When match_start arrives, show celebration and transition!
  useEffect(() => {
    if (isOpen && latestMatchStart) {
      setMatchFoundAnim(true);
      const timeout = setTimeout(() => {
        onMatchFound(latestMatchStart.match_id);
        onClose();
        setMatchFoundAnim(false);
      }, 800);
      return () => clearTimeout(timeout);
    }
  }, [isOpen, latestMatchStart, onMatchFound, onClose]);

  if (!isOpen) return null;

  const formatElapsed = (sec: number) => {
    const mins = Math.floor(sec / 60);
    const s = sec % 60;
    return `${mins.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  };

  const handleCancel = () => {
    clearQueueState();
    onClose();
  };

  const openOpponentTab = () => {
    const opponent = user?.display_name?.toLowerCase().includes('bob') ? 'alice' : 'bob';
    window.open(`${window.location.origin}/?player=${opponent}`, '_blank');
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      {/* Dark Scrim Backdrop */}
      <div
        className="fixed inset-0 bg-black/60 backdrop-blur-xs transition-opacity animate-fadeIn"
        onClick={handleCancel}
      />

      {/* Main Waiting Screen Card */}
      <div className="relative w-full max-w-xl bg-canvas rounded-[24px] border border-hairline shadow-2xl overflow-hidden z-10 animate-fadeIn">
        {/* Top bar with close */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-hairline bg-surface-soft">
          <div className="flex items-center gap-2">
            <div className="w-6 h-6 rounded-full bg-primary flex items-center justify-center text-white">
              <Swords className="w-3 h-3 text-block-lime" />
            </div>
            <span className="font-mono text-xs uppercase tracking-caption font-bold text-neutral-800">
              CodeDuel Matchmaking Pool
            </span>
          </div>

          <button
            onClick={handleCancel}
            className="w-8 h-8 rounded-full flex items-center justify-center hover:bg-hairline text-neutral-500 hover:text-ink transition-colors"
            aria-label="Close"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Modal Body */}
        <div className="p-6 md:p-8 space-y-6">
          {matchFoundAnim ? (
            /* Match Found Transition State */
            <div className="p-8 rounded-[20px] bg-block-mint text-center space-y-4 animate-fadeIn border border-semantic-success/30">
              <div className="w-16 h-16 rounded-full bg-canvas border border-semantic-success/40 mx-auto flex items-center justify-center shadow-md">
                <CheckCircle2 className="w-8 h-8 text-semantic-success animate-bounce" />
              </div>
              <span className="font-mono text-xs uppercase tracking-caption font-bold text-semantic-success">
                Opponent Matched!
              </span>
              <h2 className="font-display-lg text-3xl md:text-4xl text-ink font-normal tracking-tight">
                Entering Duel Arena...
              </h2>
              <p className="font-sans text-xs text-neutral-700">
                Match ID: <code className="font-mono">{latestMatchStart?.match_id.slice(0, 8)}</code>
              </p>
            </div>
          ) : (
            /* Searching Queue State */
            <>
              {/* Radar & Timer Centerpiece */}
              <div className="p-8 rounded-[20px] bg-block-lilac text-center relative overflow-hidden border border-purple-200">
                {/* Radar Waves Animation */}
                <div className="relative w-28 h-28 mx-auto flex items-center justify-center my-2">
                  <div className="absolute inset-0 rounded-full border-2 border-black/15 animate-ping opacity-60" />
                  <div className="absolute inset-2 rounded-full border-2 border-black/10 animate-pulse" />
                  <div className="w-16 h-16 rounded-full bg-canvas flex items-center justify-center shadow-md">
                    <Swords className="w-8 h-8 text-ink animate-bounce" />
                  </div>
                </div>

                <div className="mt-4 space-y-1">
                  <div className="font-mono text-xs uppercase tracking-caption font-bold text-neutral-800 flex items-center justify-center gap-2">
                    <span className="w-2 h-2 rounded-full bg-accent-magenta animate-ping" />
                    Searching for Opponent
                  </div>
                  <div className="font-mono text-4xl font-bold text-ink tracking-tight">
                    {formatElapsed(elapsed)}
                  </div>
                  <p className="font-sans text-xs text-neutral-700 max-w-sm mx-auto pt-1">
                    Waiting in Redis FIFO pool. The Lua <code className="bg-canvas/70 px-1 py-0.5 rounded font-mono text-[11px]">pop_pair</code> script will atomically pair you with the next available player.
                  </p>
                </div>
              </div>

              {/* Player Status Tag */}
              <div className="flex items-center justify-between p-3.5 rounded-[14px] bg-surface-soft border border-hairline text-xs font-mono">
                <div className="flex items-center gap-2.5">
                  <div className="w-7 h-7 rounded-full bg-block-lime flex items-center justify-center font-bold text-ink text-xs">
                    {user?.display_name?.charAt(0).toUpperCase() || 'P'}
                  </div>
                  <div className="font-sans">
                    <span className="font-semibold text-ink block">{user?.display_name}</span>
                    <span className="text-[11px] text-neutral-500 font-mono">Status: Ready in Queue</span>
                  </div>
                </div>

                <div className="flex items-center gap-1 text-semantic-success font-semibold">
                  <span className="w-2 h-2 rounded-full bg-semantic-success animate-pulse" />
                  <span>Enqueued</span>
                </div>
              </div>

              {/* Error notice if any */}
              {latestError && (
                <div className="p-3 rounded-lg bg-block-pink text-red-900 text-xs font-mono flex items-center gap-2">
                  <ShieldAlert className="w-4 h-4 text-red-600 flex-shrink-0" />
                  <span>{latestError.message}</span>
                </div>
              )}

              {/* Instant Test Opponent Helper */}
              <div className="p-4 rounded-[16px] bg-canvas border border-dashed border-hairline space-y-2">
                <div className="flex items-center justify-between">
                  <span className="font-mono text-xs uppercase tracking-caption font-bold text-neutral-700 flex items-center gap-1.5">
                    <Sparkles className="w-3.5 h-3.5 text-accent-magenta" />
                    Testing Locally?
                  </span>
                  <span className="text-[11px] font-mono text-neutral-400">Need Player 2</span>
                </div>
                <p className="font-sans text-xs text-neutral-600">
                  Open an opponent window to trigger the 1v1 match pair immediately:
                </p>
                <Button
                  type="button"
                  variant="secondary"
                  onClick={openOpponentTab}
                  icon={<ExternalLink className="w-3.5 h-3.5" />}
                  className="w-full text-xs py-2 h-9"
                >
                  Launch Opponent in New Tab
                </Button>
              </div>

              {/* Bottom Actions */}
              <div className="flex items-center justify-between pt-2">
                <Button
                  variant="secondary"
                  onClick={handleCancel}
                  className="w-full text-sm py-2.5"
                >
                  Cancel Matchmaking
                </Button>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
};
