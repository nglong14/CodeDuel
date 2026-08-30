package judge

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/nglong14/CodeDuel/internal/config"
)

type Language string

const (
	LanguagePython Language = "python"
	LanguageCPP    Language = "cpp"
	LanguageJava   Language = "java"
)

type OutcomeKind string

const (
	OutcomePass         OutcomeKind = "pass"
	OutcomeWrongAnswer  OutcomeKind = "wrong_answer"
	OutcomeCompileError OutcomeKind = "compile_error"
	OutcomeRuntimeError OutcomeKind = "runtime_error"
	OutcomeOutputLimit  OutcomeKind = "output_limit"
	OutcomeTimeout      OutcomeKind = "timeout"
)

type TestCase struct {
	Input    []byte
	Expected []byte
}

type Limits struct {
	MaxCodeBytes   int64
	MaxOutputBytes int64
	CompileTimeout time.Duration
	TestTimeout    time.Duration
	TotalTimeout   time.Duration
	CleanupTimeout time.Duration
	NanoCPUs       int64
	MemoryBytes    int64
	MemorySwap     int64
	PIDLimit       int64
	WorkspaceBytes int64
	TmpfsBytes     int64
}

func (l Limits) Validate() error {
	if l.MaxCodeBytes <= 0 || l.MaxOutputBytes <= 0 {
		return errors.New("code and output limits must be positive")
	}
	if l.CompileTimeout <= 0 || l.TestTimeout <= 0 || l.TotalTimeout <= 0 || l.CleanupTimeout <= 0 {
		return errors.New("sandbox timeouts must be positive")
	}
	if l.CompileTimeout > l.TotalTimeout || l.TestTimeout > l.TotalTimeout {
		return errors.New("compile and test timeouts must not exceed total timeout")
	}
	if l.NanoCPUs <= 0 || l.MemoryBytes <= 0 || l.PIDLimit <= 0 || l.WorkspaceBytes <= 0 || l.TmpfsBytes <= 0 {
		return errors.New("sandbox resource limits must be positive")
	}
	if l.MemorySwap < l.MemoryBytes {
		return errors.New("memory plus swap limit must be at least the memory limit")
	}
	return nil
}

type ExecutionRequest struct {
	Language Language
	Source   []byte
	Tests    []TestCase
	Limits   Limits
}

func (r ExecutionRequest) Validate() error {
	if err := r.Limits.Validate(); err != nil {
		return fmt.Errorf("validate limits: %w", err)
	}
	switch r.Language {
	case LanguagePython, LanguageCPP, LanguageJava:
	default:
		return fmt.Errorf("unsupported language %q", r.Language)
	}
	if len(r.Source) == 0 || int64(len(r.Source)) > r.Limits.MaxCodeBytes {
		return errors.New("source is empty or exceeds the configured limit")
	}
	if !utf8.Valid(r.Source) {
		return errors.New("source is not valid UTF-8")
	}
	if len(r.Tests) == 0 {
		return errors.New("at least one test case is required")
	}
	return nil
}

type ExecutionOutcome struct {
	Kind        OutcomeKind
	TestsPassed int
}

type Executor interface {
	Execute(context.Context, ExecutionRequest) (ExecutionOutcome, error)
}

func limitsFromConfig(cfg config.JudgeConfig) Limits {
	return Limits{
		MaxCodeBytes:   cfg.MaxCodeBytes,
		MaxOutputBytes: cfg.MaxOutputBytes,
		CompileTimeout: cfg.CompileTimeout,
		TestTimeout:    cfg.TestTimeout,
		TotalTimeout:   cfg.TotalTimeout,
		CleanupTimeout: cfg.CleanupTimeout,
		NanoCPUs:       cfg.NanoCPUs,
		MemoryBytes:    cfg.MemoryBytes,
		MemorySwap:     cfg.MemorySwapBytes,
		PIDLimit:       cfg.PIDLimit,
		WorkspaceBytes: cfg.WorkspaceBytes,
		TmpfsBytes:     cfg.TmpfsBytes,
	}
}
