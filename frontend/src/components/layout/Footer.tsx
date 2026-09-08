import React from 'react';
import { Swords } from 'lucide-react';

export const Footer: React.FC = () => {
  return (
    <footer className="w-full bg-canvas border-t border-hairline py-16 md:py-24 px-6 md:px-8 mt-24">
      <div className="max-w-7xl mx-auto">
        <div className="grid grid-cols-1 md:grid-cols-5 gap-12 mb-16">
          {/* Brand Wordmark */}
          <div className="md:col-span-2 space-y-4">
            <div className="flex items-center gap-2.5">
              <div className="w-7 h-7 rounded-full bg-primary flex items-center justify-center text-white">
                <Swords className="w-3.5 h-3.5 text-block-lime" />
              </div>
              <span className="font-sans font-bold text-2xl tracking-tight text-ink">
                CodeDuel
              </span>
            </div>
            <p className="font-sans text-sm text-neutral-600 max-w-sm font-normal leading-relaxed">
              Fast, focused 1v1 coding duels for developers who want to test their problem-solving skills.
            </p>
            <div className="pt-2">
              <span className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-surface-soft border border-hairline text-xs font-mono text-neutral-700">
                <span className="w-2 h-2 rounded-full bg-semantic-success" />
                1V1 • LIVE • COMPETITIVE
              </span>
            </div>
          </div>

          {/* Duel Column */}
          <div>
            <div className="font-mono text-xs uppercase tracking-caption font-semibold text-neutral-900 mb-4">
              Duel Basics
            </div>
            <ul className="space-y-2.5 text-sm font-sans text-neutral-600">
              <li>One shared challenge</li>
              <li>Ten-minute countdown</li>
              <li>Live submissions</li>
              <li>First full pass wins</li>
            </ul>
          </div>

          {/* Supported Languages */}
          <div>
            <div className="font-mono text-xs uppercase tracking-caption font-semibold text-neutral-900 mb-4">
              Languages
            </div>
            <ul className="space-y-2.5 text-sm font-sans text-neutral-600">
              <li>Python</li>
              <li>C++</li>
              <li>Java</li>
            </ul>
          </div>

          {/* Play Column */}
          <div>
            <div className="font-mono text-xs uppercase tracking-caption font-semibold text-neutral-900 mb-4">
              Ready to Duel?
            </div>
            <ul className="space-y-2.5 text-sm font-sans text-neutral-600">
              <li>Find a match</li>
              <li>Write your solution</li>
              <li>Watch your progress</li>
              <li>Claim the win</li>
            </ul>
          </div>
        </div>

        {/* Bottom bar */}
        <div className="pt-8 border-t border-hairline-soft flex flex-col sm:flex-row items-center justify-between gap-4 text-xs font-mono text-neutral-500 uppercase tracking-caption">
          <div>
            © {new Date().getFullYear()} CodeDuel. Rigorous engineering, technical joy.
          </div>
          <div className="flex items-center gap-6">
            <span>Inter Sans</span>
            <span>•</span>
            <span>JetBrains Mono</span>
            <span>•</span>
            <span>Figma Editorial System</span>
          </div>
        </div>
      </div>
    </footer>
  );
};
