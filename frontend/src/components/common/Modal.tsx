import React, { useEffect } from 'react';
import { X } from 'lucide-react';
import { Button } from './Button';

interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  title?: string;
  children: React.ReactNode;
  maxWidth?: string;
}

export const Modal: React.FC<ModalProps> = ({
  isOpen,
  onClose,
  title,
  children,
  maxWidth = 'max-w-md',
}) => {
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen) {
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    if (isOpen) {
      document.body.style.overflow = 'hidden';
    }
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      document.body.style.overflow = 'unset';
    };
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      {/* Scrim backdrop */}
      <div
        className="fixed inset-0 bg-black/60 transition-opacity animate-fadeIn"
        onClick={onClose}
      />

      {/* Modal card */}
      <div
        className={`relative w-full ${maxWidth} bg-canvas rounded-[24px] p-6 md:p-8 border border-hairline shadow-2xl z-10`}
      >
        <div className="flex items-center justify-between pb-4 border-b border-hairline">
          {title ? (
            <h3 className="font-card-title text-ink">{title}</h3>
          ) : (
            <div />
          )}
          <Button
            variant="icon-circular"
            onClick={onClose}
            aria-label="Close modal"
          >
            <X className="w-5 h-5 text-ink" />
          </Button>
        </div>

        <div className="mt-4">{children}</div>
      </div>
    </div>
  );
};
