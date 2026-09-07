import React, { useEffect, useState } from 'react';
import { Swords, Clock, User as UserIcon, CheckCircle2, Trophy } from 'lucide-react';
import { MatchSnapshot, PlayerSnapshot } from '../../types/api';

interface MatchHUDProps {
  match: MatchSnapshot;
  currentUserId: string;
}

export const MatchHUD: React.FC<MatchHUDProps> = ({ match, currentUserId }) => {
  const [secondsRemaining, setSecondsRemaining] = useState<number>(0);

  // Compute countdown timer accurately from server deadline
  useEffect(() => {
    const calculateTime = () => {
      const deadlineMs = new Date(match.deadline).getTime();
      const nowMs = Date.now();
      const diffSec = Math.max(0, Math.floor((deadlineMs - nowMs) / 1000));
      setSecondsRemaining(diffSec);
    };

    calculateTime();
    const timer = setInterval(calculateTime, 1000);
    return () => clearInterval(timer);
  }, [match.deadline]);

  const formatTime = (totalSec: number) => {
    const mins = Math.floor(totalSec / 60);
    const secs = totalSec % 60;
    return `${mins.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`;
  };

  const selfPlayer = match.players.find((p) => p.id === currentUserId) || match.players[0];
  const oppPlayer = match.players.find((p) => p.id !== currentUserId) || match.players[1];

  const isCritical = secondsRemaining <= 60 && match.status === 'active';

  return (
    <div className="w-full bg-canvas border border-hairline rounded-[24px] p-4 md:p-6 shadow-sm mb-6">
      <div className="flex flex-col lg:flex-row items-center justify-between gap-6">
        {/* Left: Self Player */}
        <div className="flex items-center gap-4 w-full lg:w-1/3">
          <div className="w-12 h-12 rounded-full bg-block-lime border border-black/10 flex items-center justify-center font-mono font-bold text-lg text-ink shadow-sm flex-shrink-0">
            {selfPlayer?.display_name?.charAt(0).toUpperCase() || 'P'}
          </div>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="font-card-title text-lg truncate text-ink">
                {selfPlayer?.display_name}
              </span>
              <span className="px-2 py-0.5 rounded-full bg-primary text-on-primary text-[10px] font-mono font-medium">
                YOU
              </span>
            </div>
            <div className="flex items-center gap-2 mt-1">
              <span className="text-xs font-mono text-neutral-500 uppercase">
                Slot {selfPlayer?.slot}
              </span>
              <span className="text-neutral-300">•</span>
              <span className="inline-flex items-center gap-1 text-xs font-mono font-semibold text-semantic-success bg-block-mint/50 px-2 py-0.5 rounded-full">
                <CheckCircle2 className="w-3 h-3" />
                {selfPlayer?.best_tests_passed || 0} / {match.problem.total_tests} Passed
              </span>
            </div>
          </div>
        </div>

        {/* Center: Match Timer & VS Banner */}
        <div className="flex flex-col items-center justify-center w-full lg:w-1/3">
          <div className="flex items-center gap-3">
            <div className="w-8 h-8 rounded-full bg-surface-soft border border-hairline flex items-center justify-center">
              <Swords className="w-4 h-4 text-ink" />
            </div>

            {/* Countdown Badge */}
            <div
              className={`flex items-center gap-2 px-5 py-2 rounded-full font-mono text-xl font-bold tracking-tight transition-all ${
                match.status === 'finished'
                  ? 'bg-surface-soft text-neutral-500 border border-hairline'
                  : isCritical
                  ? 'bg-block-pink text-red-700 border border-red-300 animate-pulse'
                  : 'bg-primary text-on-primary shadow-sm'
              }`}
            >
              <Clock className="w-4 h-4" />
              <span>{match.status === 'finished' ? 'FINISHED' : formatTime(secondsRemaining)}</span>
            </div>
          </div>

          <div className="mt-1 text-center">
            <span className="font-mono text-[11px] uppercase tracking-caption text-neutral-400">
              Duel Match: {match.id.slice(0, 8)}
            </span>
          </div>
        </div>

        {/* Right: Opponent Player */}
        <div className="flex items-center justify-end gap-4 w-full lg:w-1/3">
          <div className="min-w-0 text-right">
            <div className="flex items-center justify-end gap-2">
              <span className="px-2 py-0.5 rounded-full bg-surface-soft text-neutral-600 text-[10px] font-mono font-medium border border-hairline">
                OPPONENT
              </span>
              <span className="font-card-title text-lg truncate text-ink">
                {oppPlayer?.display_name || 'Waiting...'}
              </span>
            </div>
            <div className="flex items-center justify-end gap-2 mt-1">
              <span className="inline-flex items-center gap-1 text-xs font-mono font-semibold text-neutral-700 bg-surface-soft px-2 py-0.5 rounded-full border border-hairline">
                <CheckCircle2 className="w-3 h-3 text-neutral-400" />
                {oppPlayer?.best_tests_passed || 0} / {match.problem.total_tests} Passed
              </span>
              <span className="text-neutral-300">•</span>
              <span className="text-xs font-mono text-neutral-500 uppercase">
                Slot {oppPlayer?.slot || 2}
              </span>
            </div>
          </div>
          <div className="w-12 h-12 rounded-full bg-block-lilac border border-black/10 flex items-center justify-center font-mono font-bold text-lg text-ink shadow-sm flex-shrink-0">
            {oppPlayer?.display_name?.charAt(0).toUpperCase() || 'O'}
          </div>
        </div>
      </div>
    </div>
  );
};
