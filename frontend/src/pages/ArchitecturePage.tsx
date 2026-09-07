import React from 'react';
import { ColorBlock } from '../components/common/ColorBlock';
import { Server, Database, Cpu, Shield, RefreshCw, Terminal, CheckCircle2, Lock } from 'lucide-react';

export const ArchitecturePage: React.FC = () => {
  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-10 md:py-16 space-y-16">
      {/* Title */}
      <div className="max-w-3xl space-y-3">
        <div className="flex items-center gap-2 text-xs font-mono uppercase tracking-caption text-neutral-500">
          <span>System Architecture</span>
          <span>•</span>
          <span>Single Binary, Five Roles</span>
        </div>
        <h1 className="font-display-lg text-4xl md:text-5xl text-ink font-normal tracking-tight">
          How CodeDuel Works
        </h1>
        <p className="font-subhead text-neutral-700 font-normal leading-relaxed">
          CodeDuel is designed as one compiled Go binary with role flags sharing one Redis instance
          and one PostgreSQL database. Correctness under heavy concurrency is guaranteed by three core primitives.
        </p>
      </div>

      {/* Triad Color Block (Navy) */}
      <ColorBlock variant="navy">
        <span className="font-mono text-xs uppercase tracking-caption font-bold text-block-lime">
          Core Primitives
        </span>
        <h2 className="font-headline text-3xl md:text-4xl text-white font-normal mt-2 mb-6">
          The Concurrency Triad
        </h2>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
          <div className="p-5 rounded-[18px] bg-white/10 backdrop-blur-sm border border-white/15 space-y-3">
            <div className="w-8 h-8 rounded-full bg-block-lime text-ink flex items-center justify-center font-mono font-bold text-sm">
              1
            </div>
            <h3 className="font-card-title text-lg text-white">Lua Pop-Pair</h3>
            <p className="text-xs text-white/80 leading-relaxed font-sans">
              Redis ZSET queue scored by enqueue timestamp. Lua executes server-side atomically,
              popping the two oldest players without race conditions or double-pairing.
            </p>
          </div>

          <div className="p-5 rounded-[18px] bg-white/10 backdrop-blur-sm border border-white/15 space-y-3">
            <div className="w-8 h-8 rounded-full bg-block-lilac text-ink flex items-center justify-center font-mono font-bold text-sm">
              2
            </div>
            <h3 className="font-card-title text-lg text-white">Streams & Reaper</h3>
            <p className="text-xs text-white/80 leading-relaxed font-sans">
              Judge jobs live in Redis Streams. Workers claim entries in consumer groups. If a worker
              crashes, the Reaper reclaims abandoned PEL entries and redispatches work.
            </p>
          </div>

          <div className="p-5 rounded-[18px] bg-white/10 backdrop-blur-sm border border-white/15 space-y-3">
            <div className="w-8 h-8 rounded-full bg-block-mint text-ink flex items-center justify-center font-mono font-bold text-sm">
              3
            </div>
            <h3 className="font-card-title text-lg text-white">Conditional Winner UPDATE</h3>
            <p className="text-xs text-white/80 leading-relaxed font-sans">
              On a passing submission, PostgreSQL row locking guarantees atomicity:
              1 row updated = you won; 0 rows updated = opponent already won.
            </p>
          </div>
        </div>
      </ColorBlock>

      {/* The 5 Roles Grid (Cream ground) */}
      <div className="space-y-6">
        <h2 className="font-headline text-2xl md:text-3xl text-ink font-normal tracking-tight">
          Role Architecture (<code className="font-mono text-xl bg-surface-soft px-2 py-0.5 rounded">--role</code>)
        </h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          <div className="p-6 rounded-[20px] bg-surface-soft border border-hairline space-y-3">
            <div className="flex items-center gap-3">
              <Server className="w-5 h-5 text-ink" />
              <h3 className="font-card-title text-lg text-ink">API Gateway</h3>
            </div>
            <p className="text-xs text-neutral-600 font-sans leading-relaxed">
              Stateless WebSocket hub & REST auth gateway. Fans out events across per-user Redis channels,
              enforcing 64KB message limits and JWT authorization before socket upgrade.
            </p>
            <div className="pt-2 font-mono text-[11px] text-neutral-500">
              Listen: <code className="bg-canvas px-1 rounded border border-hairline">:8080</code>
            </div>
          </div>

          <div className="p-6 rounded-[20px] bg-surface-soft border border-hairline space-y-3">
            <div className="flex items-center gap-3">
              <RefreshCw className="w-5 h-5 text-ink" />
              <h3 className="font-card-title text-lg text-ink">Match Service</h3>
            </div>
            <p className="text-xs text-neutral-600 font-sans leading-relaxed">
              Pops paired players via Redis Lua, inserts active matches into PostgreSQL with synchronized
              deadlines, and publishes <code className="bg-canvas px-1 rounded">match_start</code> events.
            </p>
            <div className="pt-2 font-mono text-[11px] text-neutral-500">
              State: Stateless, Horizontal
            </div>
          </div>

          <div className="p-6 rounded-[20px] bg-surface-soft border border-hairline space-y-3">
            <div className="flex items-center gap-3">
              <Cpu className="w-5 h-5 text-ink" />
              <h3 className="font-card-title text-lg text-ink">Judge Worker</h3>
            </div>
            <p className="text-xs text-neutral-600 font-sans leading-relaxed">
              Consumes jobs from Redis Stream <code className="bg-canvas px-1 rounded">codeduel:judge:jobs</code>, executes code
              in untrusted Docker sandboxes, and commits verdicts idempotently.
            </p>
            <div className="pt-2 font-mono text-[11px] text-neutral-500">
              Isolation: Zero-Network Docker
            </div>
          </div>

          <div className="p-6 rounded-[20px] bg-surface-soft border border-hairline space-y-3">
            <div className="flex items-center gap-3">
              <Shield className="w-5 h-5 text-ink" />
              <h3 className="font-card-title text-lg text-ink">Reaper / Sweeper</h3>
            </div>
            <p className="text-xs text-neutral-600 font-sans leading-relaxed">
              Self-healing background worker guarded by PostgreSQL advisory locks. Reclaims stuck jobs,
              re-queues unacknowledged PEL entries, and finalizes expired match deadlines.
            </p>
            <div className="pt-2 font-mono text-[11px] text-neutral-500">
              Coordination: Advisory Lock
            </div>
          </div>

          <div className="p-6 rounded-[20px] bg-surface-soft border border-hairline space-y-3">
            <div className="flex items-center gap-3">
              <Database className="w-5 h-5 text-ink" />
              <h3 className="font-card-title text-lg text-ink">Migrate</h3>
            </div>
            <p className="text-xs text-neutral-600 font-sans leading-relaxed">
              Applies embedded database migrations in numerical order (<code className="bg-canvas px-1 rounded">000001_init</code> to <code className="bg-canvas px-1 rounded">000008</code>),
              enforcing table invariants and foreign keys.
            </p>
            <div className="pt-2 font-mono text-[11px] text-neutral-500">
              Engine: pgx / golang-migrate
            </div>
          </div>
        </div>
      </div>

      {/* Docker Sandbox Specs (Coral Ground) */}
      <ColorBlock variant="coral">
        <span className="font-mono text-xs uppercase tracking-caption font-bold text-neutral-800">
          Security Boundary
        </span>
        <h2 className="font-headline text-3xl md:text-4xl text-ink font-normal mt-2 mb-4">
          Docker Untrusted Execution Sandbox
        </h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mt-6">
          <div className="p-4 rounded-[16px] bg-canvas border border-black/5">
            <div className="font-mono text-xs font-bold text-neutral-800 uppercase">Network</div>
            <div className="font-sans text-sm font-semibold text-red-600 mt-1">None (--net=none)</div>
            <p className="text-xs text-neutral-500 mt-1">Zero socket or external connectivity</p>
          </div>
          <div className="p-4 rounded-[16px] bg-canvas border border-black/5">
            <div className="font-mono text-xs font-bold text-neutral-800 uppercase">Filesystem</div>
            <div className="font-sans text-sm font-semibold text-neutral-800 mt-1">Read-Only Root</div>
            <p className="text-xs text-neutral-500 mt-1">Temporary tmpfs for compilation</p>
          </div>
          <div className="p-4 rounded-[16px] bg-canvas border border-black/5">
            <div className="font-mono text-xs font-bold text-neutral-800 uppercase">PID Limits</div>
            <div className="font-sans text-sm font-semibold text-neutral-800 mt-1">--pids-limit=64</div>
            <p className="text-xs text-neutral-500 mt-1">Fork bombs neutralised immediately</p>
          </div>
          <div className="p-4 rounded-[16px] bg-canvas border border-black/5">
            <div className="font-mono text-xs font-bold text-neutral-800 uppercase">Execution Limit</div>
            <div className="font-sans text-sm font-semibold text-neutral-800 mt-1">Wall Clock Kill</div>
            <p className="text-xs text-neutral-500 mt-1">Hard kill past duration threshold</p>
          </div>
        </div>
      </ColorBlock>
    </div>
  );
};
