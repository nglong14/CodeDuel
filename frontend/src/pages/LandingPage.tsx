import React from 'react';
import { Swords, ArrowRight, ShieldCheck, Zap, Server, Terminal, Lock, Cpu, Play } from 'lucide-react';
import { Button } from '../components/common/Button';
import { ColorBlock } from '../components/common/ColorBlock';
import { useAuth } from '../context/AuthContext';

interface LandingPageProps {
  onEnterArena: () => void;
  onViewArchitecture: () => void;
}

export const LandingPage: React.FC<LandingPageProps> = ({
  onEnterArena,
  onViewArchitecture,
}) => {
  const { user } = useAuth();

  return (
    <div className="w-full">
      {/* Editorial White Hero */}
      <section className="py-20 md:py-32 px-4 sm:px-6 lg:px-8 max-w-7xl mx-auto">
        <div className="max-w-4xl space-y-8">
          <div className="inline-flex items-center gap-2 px-3.5 py-1 rounded-full bg-surface-soft border border-hairline text-xs font-mono font-medium text-neutral-800">
            <span className="w-2 h-2 rounded-full bg-semantic-success" />
            HIGH-CONCURRENCY COMPETITIVE ENGINE
          </div>

          <h1 className="font-display-xl text-ink tracking-tight font-normal">
            Real-time 1v1 competitive coding.
          </h1>

          <p className="font-subhead text-neutral-800 max-w-2xl font-normal leading-relaxed">
            Two developers matched atomically. One LeetCode-style problem. Ten minutes on the clock.
            The first correct full-pass submission verified by an isolated Docker sandbox claims the win.
          </p>

          <div className="flex flex-wrap items-center gap-4 pt-4">
            <Button
              variant="primary"
              onClick={onEnterArena}
              icon={<Swords className="w-4 h-4 text-block-lime" />}
              className="px-8 py-4 text-lg"
            >
              {user ? 'Enter Duel Arena' : 'Find a 1v1 Match'}
            </Button>
            <Button
              variant="secondary"
              onClick={onViewArchitecture}
              className="px-6 py-4 text-lg"
            >
              Explore Architecture
            </Button>
          </div>
        </div>
      </section>

      {/* Spacing & Color Block Section 1: Systems / FAQ Ground (Lime) */}
      <section className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 my-16 md:my-24">
        <ColorBlock variant="lime">
          <div className="grid grid-cols-1 lg:grid-cols-12 gap-8 items-center">
            <div className="lg:col-span-7 space-y-4">
              <span className="font-mono text-xs uppercase tracking-caption font-bold text-neutral-800">
                01 / Atomic Matchmaking
              </span>
              <h2 className="font-headline text-3xl md:text-5xl text-ink font-normal tracking-tight">
                No double-matching. Guaranteed by Redis Lua.
              </h2>
              <p className="font-sans text-lg text-neutral-800 font-normal leading-relaxed">
                Players enqueue in a Redis ZSET ranked by millisecond arrival timestamps.
                When a pair is formed, an embedded Lua script executes <code className="bg-canvas/80 px-2 py-0.5 rounded font-mono text-sm">pop_pair</code> server-side
                in a single atomic step — making concurrent pops impossible without distributed locks.
              </p>
            </div>
            <div className="lg:col-span-5 bg-canvas rounded-[20px] p-6 border border-black/5 shadow-xs space-y-4 font-mono text-xs">
              <div className="flex items-center justify-between pb-3 border-b border-hairline">
                <span className="text-neutral-500 font-semibold uppercase tracking-caption">Redis ZSET Contract</span>
                <span className="text-semantic-success font-bold">ATOMIC LUA</span>
              </div>
              <div className="bg-surface-soft p-3 rounded-lg text-neutral-700 space-y-1">
                <div className="text-neutral-400">// Pop two lowest timestamps</div>
                <div>local members = redis.call('ZRANGE', KEYS[1], 0, 1)</div>
                <div>if #members == 2 then</div>
                <div className="pl-4 text-semantic-success">redis.call('ZREM', KEYS[1], unpack(members))</div>
                <div className="pl-4">return members</div>
                <div>end</div>
              </div>
            </div>
          </div>
        </ColorBlock>
      </section>

      {/* Return to Canvas Space */}
      <div className="h-12 md:h-20" />

      {/* Color Block Section 2: Duel Arena (Navy Ground) */}
      <section className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 my-16 md:my-24">
        <ColorBlock variant="navy">
          <div className="grid grid-cols-1 lg:grid-cols-12 gap-8 items-center">
            <div className="lg:col-span-7 space-y-4">
              <span className="font-mono text-xs uppercase tracking-caption font-bold text-block-lime">
                02 / Real-time Fan-out
              </span>
              <h2 className="font-headline text-3xl md:text-5xl text-white font-normal tracking-tight">
                Stateful WebSockets. Stateless Gateway.
              </h2>
              <p className="font-sans text-lg text-white/80 font-normal leading-relaxed">
                Gateway nodes hold persistent player sockets but maintain zero game state in memory.
                When Judge workers record a verdict, events fan out across per-user Redis Pub/Sub channels
                and stream into client browsers with microsecond latency.
              </p>
              <div className="pt-2 flex items-center gap-4 text-xs font-mono text-white/60">
                <span>Heartbeat: 54s Ping</span>
                <span>•</span>
                <span>Max Payload: 64KB</span>
                <span>•</span>
                <span>JWT HS256</span>
              </div>
            </div>
            <div className="lg:col-span-5 bg-white/10 backdrop-blur-sm rounded-[20px] p-6 border border-white/15 space-y-3 font-mono text-xs text-white">
              <div className="flex items-center justify-between pb-2 border-b border-white/20">
                <span className="uppercase tracking-caption text-block-lime font-bold">Pub/Sub Fan-Out Wire</span>
                <span className="text-white/60">JSON Envelope</span>
              </div>
              <pre className="text-[11px] text-white/90 overflow-x-auto p-2 bg-black/40 rounded-lg">
{`{
  "type": "result",
  "data": {
    "verdict": "pass",
    "tests_passed": 3,
    "total_tests": 3,
    "outcome": "win"
  }
}`}
              </pre>
            </div>
          </div>
        </ColorBlock>
      </section>

      {/* Return to Canvas Space */}
      <div className="h-12 md:h-20" />

      {/* Color Block Section 3: Docker Sandbox (Coral Ground) */}
      <section className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 my-16 md:my-24">
        <ColorBlock variant="coral">
          <div className="grid grid-cols-1 lg:grid-cols-12 gap-8 items-center">
            <div className="lg:col-span-7 space-y-4">
              <span className="font-mono text-xs uppercase tracking-caption font-bold text-neutral-800">
                03 / Isolated Sandbox
              </span>
              <h2 className="font-headline text-3xl md:text-5xl text-ink font-normal tracking-tight">
                Hostile by default. Hard Docker containment.
              </h2>
              <p className="font-sans text-lg text-neutral-800 font-normal leading-relaxed">
                Every single submission runs in a fresh, isolated Docker container: zero network access,
                read-only root filesystem, dropped kernel capabilities, PID limits, and strict CPU and
                wall-clock execution timeouts.
              </p>
            </div>
            <div className="lg:col-span-5 grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div className="bg-canvas p-4 rounded-[16px] text-center border border-black/5 shadow-xs">
                <div className="font-mono text-xs font-bold text-ink">PYTHON</div>
                <div className="font-sans text-xs text-neutral-500 mt-1">3.13 Pinned</div>
              </div>
              <div className="bg-canvas p-4 rounded-[16px] text-center border border-black/5 shadow-xs">
                <div className="font-mono text-xs font-bold text-ink">C++</div>
                <div className="font-sans text-xs text-neutral-500 mt-1">GCC 14 (O2)</div>
              </div>
              <div className="bg-canvas p-4 rounded-[16px] text-center border border-black/5 shadow-xs">
                <div className="font-mono text-xs font-bold text-ink">JAVA</div>
                <div className="font-sans text-xs text-neutral-500 mt-1">Temurin 21</div>
              </div>
            </div>
          </div>
        </ColorBlock>
      </section>

      {/* Spacing */}
      <div className="h-12 md:h-20" />

      {/* Lilac Promo CTA Block */}
      <section className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 my-16">
        <ColorBlock variant="lilac">
          <div className="flex flex-col md:flex-row items-center justify-between gap-6">
            <div className="space-y-2 text-center md:text-left">
              <span className="font-mono text-xs uppercase tracking-caption font-bold text-neutral-800">
                Enter the Arena
              </span>
              <h3 className="font-headline text-2xl md:text-4xl text-ink font-normal tracking-tight">
                Are your coding skills ready for a 1v1 duel?
              </h3>
              <p className="font-sans text-sm text-neutral-700">
                Jump straight in. Match with an opponent in seconds.
              </p>
            </div>
            <Button
              variant="magenta"
              onClick={onEnterArena}
              icon={<ArrowRight className="w-4 h-4 text-white" />}
              className="px-8 py-3.5 text-base flex-shrink-0"
            >
              Start Dueling Now
            </Button>
          </div>
        </ColorBlock>
      </section>
    </div>
  );
};
