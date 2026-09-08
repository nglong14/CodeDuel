import React from 'react';
import { Swords, ArrowRight } from 'lucide-react';
import { Button } from '../components/common/Button';
import { ColorBlock } from '../components/common/ColorBlock';
import { useAuth } from '../context/AuthContext';

interface LandingPageProps {
  onEnterArena: () => void;
}

export const LandingPage: React.FC<LandingPageProps> = ({
  onEnterArena,
}) => {
  const { user } = useAuth();

  return (
    <div className="w-full">
      {/* Editorial White Hero */}
      <section className="py-20 md:py-32 px-4 sm:px-6 lg:px-8 max-w-7xl mx-auto">
        <div className="max-w-4xl space-y-8">
          <div className="inline-flex items-center gap-2 px-3.5 py-1 rounded-full bg-surface-soft border border-hairline text-xs font-mono font-medium text-neutral-800">
            <span className="w-2 h-2 rounded-full bg-semantic-success" />
            LIVE 1V1 CODING DUELS
          </div>

          <h1 className="font-display-xl text-ink tracking-tight font-normal">
            Real-time 1v1 competitive coding.
          </h1>

          <p className="font-subhead text-neutral-800 max-w-2xl font-normal leading-relaxed">
            Two developers, one coding challenge, and ten minutes on the clock.
            Submit a solution that passes every test before your opponent does.
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
          </div>
        </div>
      </section>

       {/* Matchmaking overview */}
      <section className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 my-16 md:my-24">
        <ColorBlock variant="lime">
          <div className="grid grid-cols-1 lg:grid-cols-12 gap-8 items-center">
            <div className="lg:col-span-7 space-y-4">
              <span className="font-mono text-xs uppercase tracking-caption font-bold text-neutral-800">
                01 / Find Your Match
              </span>
              <h2 className="font-headline text-3xl md:text-5xl text-ink font-normal tracking-tight">
                A new challenge is one click away.
              </h2>
              <p className="font-sans text-lg text-neutral-800 font-normal leading-relaxed">
                Join the pool and we will pair you with another developer. Both players receive the
                same problem and the same countdown.
              </p>
            </div>
            <div className="lg:col-span-5 bg-canvas rounded-[20px] p-6 border border-black/5 shadow-xs space-y-4 font-mono text-xs">
              <div className="flex items-center justify-between pb-3 border-b border-hairline">
                <span className="text-neutral-500 font-semibold uppercase tracking-caption">Duel Brief</span>
                <span className="text-semantic-success font-bold">READY</span>
              </div>
              <div className="bg-surface-soft p-3 rounded-lg text-neutral-700 space-y-1">
                <div>Challenge: shared problem</div>
                <div>Clock: 10 minutes</div>
                <div>Goal: pass every test</div>
                <div className="text-semantic-success">Winner: first full pass</div>
              </div>
            </div>
          </div>
        </ColorBlock>
      </section>

      {/* Return to Canvas Space */}
      <div className="h-12 md:h-20" />

       {/* Duel overview */}
      <section className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 my-16 md:my-24">
        <ColorBlock variant="navy">
          <div className="grid grid-cols-1 lg:grid-cols-12 gap-8 items-center">
            <div className="lg:col-span-7 space-y-4">
              <span className="font-mono text-xs uppercase tracking-caption font-bold text-block-lime">
                02 / Race the Clock
              </span>
              <h2 className="font-headline text-3xl md:text-5xl text-white font-normal tracking-tight">
                See every submission as it lands.
              </h2>
              <p className="font-sans text-lg text-white/80 font-normal leading-relaxed">
                Keep an eye on the countdown, your best score, and each completed submission while
                you work toward a full solution.
              </p>
              <div className="pt-2 flex items-center gap-4 text-xs font-mono text-white/60">
                <span>Shared challenge</span>
                <span>•</span>
                <span>Live results</span>
                <span>•</span>
                <span>One winner</span>
              </div>
            </div>
            <div className="lg:col-span-5 bg-white/10 backdrop-blur-sm rounded-[20px] p-6 border border-white/15 space-y-3 font-mono text-xs text-white">
              <div className="flex items-center justify-between pb-2 border-b border-white/20">
                <span className="uppercase tracking-caption text-block-lime font-bold">Match Progress</span>
                <span className="text-white/60">LIVE</span>
              </div>
              <pre className="text-[11px] text-white/90 overflow-x-auto p-2 bg-black/40 rounded-lg">
{`YOU                 OPPONENT
2 / 3 tests         1 / 3 tests

Time remaining       04:32
Keep coding. Every test counts.`}
              </pre>
            </div>
          </div>
        </ColorBlock>
      </section>

      {/* Return to Canvas Space */}
      <div className="h-12 md:h-20" />

       {/* Language overview */}
      <section className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 my-16 md:my-24">
        <ColorBlock variant="coral">
          <div className="grid grid-cols-1 lg:grid-cols-12 gap-8 items-center">
            <div className="lg:col-span-7 space-y-4">
              <span className="font-mono text-xs uppercase tracking-caption font-bold text-neutral-800">
                03 / Choose Your Language
              </span>
              <h2 className="font-headline text-3xl md:text-5xl text-ink font-normal tracking-tight">
                Code in the language you know best.
              </h2>
              <p className="font-sans text-lg text-neutral-800 font-normal leading-relaxed">
                Start with a practical template for Python, C++, or Java, then focus on solving the
                problem before the clock runs out.
              </p>
            </div>
            <div className="lg:col-span-5 grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div className="bg-canvas p-4 rounded-[16px] text-center border border-black/5 shadow-xs">
                <div className="font-mono text-xs font-bold text-ink">PYTHON</div>
                <div className="font-sans text-xs text-neutral-500 mt-1">Fast and familiar</div>
              </div>
              <div className="bg-canvas p-4 rounded-[16px] text-center border border-black/5 shadow-xs">
                <div className="font-mono text-xs font-bold text-ink">C++</div>
                <div className="font-sans text-xs text-neutral-500 mt-1">Built for performance</div>
              </div>
              <div className="bg-canvas p-4 rounded-[16px] text-center border border-black/5 shadow-xs">
                <div className="font-mono text-xs font-bold text-ink">JAVA</div>
                <div className="font-sans text-xs text-neutral-500 mt-1">Ready for clean solutions</div>
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
