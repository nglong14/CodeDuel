import React, { useState, useEffect, useRef } from 'react';
import { AuthProvider, useAuth } from './context/AuthContext';
import { WebSocketProvider, useWS } from './context/WebSocketContext';
import { Navbar } from './components/layout/Navbar';
import { MarqueeStrip } from './components/layout/MarqueeStrip';
import { Footer } from './components/layout/Footer';
import { AuthModal } from './components/auth/AuthModal';
import { MatchmakingModal } from './components/lobby/MatchmakingModal';
import { LandingPage } from './pages/LandingPage';
import { LobbyPage } from './pages/LobbyPage';
import { ArenaPage } from './pages/ArenaPage';

const AppContent: React.FC = () => {
  const { user, quickLoginDemo } = useAuth();
  const { latestMatchStart, isWaitingModalOpen, setIsWaitingModalOpen, startMatchmaking, clearMatchState, clearQueueState } = useWS();
  const [currentView, setCurrentView] = useState<string>('landing');
  const [authModalOpen, setAuthModalOpen] = useState(false);
  const [selectedMatchId, setSelectedMatchId] = useState<string | undefined>(undefined);
  const previousUserId = useRef<string | null>(null);

  // Check URL parameters on initial load
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const playerParam = params.get('player');
    const matchParam = params.get('match');

    if (playerParam === 'bob' || playerParam === 'player2') {
      quickLoginDemo('player2');
      setCurrentView('lobby');
    } else if (playerParam === 'alice' || playerParam === 'player1') {
      quickLoginDemo('player1');
      setCurrentView('lobby');
    }

    if (matchParam) {
      setSelectedMatchId(matchParam);
      setCurrentView('arena');
    }
  }, []);

  // When a new match starts over WebSocket, automatically navigate to Arena!
  useEffect(() => {
    if (latestMatchStart) {
      setSelectedMatchId(latestMatchStart.match_id);
      setCurrentView('arena');
    }
  }, [latestMatchStart]);

  useEffect(() => {
    if (previousUserId.current && previousUserId.current !== user?.id) {
      clearMatchState();
      clearQueueState();
      setSelectedMatchId(undefined);
      setCurrentView('lobby');
    }
    previousUserId.current = user?.id ?? null;
  }, [clearMatchState, clearQueueState, user?.id]);

  const handleEnterMatch = (matchId: string) => {
    setSelectedMatchId(matchId);
    setCurrentView('arena');
  };

  const handleEnterArena = async () => {
    if (!user) {
      // Auto-connect as Demo Alice for zero-friction 1-click test flow
      await quickLoginDemo('player1');
    }
    startMatchmaking();
  };

  return (
    <div className="min-h-screen flex flex-col bg-canvas text-ink font-sans selection:bg-block-lime selection:text-ink">
      {/* Top sticky nav */}
      <Navbar
        currentView={currentView}
        onNavigate={(view) => setCurrentView(view)}
        onOpenAuth={() => setAuthModalOpen(true)}
      />

      {/* Ticker strip */}
      <MarqueeStrip />

      {/* Main page content */}
      <main className="flex-1">
        {currentView === 'landing' && (
          <LandingPage
            onEnterArena={handleEnterArena}
          />
        )}

        {currentView === 'lobby' && (
          <LobbyPage
            onOpenAuth={() => setAuthModalOpen(true)}
            onEnterMatch={handleEnterMatch}
          />
        )}

        {currentView === 'arena' && (
          <ArenaPage
            matchId={selectedMatchId}
            onBackToLobby={() => setCurrentView('lobby')}
          />
        )}
      </main>

      {/* Editorial footer */}
      <Footer />

      {/* Authentication Modal */}
      <AuthModal
        isOpen={authModalOpen}
        onClose={() => setAuthModalOpen(false)}
      />

      {/* Matchmaking Waiting Screen Modal */}
      <MatchmakingModal
        isOpen={isWaitingModalOpen}
        onClose={() => setIsWaitingModalOpen(false)}
        onMatchFound={handleEnterMatch}
      />
    </div>
  );
};

export const App: React.FC = () => {
  return (
    <AuthProvider>
      <WebSocketProvider>
        <AppContent />
      </WebSocketProvider>
    </AuthProvider>
  );
};

export default App;
