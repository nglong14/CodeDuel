import React, { createContext, useContext, useEffect, useState } from 'react';
import { wsClient, WSConnectionStatus } from '../services/websocket';
import {
  ErrorData,
  JudgingData,
  MatchEndData,
  MatchStartData,
  QueuedData,
  ResultData,
  SupportedLanguage,
} from '../types/proto';

interface WebSocketContextType {
  status: WSConnectionStatus;
  isQueued: boolean;
  isWaitingModalOpen: boolean;
  activeMatchId: string | null;
  latestMatchStart: MatchStartData | null;
  latestJudging: JudgingData | null;
  latestResult: ResultData | null;
  latestMatchEnd: MatchEndData | null;
  latestError: ErrorData | null;
  joinQueue: () => void;
  startMatchmaking: () => void;
  cancelMatchmaking: () => void;
  submitCode: (matchId: string, language: SupportedLanguage, code: string) => string;
  clearMatchState: () => void;
  clearQueueState: () => void;
  setIsWaitingModalOpen: (open: boolean) => void;
}

const WebSocketContext = createContext<WebSocketContextType | undefined>(undefined);

export const WebSocketProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [status, setStatus] = useState<WSConnectionStatus>(wsClient.getStatus());
  const [isQueued, setIsQueued] = useState(false);
  const [isWaitingModalOpen, setIsWaitingModalOpen] = useState(false);
  const [activeMatchId, setActiveMatchId] = useState<string | null>(null);
  const [latestMatchStart, setLatestMatchStart] = useState<MatchStartData | null>(null);
  const [latestJudging, setLatestJudging] = useState<JudgingData | null>(null);
  const [latestResult, setLatestResult] = useState<ResultData | null>(null);
  const [latestMatchEnd, setLatestMatchEnd] = useState<MatchEndData | null>(null);
  const [latestError, setLatestError] = useState<ErrorData | null>(null);

  useEffect(() => {
    const unsubStatus = wsClient.onStatusChange((newStatus) => {
      setStatus(newStatus);
      if (newStatus === 'disconnected') {
        setIsQueued(false);
      }
    });

    const unsubQueued = wsClient.on<QueuedData>('queued', () => {
      setIsQueued(true);
      setLatestError(null);
    });

    const unsubMatchStart = wsClient.on<MatchStartData>('match_start', (data) => {
      setIsQueued(false);
      setIsWaitingModalOpen(false);
      setActiveMatchId(data.match_id);
      setLatestMatchStart(data);
      setLatestMatchEnd(null);
      setLatestResult(null);
      setLatestJudging(null);
      setLatestError(null);
    });

    const unsubJudging = wsClient.on<JudgingData>('judging', (data) => {
      setLatestJudging(data);
    });

    const unsubResult = wsClient.on<ResultData>('result', (data) => {
      setLatestResult(data);
    });

    const unsubMatchEnd = wsClient.on<MatchEndData>('match_end', (data) => {
      setLatestMatchEnd(data);
    });

    const unsubError = wsClient.on<ErrorData>('error', (data) => {
      setLatestError(data);
      if (data.code === 'queue_unavailable' || data.code === 'already_in_match') {
        setIsQueued(false);
      }
    });

    return () => {
      unsubStatus();
      unsubQueued();
      unsubMatchStart();
      unsubJudging();
      unsubResult();
      unsubMatchEnd();
      unsubError();
    };
  }, []);

  const joinQueue = () => {
    setLatestError(null);
    setIsQueued(true);
    setIsWaitingModalOpen(true);
    try {
      wsClient.joinQueue();
    } catch (err) {
      console.warn('joinQueue warning:', err);
    }
  };

  const startMatchmaking = () => {
    joinQueue();
  };

  const cancelMatchmaking = () => {
    setIsQueued(false);
    setIsWaitingModalOpen(false);
  };

  const submitCode = (matchId: string, language: SupportedLanguage, code: string) => {
    return wsClient.submitCode(matchId, language, code);
  };

  const clearMatchState = () => {
    setActiveMatchId(null);
    setLatestMatchStart(null);
    setLatestJudging(null);
    setLatestResult(null);
    setLatestMatchEnd(null);
    setLatestError(null);
  };

  const clearQueueState = () => {
    setIsQueued(false);
    setIsWaitingModalOpen(false);
  };

  return (
    <WebSocketContext.Provider
      value={{
        status,
        isQueued,
        isWaitingModalOpen,
        activeMatchId,
        latestMatchStart,
        latestJudging,
        latestResult,
        latestMatchEnd,
        latestError,
        joinQueue,
        startMatchmaking,
        cancelMatchmaking,
        submitCode,
        clearMatchState,
        clearQueueState,
        setIsWaitingModalOpen,
      }}
    >
      {children}
    </WebSocketContext.Provider>
  );
};

export const useWS = (): WebSocketContextType => {
  const context = useContext(WebSocketContext);
  if (!context) {
    throw new Error('useWS must be used within a WebSocketProvider');
  }
  return context;
};
