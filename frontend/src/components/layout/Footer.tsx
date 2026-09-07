import React from 'react';
import { Swords, Github } from 'lucide-react';

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
              A high-concurrency, 1v1 competitive coding engine powered by Go, Redis Streams,
              PostgreSQL conditional winner updates, and untrusted Docker sandboxes.
            </p>
            <div className="pt-2">
              <span className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-surface-soft border border-hairline text-xs font-mono text-neutral-700">
                <span className="w-2 h-2 rounded-full bg-semantic-success" />
                Go 1.26 • Redis 7 • Postgres 16
              </span>
            </div>
          </div>

          {/* Engine Column */}
          <div>
            <div className="font-mono text-xs uppercase tracking-caption font-semibold text-neutral-900 mb-4">
              Core Engine
            </div>
            <ul className="space-y-2.5 text-sm font-sans text-neutral-600">
              <li>API Gateway (WS Hub)</li>
              <li>Match Service (FIFO Pop)</li>
              <li>Judge Worker (Streams)</li>
              <li>Reaper & Sweeper</li>
              <li>Docker Sandbox Runner</li>
            </ul>
          </div>

          {/* Supported Languages */}
          <div>
            <div className="font-mono text-xs uppercase tracking-caption font-semibold text-neutral-900 mb-4">
              Sandbox Runtimes
            </div>
            <ul className="space-y-2.5 text-sm font-sans text-neutral-600">
              <li>Python 3.13 (Isolated)</li>
              <li>C++ GCC 14 (Seccomp)</li>
              <li>Java 21 (Temurin)</li>
              <li>Read-only RootFS</li>
              <li>Strict PIDs & CPU Caps</li>
            </ul>
          </div>

          {/* Docs Column */}
          <div>
            <div className="font-mono text-xs uppercase tracking-caption font-semibold text-neutral-900 mb-4">
              Architecture
            </div>
            <ul className="space-y-2.5 text-sm font-sans text-neutral-600">
              <li>Advisory Locks</li>
              <li>Lua Pop-Pair Script</li>
              <li>Idempotent Upserts</li>
              <li>PEL Auto-Reclaim</li>
              <li>Deterministic UUIDv5</li>
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
