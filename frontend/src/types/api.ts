export interface User {
  id: string;
  email: string;
  display_name: string;
  created_at: string;
}

export interface SessionResponse {
  token: string;
  expires_at: string;
  user: User;
}

export interface MeResponse {
  user: User;
}

export interface ProblemSnapshot {
  id: string;
  title: string;
  statement: string;
  total_tests: number;
}

export interface PlayerSnapshot {
  id: string;
  display_name: string;
  slot: number;
  best_tests_passed: number;
}

export type SubmissionStatus = 'pending' | 'running' | 'completed';
export type SubmissionVerdict = 'pass' | 'fail' | 'error' | 'timeout' | 'failed';
export type SubmissionFailureKind =
  | 'wrong_answer'
  | 'compile_error'
  | 'runtime_error'
  | 'output_limit'
  | 'infrastructure_error';

export interface SubmissionSnapshot {
  id: string;
  request_id: string;
  language: 'python' | 'cpp' | 'java';
  status: SubmissionStatus;
  verdict: SubmissionVerdict | null;
  failure_kind: SubmissionFailureKind | null;
  tests_passed: number;
  created_at: string;
  finished_at: string | null;
}

export type MatchStatus = 'active' | 'finished';
export type MatchOutcome = 'win' | 'loss' | 'draw';

export interface MatchSnapshot {
  id: string;
  status: MatchStatus;
  created_at: string;
  deadline: string;
  winner_id: string | null;
  outcome: MatchOutcome | null;
  server_time: string;
  problem: ProblemSnapshot;
  players: PlayerSnapshot[];
  submissions: SubmissionSnapshot[];
  submissions_truncated: boolean;
}

export interface MatchResponse {
  match: MatchSnapshot;
}

export interface ApiErrorResponse {
  error: {
    code: string;
    message: string;
  };
}
