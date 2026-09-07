import React, { useState } from 'react';
import { useAuth } from '../../context/AuthContext';
import { Button } from '../common/Button';
import { Modal } from '../common/Modal';
import { Sparkles, AlertCircle } from 'lucide-react';

interface AuthModalProps {
  isOpen: boolean;
  onClose: () => void;
  defaultMode?: 'login' | 'register';
}

export const AuthModal: React.FC<AuthModalProps> = ({
  isOpen,
  onClose,
  defaultMode = 'login',
}) => {
  const { login, register, quickLoginDemo } = useAuth();
  const [mode, setMode] = useState<'login' | 'register'>(defaultMode);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!email.includes('@')) {
      setError('Please enter a valid email address.');
      return;
    }
    if (password.length < 8) {
      setError('Password must be at least 8 characters long.');
      return;
    }

    setLoading(true);
    try {
      if (mode === 'login') {
        await login(email, password);
      } else {
        await register(email, password);
      }
      onClose();
    } catch (err: unknown) {
      const apiErr = err as { message?: string };
      setError(apiErr.message || 'Authentication failed. Please check credentials.');
    } finally {
      setLoading(false);
    }
  };

  const handleDemoClick = async (player: 'player1' | 'player2') => {
    setError(null);
    setLoading(true);
    try {
      await quickLoginDemo(player);
      onClose();
    } catch (err: unknown) {
      const apiErr = err as { message?: string };
      setError(apiErr.message || 'Demo sign-in failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={mode === 'login' ? 'Sign In to Arena' : 'Create Account'}
    >
      {/* 1-Click Demo Accounts Banner */}
      <div className="mb-6 p-4 rounded-[16px] bg-block-lime/60 border border-black/10">
        <div className="flex items-center gap-2 mb-2">
          <Sparkles className="w-4 h-4 text-ink" />
          <span className="font-mono text-xs uppercase tracking-caption font-bold text-ink">
            Instant Test Login
          </span>
        </div>
        <p className="text-xs text-neutral-700 font-sans mb-3">
          Jump straight into a 1v1 duel using our pre-seeded local development profiles:
        </p>
        <div className="grid grid-cols-2 gap-2">
          <Button
            type="button"
            variant="secondary"
            onClick={() => handleDemoClick('player1')}
            disabled={loading}
            className="text-xs py-1.5 h-9 bg-canvas hover:bg-white"
          >
            Play as Alice
          </Button>
          <Button
            type="button"
            variant="secondary"
            onClick={() => handleDemoClick('player2')}
            disabled={loading}
            className="text-xs py-1.5 h-9 bg-canvas hover:bg-white"
          >
            Play as Bob
          </Button>
        </div>
      </div>

      <div className="relative flex items-center justify-center my-4">
        <div className="absolute inset-0 flex items-center">
          <div className="w-full border-t border-hairline" />
        </div>
        <span className="relative bg-canvas px-3 text-xs font-mono uppercase tracking-caption text-neutral-400">
          or use credentials
        </span>
      </div>

      {error && (
        <div className="mb-4 p-3 rounded-[12px] bg-block-pink/80 border border-red-200 text-ink text-xs flex items-center gap-2">
          <AlertCircle className="w-4 h-4 text-red-600 flex-shrink-0" />
          <span>{error}</span>
        </div>
      )}

      <form onSubmit={handleSubmit} className="space-y-4">
        <div>
          <label className="block text-xs font-mono uppercase tracking-caption text-neutral-700 mb-1.5 font-medium">
            Email Address
          </label>
          <input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="developer@example.com"
            className="w-full px-3.5 py-2.5 rounded-md border border-hairline bg-canvas text-ink text-sm focus:outline-none focus:ring-2 focus:ring-black focus:border-transparent transition-all"
          />
        </div>

        <div>
          <label className="block text-xs font-mono uppercase tracking-caption text-neutral-700 mb-1.5 font-medium">
            Password
          </label>
          <input
            type="password"
            required
            minLength={8}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="••••••••"
            className="w-full px-3.5 py-2.5 rounded-md border border-hairline bg-canvas text-ink text-sm focus:outline-none focus:ring-2 focus:ring-black focus:border-transparent transition-all"
          />
          <span className="text-[11px] text-neutral-500 font-sans mt-1 block">
            Minimum 8 characters
          </span>
        </div>

        <div className="pt-2">
          <Button
            type="submit"
            variant="primary"
            isLoading={loading}
            className="w-full py-3 text-base"
          >
            {mode === 'login' ? 'Sign In' : 'Create Account'}
          </Button>
        </div>

        <div className="text-center pt-2">
          {mode === 'login' ? (
            <p className="text-xs text-neutral-600">
              Don't have an account?{' '}
              <button
                type="button"
                onClick={() => {
                  setMode('register');
                  setError(null);
                }}
                className="font-medium text-ink underline hover:text-neutral-800"
              >
                Create one now
              </button>
            </p>
          ) : (
            <p className="text-xs text-neutral-600">
              Already have an account?{' '}
              <button
                type="button"
                onClick={() => {
                  setMode('login');
                  setError(null);
                }}
                className="font-medium text-ink underline hover:text-neutral-800"
              >
                Sign in instead
              </button>
            </p>
          )}
        </div>
      </form>
    </Modal>
  );
};
