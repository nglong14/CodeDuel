import React, { useState, useEffect } from 'react';
import { Swords, ShieldAlert, Sparkles, ArrowRight, Eye } from 'lucide-react';
import { useWS } from '../../context/WebSocketContext';
import { useAuth } from '../../context/AuthContext';
import { Button } from '../common/Button';
import { ColorBlock } from '../common/ColorBlock';

interface QueueCardProps {
  onOpenAuth: () => void;
  onViewActiveMatch: () => void;
}

export const QueueCard: React.FC<QueueCardProps> = ({
  onOpenAuth,
  onViewActiveMatch,
}) => {
  const { user, quickLoginDemo } = useAuth();
  const { isSearching, activeMatchId, latestError, startMatchmaking, cancelMatchmaking, setIsWaitingModalOpen } = useWS();
  const [queueElapsed, setQueueElapsed] = useState(0);

  useEffect(() => {
    let timer: ReturnType<typeof setInterval> | null = null;
    if (isSearching) {
      setQueueElapsed(0);
      timer = setInterval(() => {
        setQueueElapsed((prev) => prev + 1);
      }, 1000);
    } else {
      setQueueElapsed(0);
    }
    return () => {
      if (timer) clearInterval(timer);
    };
  }, [isSearching]);

  const formatElapsed = (sec: number) => {
    const mins = Math.floor(sec / 60);
    const s = sec % 60;
    return `${mins}:${s.toString().padStart(2, '0')}`;
  };

  if (!user) {
    return (
      <ColorBlock variant="lime" className="my-8">
        <div className="max-w-2xl">
          <span className="font-mono text-xs uppercase tracking-caption font-bold text-neutral-800 mb-2 block">
            Arena Matchmaking Pool
          </span>
          <h2 className="font-headline text-3xl md:text-4xl text-ink mb-4 font-normal tracking-tight">
            Ready to enter the CodeDuel 1v1 queue?
          </h2>
          <p className="font-sans text-base text-neutral-800 mb-6 font-normal leading-relaxed">
            Matches are real-time, 1-on-1 coding battles where the first complete solution wins.
            Jump straight into matchmaking with 1-click test access or custom credentials.
          </p>
          <div className="flex flex-wrap items-center gap-3">
            <Button
              variant="primary"
              onClick={async () => {
                await quickLoginDemo('player1');
                startMatchmaking();
              }}
              icon={<Sparkles className="w-4 h-4 text-block-lime" />}
              className="px-6 py-3 text-base"
            >
              Quick Play (1-Click Duel)
            </Button>
            <Button
              variant="secondary"
              onClick={onOpenAuth}
              className="px-6 py-3 text-base"
            >
              Sign In with Email
            </Button>
          </div>
        </div>
      </ColorBlock>
    );
  }

  return (
    <div className="space-y-6 my-8">
      {/* Active match detection banner */}
      {activeMatchId && (
        <div className="p-6 rounded-[24px] bg-block-lilac border border-purple-200 flex flex-col sm:flex-row items-center justify-between gap-4 shadow-sm animate-fadeIn">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-full bg-canvas flex items-center justify-center">
              <Swords className="w-5 h-5 text-ink" />
            </div>
            <div>
              <h4 className="font-card-title text-base text-ink">
                You have an active duel in progress!
              </h4>
              <p className="text-xs font-sans text-neutral-700">
                Match ID: <code className="font-mono">{activeMatchId.slice(0, 8)}</code>
              </p>
            </div>
          </div>
          <Button
            variant="primary"
            onClick={onViewActiveMatch}
            icon={<ArrowRight className="w-4 h-4" />}
            className="px-6 py-2.5"
          >
            Resume Active Duel
          </Button>
        </div>
      )}

      {/* Error alert banner */}
      {latestError && (
        <div className="p-4 rounded-[16px] bg-block-pink border border-red-300 text-red-900 text-xs font-mono flex items-center gap-3">
          <ShieldAlert className="w-5 h-5 text-red-600 flex-shrink-0" />
          <div className="flex-1">
            <span className="font-bold">Matchmaking Notice: </span>
            <span>{latestError.message}</span>
            {latestError.code === 'already_in_match' && (
              <span className="ml-2 underline cursor-pointer" onClick={onViewActiveMatch}>
                Resume Match
              </span>
            )}
          </div>
        </div>
      )}

      {/* Main Queue Card */}
      {isSearching ? (
        <ColorBlock variant="lilac">
          <div className="flex flex-col md:flex-row items-center justify-between gap-8">
            <div className="space-y-3 max-w-xl text-center md:text-left">
              <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-canvas text-xs font-mono font-bold text-ink shadow-xs">
                <span className="w-2.5 h-2.5 rounded-full bg-accent-magenta animate-ping" />
                SEARCHING FOR A DUEL
              </div>
              <h2 className="font-headline text-3xl md:text-4xl text-ink font-normal tracking-tight">
                Searching for your opponent...
              </h2>
              <p className="font-sans text-base text-neutral-800 font-normal">
                Waiting for another player to join. You will enter the arena as soon as a match is ready.
              </p>
              <div className="pt-2 flex items-center gap-3">
                <Button
                  variant="secondary"
                  onClick={() => setIsWaitingModalOpen(true)}
                  icon={<Eye className="w-4 h-4" />}
                  className="px-5 py-2 text-xs bg-canvas"
                >
                  Open Waiting Screen
                </Button>
              </div>
            </div>

            {/* Radar / Timer display */}
            <div className="flex flex-col items-center gap-4 bg-canvas p-6 rounded-[24px] border border-hairline shadow-md min-w-[240px]">
              <div className="relative w-20 h-20 flex items-center justify-center">
                <div className="absolute inset-0 rounded-full border-2 border-black/10 animate-ping opacity-75" />
                <div className="w-14 h-14 rounded-full bg-block-lime flex items-center justify-center shadow-inner">
                  <Swords className="w-6 h-6 text-ink animate-bounce" />
                </div>
              </div>
              <div className="text-center">
                <div className="font-mono text-2xl font-bold text-ink">
                  {formatElapsed(queueElapsed)}
                </div>
                <span className="text-[11px] font-mono text-neutral-500 uppercase tracking-caption">
                  Elapsed Time
                </span>
              </div>
              <Button
                variant="secondary"
                  onClick={cancelMatchmaking}
                className="w-full text-xs py-2"
              >
                Cancel Search
              </Button>
            </div>
          </div>
        </ColorBlock>
      ) : (
        <ColorBlock variant="lime">
          <div className="flex flex-col md:flex-row items-center justify-between gap-8">
            <div className="space-y-3 max-w-xl">
              <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-canvas text-xs font-mono font-bold text-ink">
                <span className="w-2 h-2 rounded-full bg-semantic-success" />
                ARENA READY • 1V1 DUEL
              </div>
              <h2 className="font-headline text-3xl md:text-4xl text-ink font-normal tracking-tight">
                Ready to compete, {user.display_name}?
              </h2>
              <p className="font-sans text-base text-neutral-800 font-normal leading-relaxed">
                Enter the matchmaking pool. You will be matched against another developer in a 10-minute race. The first complete solution wins.
              </p>
              <div className="pt-2 flex flex-wrap items-center gap-3">
                <Button
                  variant="primary"
                  onClick={() => startMatchmaking()}
                  icon={<Swords className="w-4 h-4" />}
                  className="px-8 py-3.5 text-base"
                >
                  Find a 1v1 Match
                </Button>
              </div>
            </div>

            {/* Rules sticky note */}
            <div className="bg-canvas p-6 rounded-[20px] border border-black/5 shadow-xs w-full md:w-80 space-y-3">
              <div className="font-mono text-xs uppercase tracking-caption font-bold text-neutral-700">
                Match Rules
              </div>
              <ul className="space-y-2 text-xs font-sans text-neutral-600">
                <li className="flex items-center gap-2">
                  <span className="w-1.5 h-1.5 rounded-full bg-black" />
                  <span><strong>10 Minute Limit:</strong> Strict countdown.</span>
                </li>
                <li className="flex items-center gap-2">
                  <span className="w-1.5 h-1.5 rounded-full bg-black" />
                  <span><strong>First Full Pass Wins:</strong> Finish every test first.</span>
                </li>
                <li className="flex items-center gap-2">
                  <span className="w-1.5 h-1.5 rounded-full bg-black" />
                  <span><strong>Languages:</strong> Python, C++, or Java.</span>
                </li>
                <li className="flex items-center gap-2">
                  <span className="w-1.5 h-1.5 rounded-full bg-black" />
                  <span><strong>Fair Play:</strong> Same challenge, same time limit.</span>
                </li>
              </ul>
            </div>
          </div>
        </ColorBlock>
      )}
    </div>
  );
};
