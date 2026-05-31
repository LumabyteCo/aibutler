package supervisor

import (
	"context"
	"errors"

	"github.com/LumabyteCo/aibutler/internal/mission"
)

// Replanner is the optional strategy the supervisor consults when a step
// fails. The default Supervisor has no Replanner — a failing step
// terminates the mission, preserving the v0.2.x default behaviour. Set
// Supervisor.Replanner to opt in to LLM-driven (or any other) replanning.
//
// A Replanner receives full context for the failure (the goal, what
// completed, what failed and why, what remained unstarted, and how many
// replan attempts have already been used) and returns a NEW sequence of
// steps to take from the failure point onward. The supervisor persists
// those via Manager.Replan and continues — the failed step stays in
// state=failed as historical fact in the audit log.
//
// A Replanner that returns ErrReplanRejected signals "no recovery is
// worth trying for this failure — fail the mission without consuming
// a replan attempt." The supervisor then takes the fail-fast path.
// Any other non-nil error is treated as a Replanner implementation
// failure; the supervisor falls back to failing the mission and the
// error is surfaced in the mission.failed reason.
//
// Replanner implementations should be safe to call concurrently — a
// single Supervisor is single-threaded today but the runtime may share
// one Replanner instance across many supervisors.
type Replanner interface {
	Replan(ctx context.Context, req ReplanRequest) ([]mission.Step, error)
}

// ReplanRequest is the input handed to a Replanner. All fields are
// snapshots — the Replanner must not mutate them.
type ReplanRequest struct {
	// MissionID is the mission being replanned.
	MissionID string
	// Goal is the original mission goal string (Mission.Goal).
	Goal string
	// CompletedSteps are steps that finished successfully before the
	// failure, in plan order. Each carries its Output so the Replanner
	// can build on prior work rather than redoing it.
	CompletedSteps []mission.Step
	// FailedStep is the step that just failed. Its State is StateFailed
	// and Error carries the failure reason as recorded by the worker.
	FailedStep mission.Step
	// FailureReason is the supervisor's framing of the failure (often
	// the same as FailedStep.Error but may include dispatch / timeout
	// context the worker couldn't observe).
	FailureReason string
	// RemainingSteps are steps that were planned but not yet started,
	// in plan order. The Replanner may choose to keep some, drop some,
	// or replace them all. The returned step list becomes the new
	// from-failure-onward sequence in full.
	RemainingSteps []mission.Step
	// PriorReplans is how many times this mission has already been
	// replanned (0 on the first attempt). The Replanner can use this
	// to bias toward conservative recovery as the count grows.
	PriorReplans int
}

// ErrReplanRejected is returned by a Replanner when it decides the
// failure is not recoverable and the mission should be failed without
// further replan attempts. Distinguished from a Replanner implementation
// error so the supervisor can choose to skip the "exhausted attempts"
// path and fail-fast immediately.
var ErrReplanRejected = errors.New("supervisor: replanner declined to replan")

// NoopReplanner is a Replanner that always rejects. Useful in tests and
// as documentation: passing NoopReplanner is semantically equivalent to
// leaving Supervisor.Replanner nil (both yield fail-on-step).
type NoopReplanner struct{}

// Replan satisfies Replanner by always returning ErrReplanRejected.
func (NoopReplanner) Replan(context.Context, ReplanRequest) ([]mission.Step, error) {
	return nil, ErrReplanRejected
}
