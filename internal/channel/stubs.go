package channel

import "context"

// stubAdapter is a placeholder for channels not yet implemented.
type stubAdapter struct {
	name string
}

// NewStubAdapter returns a channel that returns ErrNotImplemented for all operations.
func NewStubAdapter(name string) Channel {
	return &stubAdapter{name: name}
}

func (s *stubAdapter) Name() string { return s.name }

func (s *stubAdapter) Start(_ context.Context, _ MessageHandler) error {
	return ErrNotImplemented
}

func (s *stubAdapter) Stop(_ context.Context) error {
	return ErrNotImplemented
}

func (s *stubAdapter) Send(_ context.Context, _ string, _ OutgoingMessage) error {
	return ErrNotImplemented
}

func (s *stubAdapter) SendTyping(_ context.Context, _ string) error {
	return ErrNotImplemented
}
