import React from 'react';

export type ColorBlockVariant =
  | 'lime'
  | 'lilac'
  | 'cream'
  | 'pink'
  | 'mint'
  | 'coral'
  | 'navy'
  | 'soft';

interface ColorBlockProps {
  variant?: ColorBlockVariant;
  className?: string;
  children: React.ReactNode;
}

const variantStyles: Record<ColorBlockVariant, string> = {
  lime: 'bg-block-lime text-ink',
  lilac: 'bg-block-lilac text-ink',
  cream: 'bg-block-cream text-ink',
  pink: 'bg-block-pink text-ink',
  mint: 'bg-block-mint text-ink',
  coral: 'bg-block-coral text-ink',
  navy: 'bg-block-navy text-inverse-ink',
  soft: 'bg-surface-soft text-ink border border-hairline',
};

export const ColorBlock: React.FC<ColorBlockProps> = ({
  variant = 'lime',
  className = '',
  children,
}) => {
  return (
    <div
      className={`w-full rounded-[24px] p-8 md:p-12 transition-all ${variantStyles[variant]} ${className}`}
    >
      {children}
    </div>
  );
};
