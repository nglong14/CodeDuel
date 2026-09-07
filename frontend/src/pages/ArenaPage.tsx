import React, { useEffect, useState, useCallback } from 'react';
import { useAuth } from '../context/AuthContext';
import { useWS } from '../context/WebSocketContext';
import { api } from '../services/api';
import { MatchSnapshot, SubmissionSnapshot } from '../types/api';
import { SupportedLanguage } from '../types/proto';
import { MatchHUD } from '../components/arena/MatchHUD';
import { ProblemPanel } from '../components/arena/ProblemPanel';
import { SubmissionsPanel } from '../components/arena/SubmissionsPanel';
import { CodeEditor } from '../components/editor/CodeEditor';
import { MatchEndModal } from '../components/arena/MatchEndModal';
import { Button } from '../components/common/Button';
import { FileText, Cpu, ArrowLeft, Loader2, AlertCircle } from 'lucide-react';

interface ArenaPageProps {
  matchId?: string;
  onBackToLobby: () => void;
}

export const ArenaPage: React.FC<ArenaPageProps> = ({
  matchId,
  onBackToLobby,
}) => {
  const { user } = useAuth();
  const {
    submitCode,
    latestJudging,
    latestResult,
    latestMatchEnd,
    latestError,
    clearMatchState,
  } = useWS();

  const [match, setMatch] = useState<MatchSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<'problem' | 'submissions'>('problem');
  const [selectedLanguage, setSelectedLanguage] = useState<SupportedLanguage>('python');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isModalOpen, setIsModalOpen] = useState(false);

  // Load match snapshot from database
  const loadMatch = useCallback(async () => {
    try {
      setLoading(true);
      let data: MatchSnapshot | null = null;
      if (matchId) {
        data = await api.getMatchById(matchId);
      } else {
        data = await api.getCurrentMatch();
      }

      if (!data) {
        setError('Match not found or already expired.');
      } else {
        setMatch(data);
        if (data.status === 'finished') {
          setIsModalOpen(true);
        }
      }
    } catch (err: unknown) {
      const apiErr = err as { message?: string };
      setError(apiErr.message || 'Failed to load match snapshot.');
    } finally {
      setLoading(false);
    }
  }, [matchId]);

  useEffect(() => {
    loadMatch();
  }, [loadMatch]);

  // Handle WebSocket judging event
  useEffect(() => {
    if (latestJudging && match && user) {
      setIsSubmitting(false);
      // Append or update pending submission in list
      const newSub: SubmissionSnapshot = {
        id: latestJudging.submission_id,
        request_id: latestJudging.request_id,
        language: selectedLanguage,
        status: 'pending',
        verdict: null,
        failure_kind: null,
        tests_passed: 0,
        created_at: new Date().toISOString(),
        finished_at: null,
      };

      setMatch((prev) => {
        if (!prev) return prev;
        const exists = prev.submissions.some((s) => s.id === newSub.id);
        if (exists) return prev;
        return {
          ...prev,
          submissions: [newSub, ...prev.submissions],
        };
      });

      // Switch to submissions tab to view live judging
      setActiveTab('submissions');
    }
  }, [latestJudging, match, user, selectedLanguage]);

  // Handle WebSocket result event
  useEffect(() => {
    if (latestResult && match) {
      setIsSubmitting(false);
      setMatch((prev) => {
        if (!prev) return prev;
        // Update submission verdict
        const updatedSubmissions = prev.submissions.map((sub) => {
          if (sub.id === latestResult.submission_id || sub.request_id === latestResult.request_id) {
            return {
              ...sub,
              status: 'completed' as const,
              verdict: latestResult.verdict,
              tests_passed: latestResult.tests_passed,
              finished_at: new Date().toISOString(),
            };
          }
          return sub;
        });

        // Update player best tests passed
        const updatedPlayers = prev.players.map((player) => {
          if (player.id === latestResult.player_id) {
            return {
              ...player,
              best_tests_passed: Math.max(player.best_tests_passed, latestResult.tests_passed),
            };
          }
          return player;
        });

        const isFinished = !!latestResult.winner_id || latestResult.outcome !== undefined;

        return {
          ...prev,
          status: isFinished ? 'finished' : prev.status,
          winner_id: latestResult.winner_id || prev.winner_id,
          outcome: latestResult.outcome || prev.outcome,
          players: updatedPlayers,
          submissions: updatedSubmissions,
        };
      });

      if (latestResult.outcome || latestResult.winner_id) {
        setIsModalOpen(true);
      }
    }
  }, [latestResult, match]);

  // Handle WebSocket match_end event
  useEffect(() => {
    if (latestMatchEnd && match) {
      setMatch((prev) => {
        if (!prev) return prev;
        return {
          ...prev,
          status: 'finished',
          winner_id: latestMatchEnd.winner_id || prev.winner_id,
          outcome: latestMatchEnd.outcome,
        };
      });
      setIsModalOpen(true);
    }
  }, [latestMatchEnd, match]);

  const handleSubmitCode = (code: string) => {
    if (!match || match.status === 'finished') return;
    setIsSubmitting(true);
    try {
      submitCode(match.id, selectedLanguage, code);
    } catch (err: unknown) {
      setIsSubmitting(false);
      console.error('Submit code failed:', err);
    }
  };

  if (loading) {
    return (
      <div className="min-h-[70vh] flex flex-col items-center justify-center space-y-4">
        <Loader2 className="w-8 h-8 text-ink animate-spin" />
        <p className="font-mono text-xs uppercase tracking-caption text-neutral-500">
          Loading duel state & sandbox...
        </p>
      </div>
    );
  }

  if (error || !match) {
    return (
      <div className="max-w-2xl mx-auto my-20 p-8 rounded-[24px] bg-block-pink border border-red-200 text-center space-y-4">
        <AlertCircle className="w-10 h-10 text-red-600 mx-auto" />
        <h2 className="font-headline text-2xl text-ink font-normal">
          Unable to Load Duel
        </h2>
        <p className="font-sans text-sm text-neutral-700">
          {error || 'No active match found for your account.'}
        </p>
        <Button variant="primary" onClick={onBackToLobby} className="mt-4">
          Return to Lobby
        </Button>
      </div>
    );
  }

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6 md:py-10">
      {/* Top back navigation */}
      <div className="mb-4 flex items-center justify-between">
        <button
          onClick={onBackToLobby}
          className="inline-flex items-center gap-2 text-xs font-mono text-neutral-600 hover:text-ink transition-colors px-3 py-1.5 rounded-full hover:bg-surface-soft border border-hairline"
        >
          <ArrowLeft className="w-3.5 h-3.5" />
          <span>Exit Duel / Back to Lobby</span>
        </button>

        <div className="text-xs font-mono text-neutral-500">
          Status:{' '}
          <span
            className={`font-semibold uppercase ${
              match.status === 'active' ? 'text-semantic-success' : 'text-neutral-500'
            }`}
          >
            {match.status}
          </span>
        </div>
      </div>

      {/* Match HUD (Players, vs, clock) */}
      <MatchHUD match={match} currentUserId={user?.id || ''} />

      {/* Duel Arena Split View */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
        {/* Left Column: Problem Statement & Submissions */}
        <div className="lg:col-span-5 flex flex-col h-[650px]">
          {/* Tabs header */}
          <div className="flex items-center gap-1 mb-3 p-1 bg-surface-soft rounded-full border border-hairline w-fit">
            <button
              onClick={() => setActiveTab('problem')}
              className={`flex items-center gap-2 px-4 py-1.5 rounded-full text-xs font-mono font-medium transition-all ${
                activeTab === 'problem'
                  ? 'bg-canvas text-ink shadow-xs border border-hairline'
                  : 'text-neutral-600 hover:text-ink'
              }`}
            >
              <FileText className="w-3.5 h-3.5" />
              Problem
            </button>
            <button
              onClick={() => setActiveTab('submissions')}
              className={`flex items-center gap-2 px-4 py-1.5 rounded-full text-xs font-mono font-medium transition-all ${
                activeTab === 'submissions'
                  ? 'bg-canvas text-ink shadow-xs border border-hairline'
                  : 'text-neutral-600 hover:text-ink'
              }`}
            >
              <Cpu className="w-3.5 h-3.5" />
              Submissions ({match.submissions.length})
            </button>
          </div>

          <div className="flex-1 overflow-hidden">
            {activeTab === 'problem' ? (
              <ProblemPanel problem={match.problem} />
            ) : (
              <SubmissionsPanel
                submissions={match.submissions}
                totalTests={match.problem.total_tests}
              />
            )}
          </div>
        </div>

        {/* Right Column: Code Editor */}
        <div className="lg:col-span-7 h-[650px] pt-11 lg:pt-0">
          <CodeEditor
            language={selectedLanguage}
            onLanguageChange={setSelectedLanguage}
            onSubmit={handleSubmitCode}
            isSubmitting={isSubmitting}
            disabled={match.status === 'finished'}
          />
        </div>
      </div>

      {/* Match End Celebration / Outcome Modal */}
      {match && (
        <MatchEndModal
          isOpen={isModalOpen}
          onClose={() => setIsModalOpen(false)}
          match={match}
          currentUserId={user?.id || ''}
          onPlayAgain={() => {
            setIsModalOpen(false);
            clearMatchState();
            onBackToLobby();
          }}
        />
      )}
    </div>
  );
};
