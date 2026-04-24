package shell

import "context"

// ApprovalHandler is the interface for interactive command approval.
// Channel implementations (Slack, Telegram, etc.) provide concrete handlers.
type ApprovalHandler interface {
	RequestApproval(ctx context.Context, command, cmdName string) (bool, error)
}

// ApprovalFunc adapts a function into an ApprovalHandler.
type ApprovalFunc func(ctx context.Context, command, cmdName string) (bool, error)

// RequestApproval implements ApprovalHandler.
func (f ApprovalFunc) RequestApproval(ctx context.Context, command, cmdName string) (bool, error) {
	return f(ctx, command, cmdName)
}

// AlwaysDeny returns an approval handler that always denies.
func AlwaysDeny() ApprovalHandler {
	return ApprovalFunc(func(_ context.Context, _, _ string) (bool, error) {
		return false, nil
	})
}

// AlwaysApprove returns an approval handler that always approves. For testing only.
func AlwaysApprove() ApprovalHandler {
	return ApprovalFunc(func(_ context.Context, _, _ string) (bool, error) {
		return true, nil
	})
}
