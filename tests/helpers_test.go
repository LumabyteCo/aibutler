package tests

import "context"

// testCtx returns a background context for tests.
func testCtx() context.Context {
	return context.Background()
}
