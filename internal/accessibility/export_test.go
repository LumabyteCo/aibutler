package accessibility

// Test-only export of the macOS script builder so the external _test
// package can assert the generated AppleScript without running osascript.
func BuildMacScript(app string, depth int) string { return buildMacScript(app, depth) }
