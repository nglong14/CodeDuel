import React, { useEffect } from 'react';
import confetti from 'canvas-confetti';
import { Trophy, Frown, Equal, ArrowRight, RotateCcw } from 'lucide-react';
import { MatchSnapshot } from '../../types/api';
import { Button } from '../common/Button';
import { Modal } from '../common/Modal';

interface MatchEndModalProps {
  isOpen: boolean;
  onClose: () => void;
  match: MatchSnapshot;
  currentUserId: string;
  onPlayAgain: () => void;
}

export const MatchEndModal: React.FC<MatchEndModalProps> = ({
  isOpen,
  onClose,
  match,
  currentUserId,
  onPlayAgain,
}) => {
  const isWinner = match.winner_id === currentUserId || match.outcome === 'win';
  const isDraw = !match.winner_id || match.outcome === 'draw';
  const isLoss = !isWinner && !isDraw;

  useEffect(() => {
    if (isOpen && isWinner) {
      try {
        confetti({
          particleCount: 120,
          spread: 70,
          origin: { y: 0.6 },
          colors: ['#dceeb1', '#c5b0f4', '#c8e6cd', '#ff3d8b', '#000000'],
        });
      } catch (err) {
        // Confetti fallback
      }
    }
  }, [isOpen, isWinner]);

  if (!isOpen) return null;

  const winnerPlayer = match.players.find((p) => p.id === match.winner_id);

  return (
    <Modal isOpen={isOpen} onClose={onClose} maxWidth="max-w-lg">
      <div className="text-center py-4 space-y-6">
        {/* Banner based on outcome */}
        {isWinner && (
          <div className="p-8 rounded-[24px] bg-block-mint text-ink border border-semantic-success/30 shadow-md">
            <div className="w-16 h-16 rounded-full bg-canvas border border-semantic-success/40 mx-auto flex items-center justify-center mb-4 shadow-sm">
              <Trophy className="w-8 h-8 text-semantic-success" />
            </div>
            <span className="font-mono text-xs uppercase tracking-caption font-bold text-semantic-success block mb-1">
              Match Verdict
            </span>
            <h2 className="font-display-lg text-3xl md:text-4xl text-ink font-normal tracking-tight mb-2">
              VICTORY!
            </h2>
            <p className="font-sans text-sm text-neutral-700 max-w-sm mx-auto">
              You submitted the first correct full-pass solution and claimed the victory.
            </p>
          </div>
        )}

        {isLoss && (
          <div className="p-8 rounded-[24px] bg-block-pink text-ink border border-red-300 shadow-md">
            <div className="w-16 h-16 rounded-full bg-canvas border border-red-300 mx-auto flex items-center justify-center mb-4 shadow-sm">
              <Frown className="w-8 h-8 text-red-600" />
            </div>
            <span className="font-mono text-xs uppercase tracking-caption font-bold text-red-600 block mb-1">
              Match Verdict
            </span>
            <h2 className="font-display-lg text-3xl md:text-4xl text-ink font-normal tracking-tight mb-2">
              DEFEAT
            </h2>
            <p className="font-sans text-sm text-neutral-700 max-w-sm mx-auto">
              Your opponent ({winnerPlayer?.display_name || 'Opponent'}) successfully completed all tests first.
            </p>
          </div>
        )}

        {isDraw && (
          <div className="p-8 rounded-[24px] bg-block-cream text-ink border border-amber-300 shadow-md">
            <div className="w-16 h-16 rounded-full bg-canvas border border-amber-300 mx-auto flex items-center justify-center mb-4 shadow-sm">
              <Equal className="w-8 h-8 text-amber-700" />
            </div>
            <span className="font-mono text-xs uppercase tracking-caption font-bold text-amber-700 block mb-1">
              Match Verdict
            </span>
            <h2 className="font-display-lg text-3xl md:text-4xl text-ink font-normal tracking-tight mb-2">
              DRAW
            </h2>
            <p className="font-sans text-sm text-neutral-700 max-w-sm mx-auto">
              The duel deadline expired without a full-pass winner. Both players tied.
            </p>
          </div>
        )}

        {/* Match Statistics summary */}
        <div className="p-4 rounded-[16px] bg-surface-soft border border-hairline grid grid-cols-2 gap-4 text-left">
          <div>
            <span className="text-[11px] font-mono uppercase tracking-caption text-neutral-500 block">
              Problem
            </span>
            <span className="font-sans text-sm font-semibold text-ink">
              {match.problem.title}
            </span>
          </div>
          <div>
            <span className="text-[11px] font-mono uppercase tracking-caption text-neutral-500 block">
              Test Cases
            </span>
            <span className="font-sans text-sm font-semibold text-ink">
              {match.problem.total_tests} Tests
            </span>
          </div>
        </div>

        {/* Action pills */}
        <div className="flex flex-col sm:flex-row items-center justify-center gap-3 pt-2">
          <Button
            variant="secondary"
            onClick={onClose}
            className="w-full sm:w-auto px-6 py-2.5"
          >
            Review Code
          </Button>
          <Button
            variant="primary"
            onClick={onPlayAgain}
            icon={<ArrowRight className="w-4 h-4" />}
            className="w-full sm:w-auto px-6 py-2.5"
          >
            Find Another Duel
          </Button>
        </div>
      </div>
    </Modal>
  );
};
