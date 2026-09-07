import React from 'react';

export const MarqueeStrip: React.FC = () => {
  const items = [
    '1V1 REAL-TIME DUELS',
    'REDIS FIFO MATCHMAKING',
    'DOCKER SANDBOX ISOLATION',
    'ATOMIC WINNER SELECTION',
    'POSTGRES ADVISORY LOCKS',
    'SUB-MILLISECOND EVENT FANOUT',
    'PYTHON • C++ • JAVA',
    'SELF-HEALING REAPER',
  ];

  return (
    <div className="w-full h-9 bg-inverse-canvas text-inverse-ink overflow-hidden flex items-center border-y border-black">
      <div className="animate-marquee whitespace-nowrap flex items-center font-mono text-xs uppercase tracking-caption font-medium">
        {/* Repeating sequence for infinite marquee scroll */}
        {[...items, ...items, ...items, ...items].map((item, idx) => (
          <span key={idx} className="inline-flex items-center">
            <span className="mx-4 text-white/90">{item}</span>
            <span className="text-block-lime font-bold">✦</span>
          </span>
        ))}
      </div>
    </div>
  );
};
