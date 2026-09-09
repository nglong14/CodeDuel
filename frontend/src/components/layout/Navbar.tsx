import React, { useState } from 'react';
import { Swords, Wifi, WifiOff, User as UserIcon, LogOut, Menu, X, Sparkles } from 'lucide-react';
import { useAuth } from '../../context/AuthContext';
import { useWS } from '../../context/WebSocketContext';
import { Button } from '../common/Button';

interface NavbarProps {
  currentView: string;
  onNavigate: (view: string) => void;
  onOpenAuth: () => void;
}

export const Navbar: React.FC<NavbarProps> = ({
  currentView,
  onNavigate,
  onOpenAuth,
}) => {
  const { user, logout, quickLoginDemo } = useAuth();
  const { status, isSearching, activeMatchId } = useWS();
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [quickSwitchOpen, setQuickSwitchOpen] = useState(false);

  const getStatusBadge = () => {
    switch (status) {
      case 'connected':
        return (
          <div className="flex items-center gap-1.5 px-3 py-1 rounded-full bg-block-mint/70 text-ink text-xs font-mono font-medium border border-semantic-success/30">
            <span className="w-2 h-2 rounded-full bg-semantic-success animate-pulse" />
            LIVE
          </div>
        );
      case 'connecting':
        return (
          <div className="flex items-center gap-1.5 px-3 py-1 rounded-full bg-block-cream text-ink text-xs font-mono font-medium border border-hairline">
            <span className="w-2 h-2 rounded-full bg-amber-500 animate-ping" />
            CONNECTING
          </div>
        );
      default:
        return (
          <div className="flex items-center gap-1.5 px-3 py-1 rounded-full bg-surface-soft text-ink/70 text-xs font-mono font-medium border border-hairline">
            <span className="w-2 h-2 rounded-full bg-neutral-400" />
            STANDBY
          </div>
        );
    }
  };

  return (
    <header className="sticky top-0 z-40 w-full h-14 bg-canvas border-b border-hairline transition-all">
      <div className="max-w-7xl mx-auto h-full px-4 sm:px-6 lg:px-8 flex items-center justify-between">
        {/* Logo and Brand */}
        <div className="flex items-center gap-8">
          <button
            onClick={() => onNavigate('landing')}
            className="flex items-center gap-2.5 group focus:outline-none"
          >
            <div className="w-8 h-8 rounded-full bg-primary flex items-center justify-center text-white transition-transform group-hover:scale-105">
              <Swords className="w-4 h-4 text-block-lime" />
            </div>
            <span className="font-sans font-bold text-xl tracking-tight text-ink">
              Code<span className="text-black/80 font-normal">Duel</span>
            </span>
          </button>

          {/* Desktop Nav links */}
          <nav className="hidden md:flex items-center gap-1">
            <button
              onClick={() => onNavigate('landing')}
              className={`px-3 py-1.5 rounded-full text-sm font-medium transition-colors ${
                currentView === 'landing'
                  ? 'bg-surface-soft text-ink font-semibold'
                  : 'text-ink/80 hover:text-ink hover:bg-surface-soft/60'
              }`}
            >
              Overview
            </button>
            <button
              onClick={() => onNavigate('lobby')}
              className={`px-3 py-1.5 rounded-full text-sm font-medium flex items-center gap-2 transition-colors ${
                currentView === 'lobby' || currentView === 'arena'
                  ? 'bg-surface-soft text-ink font-semibold'
                  : 'text-ink/80 hover:text-ink hover:bg-surface-soft/60'
              }`}
            >
              Arena
              {isSearching && (
                <span className="w-2 h-2 rounded-full bg-accent-magenta animate-ping" />
              )}
              {activeMatchId && (
                <span className="px-1.5 py-0.2 text-[10px] font-mono rounded-full bg-block-lime text-ink">
                  ACTIVE
                </span>
              )}
            </button>
          </nav>
        </div>

        {/* Right side status and auth controls */}
        <div className="flex items-center gap-3">
          {/* Connection badge */}
          <div className="hidden sm:block">{getStatusBadge()}</div>

          {user ? (
            <div className="flex items-center gap-2 relative">
              {/* User badge */}
              <button
                onClick={() => setQuickSwitchOpen(!quickSwitchOpen)}
                className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-surface-soft border border-hairline text-sm font-medium hover:bg-hairline/60 transition-colors"
                title="Click to switch or manage player"
              >
                <div className="w-5 h-5 rounded-full bg-block-lime flex items-center justify-center text-xs font-mono font-bold text-ink">
                  {user.display_name.charAt(0).toUpperCase()}
                </div>
                <span className="max-w-[120px] truncate font-sans font-medium text-ink">
                  {user.display_name}
                </span>
              </button>

              {/* Quick switch popover */}
              {quickSwitchOpen && (
                <div className="absolute right-0 top-12 w-64 bg-canvas border border-hairline rounded-[16px] shadow-xl p-3 z-50 animate-fadeIn">
                  <div className="text-xs font-mono uppercase tracking-caption text-neutral-500 mb-2 px-2">
                    Signed in as
                  </div>
                  <div className="px-2 py-1 mb-3 bg-surface-soft rounded-lg text-xs font-mono break-all text-neutral-800">
                    {user.email}
                  </div>
                  <div className="text-xs font-mono uppercase tracking-caption text-neutral-500 mb-1.5 px-2">
                    Switch Test Account
                  </div>
                  <div className="space-y-1">
                    <button
                      onClick={() => {
                        quickLoginDemo('player1');
                        setQuickSwitchOpen(false);
                      }}
                      className="w-full text-left px-3 py-2 rounded-lg text-xs hover:bg-block-lime transition-colors flex items-center justify-between"
                    >
                      <span className="font-medium">Player 1 (Alice)</span>
                      <span className="text-[10px] font-mono opacity-70">Slot 1</span>
                    </button>
                    <button
                      onClick={() => {
                        quickLoginDemo('player2');
                        setQuickSwitchOpen(false);
                      }}
                      className="w-full text-left px-3 py-2 rounded-lg text-xs hover:bg-block-lilac transition-colors flex items-center justify-between"
                    >
                      <span className="font-medium">Player 2 (Bob)</span>
                      <span className="text-[10px] font-mono opacity-70">Slot 2</span>
                    </button>
                  </div>
                  <div className="border-t border-hairline mt-3 pt-2">
                    <button
                      onClick={() => {
                        logout();
                        setQuickSwitchOpen(false);
                      }}
                      className="w-full text-left px-3 py-1.5 rounded-lg text-xs text-red-600 hover:bg-block-pink/50 transition-colors flex items-center gap-1.5 font-medium"
                    >
                      <LogOut className="w-3.5 h-3.5" />
                      Sign Out
                    </button>
                  </div>
                </div>
              )}

              <Button
                variant="icon-circular"
                onClick={logout}
                title="Log out"
                className="hidden sm:flex"
              >
                <LogOut className="w-4 h-4 text-ink" />
              </Button>
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <Button
                variant="secondary"
                onClick={() => quickLoginDemo('player1')}
                className="hidden sm:inline-flex text-xs px-3.5 py-1.5 min-h-[38px]"
              >
                <Sparkles className="w-3.5 h-3.5 text-accent-magenta" />
                Demo Alice
              </Button>
              <Button
                variant="primary"
                onClick={onOpenAuth}
                className="text-xs px-4 py-1.5 min-h-[38px]"
              >
                Sign In
              </Button>
            </div>
          )}

          {/* Mobile menu toggle */}
          <div className="md:hidden">
            <Button
              variant="icon-circular"
              onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
              aria-label="Toggle menu"
            >
              {mobileMenuOpen ? <X className="w-5 h-5" /> : <Menu className="w-5 h-5" />}
            </Button>
          </div>
        </div>
      </div>

      {/* Mobile nav dropdown */}
      {mobileMenuOpen && (
        <div className="md:hidden bg-canvas border-b border-hairline px-4 py-4 space-y-2 shadow-lg animate-fadeIn">
          <button
            onClick={() => {
              onNavigate('landing');
              setMobileMenuOpen(false);
            }}
            className="w-full text-left px-3 py-2 rounded-lg text-base font-medium hover:bg-surface-soft"
          >
            Overview
          </button>
          <button
            onClick={() => {
              onNavigate('lobby');
              setMobileMenuOpen(false);
            }}
            className="w-full text-left px-3 py-2 rounded-lg text-base font-medium hover:bg-surface-soft flex items-center justify-between"
          >
            <span>Duel Arena</span>
            {isSearching && (
              <span className="px-2 py-0.5 text-xs font-mono rounded-full bg-accent-magenta text-white">
                SEARCHING
              </span>
            )}
          </button>
          <div className="pt-2 border-t border-hairline flex items-center justify-between">
            <span className="text-xs font-mono uppercase tracking-caption text-neutral-500">
              Connection
            </span>
            {getStatusBadge()}
          </div>
        </div>
      )}
    </header>
  );
};
