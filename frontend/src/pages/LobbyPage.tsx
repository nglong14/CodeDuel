import React, { useEffect, useState } from 'react';
import { useAuth } from '../context/AuthContext';
import { useWS } from '../context/WebSocketContext';
import { api } from '../services/api';
import { MatchSnapshot } from '../types/api';
import { QueueCard } from '../components/lobby/QueueCard';
import { DualPlaySimulator } from '../components/lobby/DualPlaySimulator';
import { Swords, History, CheckCircle2, ArrowRight } from 'lucide-react';
import { Button } from '../components/common/Button';

interface LobbyPageProps {
  onOpenAuth: () => void;
  onEnterMatch: (matchId: string) => void;
}

export const LobbyPage: React.FC<LobbyPageProps> = ({
  onOpenAuth,
  onEnterMatch,
}) => {
  const { user } = useAuth();
  const { activeMatchId, latestMatchStart, status } = useWS();
  const [currentMatch, setCurrentMatch] = useState<MatchSnapshot | null>(null);
  const [loadingMatch, setLoadingMatch] = useState<boolean>(false);

  // Check if current user has an active match in the database
  useEffect(() => {
    async function checkCurrentMatch() {
      if (!user) return;
      setLoadingMatch(true);
      try {
        const match = await api.getCurrentMatch();
        setCurrentMatch(match);
      } catch (err) {
        console.warn('Failed to load current match:', err);
      } finally {
        setLoadingMatch(false);
      }
    }

    if (status === 'connected') {
      checkCurrentMatch();
    }
  }, [status, user]);

  const targetMatchId = latestMatchStart?.match_id || activeMatchId || currentMatch?.id;

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-10 md:py-16">
      {/* Header */}
      <div className="mb-8">
        <div className="flex items-center gap-2 text-xs font-mono uppercase tracking-caption text-neutral-500 mb-2">
          <span>Arena Lobby</span>
          <span>•</span>
          <span>Ranked 1v1 Pool</span>
        </div>
        <h1 className="font-display-lg text-4xl md:text-5xl text-ink font-normal tracking-tight">
          Matchmaking Arena
        </h1>
        <p className="font-sans text-base text-neutral-600 mt-2 max-w-xl">
          Queue up for a 1v1 competitive coding duel. Two players race on the same problem with real-time test case execution.
        </p>
      </div>

      {/* Main Matchmaking Status & Action Card */}
      <QueueCard
        onOpenAuth={onOpenAuth}
        onViewActiveMatch={() => {
          if (targetMatchId) onEnterMatch(targetMatchId);
        }}
      />

      {/* If an active match is loaded from DB */}
      {currentMatch && currentMatch.status === 'active' && (
        <div className="my-8 p-6 rounded-[24px] bg-block-mint/70 border border-semantic-success/30 flex flex-col sm:flex-row items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-full bg-canvas border border-semantic-success/40 flex items-center justify-center">
              <Swords className="w-5 h-5 text-semantic-success" />
            </div>
            <div>
              <div className="font-card-title text-base text-ink">
                Active Duel: {currentMatch.problem.title}
              </div>
              <p className="font-sans text-xs text-neutral-600">
                Deadline: {new Date(currentMatch.deadline).toLocaleTimeString()} • {currentMatch.problem.total_tests} test cases
              </p>
            </div>
          </div>
          <Button
            variant="primary"
            onClick={() => onEnterMatch(currentMatch.id)}
            icon={<ArrowRight className="w-4 h-4" />}
            className="px-6 py-2.5"
          >
            Enter Duel Room
          </Button>
        </div>
      )}

      {/* Multiplayer Simulator for Easy Testing */}
      <div className="my-12">
        <DualPlaySimulator />
      </div>
    </div>
  );
};
