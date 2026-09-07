import React, { useState, useEffect } from 'react';
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
import { ArchitecturePage } from './pages/ArchitecturePage';

const AppContent: React.FC = () => {
  const { user, quickLoginDemo } = useAuth();
  const { latestMatchStart, isWaitingModalOpen, setIsWaitingModalOpen, startMatchmaking } = useWS();
  const [currentView, setCurrentView] = useState<string>('landing');
  const [authModalOpen, setAuthModalOpen] = useState(false);
  const [selectedMatchId, setSelectedMatchId] = useState<string | undefined>(undefined);

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
            onViewArchitecture={() => setCurrentView('architecture')}
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

        {currentView === 'architecture' && <ArchitecturePage />}
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
