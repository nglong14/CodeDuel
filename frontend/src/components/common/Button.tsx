import React from 'react';

export type ButtonVariant =
  | 'primary'
  | 'secondary'
  | 'magenta'
  | 'icon-circular'
  | 'icon-circular-inverse'
  | 'ghost';

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  isLoading?: boolean;
  icon?: React.ReactNode;
  children?: React.ReactNode;
}

export const Button: React.FC<ButtonProps> = ({
  variant = 'primary',
  isLoading = false,
  icon,
  children,
  className = '',
  disabled,
  ...props
}) => {
  const baseClasses =
    'inline-flex items-center justify-center font-medium transition-all duration-150 select-none disabled:opacity-50 disabled:cursor-not-allowed';

  let variantClasses = '';
  switch (variant) {
    case 'primary':
      variantClasses =
        'bg-primary text-on-primary rounded-full px-5 py-2.5 min-h-[44px] text-base hover:opacity-90 active:scale-98 border border-transparent shadow-sm';
      break;
    case 'secondary':
      variantClasses =
        'bg-canvas text-ink border border-hairline rounded-full px-5 py-2 min-h-[44px] text-base hover:bg-surface-soft active:scale-98 shadow-sm';
      break;
    case 'magenta':
      variantClasses =
        'bg-accent-magenta text-on-primary rounded-full px-5 py-2.5 min-h-[44px] text-base hover:opacity-95 active:scale-98 shadow-sm';
      break;
    case 'icon-circular':
      variantClasses =
        'w-10 h-10 rounded-full bg-surface-soft text-ink border border-hairline hover:bg-hairline active:scale-95 p-0';
      break;
    case 'icon-circular-inverse':
      variantClasses =
        'w-10 h-10 rounded-full bg-on-inverse-soft text-inverse-ink border border-white/20 hover:bg-white/25 active:scale-95 p-0';
      break;
    case 'ghost':
      variantClasses =
        'text-ink hover:bg-surface-soft rounded-full px-4 py-2 min-h-[40px] text-base';
      break;
  }

  return (
    <button
      disabled={disabled || isLoading}
      className={`${baseClasses} ${variantClasses} ${className}`}
      {...props}
    >
      {isLoading ? (
        <span className="flex items-center gap-2">
          <svg
            className="animate-spin h-4 w-4 text-current"
            xmlns="http://www.w3.org/2000/svg"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle
              className="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              strokeWidth="4"
            />
            <path
              className="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            />
          </svg>
          <span>{children}</span>
        </span>
      ) : (
        <span className="flex items-center gap-2">
          {icon && <span className="flex-shrink-0">{icon}</span>}
          {children}
        </span>
      )}
    </button>
  );
};
