export type SupportedLanguage = 'python' | 'cpp' | 'java';

export interface Envelope<T = unknown> {
  type: string;
  data?: T;
}

export interface JoinQueueData {}

export interface SubmitCodeData {
  match_id: string;
  request_id: string;
  language: SupportedLanguage;
  code: string;
}

export interface ReadyData {
  user_id: string;
}

export interface QueuedData {}

export interface MatchStartData {
  match_id: string;
  problem_id: string;
  deadline: string; // ISO 8601
}

export interface JudgingData {
  request_id: string;
  submission_id: string;
}

export interface ResultData {
  event_id: string;
  request_id: string;
  submission_id: string;
  match_id: string;
  player_id: string;
  verdict: 'pass' | 'fail' | 'error' | 'timeout' | 'failed';
  tests_passed: number;
  total_tests: number;
  winner_id?: string;
  outcome?: 'win' | 'loss' | 'draw';
}

export interface MatchEndData {
  event_id: string;
  match_id: string;
  winner_id?: string;
  outcome: 'win' | 'loss' | 'draw';
  tests_passed: number;
  total_tests: number;
}

export interface ErrorData {
  code?: string;
  message: string;
  match_id?: string;
  request_id?: string;
}
