package accessibility

import "github.com/godbus/dbus/v5"

// Test-only export of the macOS script builder so the external _test
// package can assert the generated AppleScript without running osascript.
func BuildMacScript(app string, depth int) string { return buildMacScript(app, depth) }

// Test-only exports for the Windows UIAutomation backend: the PowerShell
// script body (asserted for shape / no app-name interpolation) and the
// binary resolver.
const WinUIAScript = winUIAScript

func ResolvePowerShell() string { return resolvePowerShell() }

// VariantString exposes the Linux AT-SPI property-variant formatter so the
// external _test package can assert string/number/empty rendering without a
// live a11y bus.
func VariantString(v dbus.Variant) string { return variantString(v) }
