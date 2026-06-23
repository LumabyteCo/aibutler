package accessibility

import (
	"context"
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
)

// AT-SPI2 D-Bus constants. The accessibility tree is exposed over a
// dedicated "a11y" bus (separate from the session bus); each accessible
// object is addressed by a (bus-name, object-path) pair and implements
// org.a11y.atspi.Accessible.
const (
	atspiRegistryService = "org.a11y.atspi.Registry"
	atspiRootPath        = "/org/a11y/atspi/accessible/root"
	atspiAccessibleIface = "org.a11y.atspi.Accessible"
	dbusPropertiesIface  = "org.freedesktop.DBus.Properties"
)

// axRef is an AT-SPI object reference: the (so) struct returned by
// GetChildren / Parent — a bus name plus an object path.
type axRef struct {
	Name string
	Path dbus.ObjectPath
}

// readLinux walks the AT-SPI2 accessibility tree for the named application
// and emits the same tab-delimited "<indent><role>\t<name>\t<value>" shape
// as the macOS and Windows backends.
//
// It connects to the a11y bus (whose address is published on the session
// bus via org.a11y.Bus.GetAddress), finds the application whose top-level
// Name matches app, and recurses its children to depth. Requires a running
// AT-SPI environment (at-spi2-core) and an accessibility-exposing toolkit
// (GTK/Qt); without one it returns a clear, actionable error.
func (r *Reader) readLinux(ctx context.Context, app string, depth int) (string, error) {
	execCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	conn, err := dialA11yBus(execCtx)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	root := axRef{Name: atspiRegistryService, Path: atspiRootPath}
	apps, err := getChildren(execCtx, conn, root)
	if err != nil {
		return "", fmt.Errorf("accessibility.read: enumerating applications: %w", err)
	}

	var match *axRef
	for i := range apps {
		name, _ := getName(execCtx, conn, apps[i])
		if strings.EqualFold(strings.TrimSpace(name), app) {
			match = &apps[i]
			break
		}
	}
	if match == nil {
		return "", fmt.Errorf("accessibility.read: no UI elements found for %q (is it running with an accessible window?)", app)
	}

	var b strings.Builder
	if err := walkA11y(execCtx, conn, *match, 0, depth, &b); err != nil {
		return "", err
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", fmt.Errorf("accessibility.read: no UI elements found for %q (is it running with an accessible window?)", app)
	}
	return out, nil
}

// dialA11yBus resolves the AT-SPI bus address from the session bus and
// connects to it. The a11y bus is a private message bus; the session bus
// publishes its address via org.a11y.Bus.GetAddress.
func dialA11yBus(ctx context.Context) (*dbus.Conn, error) {
	session, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("accessibility.read: no D-Bus session bus (is a desktop session running?): %w", err)
	}
	defer session.Close()

	var addr string
	obj := session.Object("org.a11y.Bus", "/org/a11y/bus")
	if err := obj.CallWithContext(ctx, "org.a11y.Bus.GetAddress", 0).Store(&addr); err != nil {
		return nil, fmt.Errorf("accessibility.read: AT-SPI bus unavailable — install/enable at-spi2-core (org.a11y.Bus.GetAddress failed): %w", err)
	}
	if strings.TrimSpace(addr) == "" {
		return nil, fmt.Errorf("accessibility.read: AT-SPI returned an empty bus address — at-spi2-core may not be running")
	}

	conn, err := dbus.Dial(addr)
	if err != nil {
		return nil, fmt.Errorf("accessibility.read: dialing AT-SPI bus: %w", err)
	}
	if err := conn.Auth(nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("accessibility.read: authenticating to AT-SPI bus: %w", err)
	}
	if err := conn.Hello(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("accessibility.read: AT-SPI bus handshake: %w", err)
	}
	return conn, nil
}

// walkA11y recursively emits one line per accessible element from el down
// to maxDepth, matching the indent/role/name/value format of the other
// backends.
func walkA11y(ctx context.Context, conn *dbus.Conn, el axRef, level, maxDepth int, b *strings.Builder) error {
	if level >= maxDepth {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("accessibility.read: timeout walking tree: %w", err)
	}
	children, err := getChildren(ctx, conn, el)
	if err != nil {
		// A child that vanished or denies introspection shouldn't abort the
		// whole walk; just stop descending this branch.
		return nil //nolint:nilerr
	}
	indent := strings.Repeat("· ", level)
	for _, c := range children {
		role := getRoleName(ctx, conn, c)
		name, value := getNameValue(ctx, conn, c)
		b.WriteString(indent)
		b.WriteString(role)
		b.WriteByte('\t')
		b.WriteString(name)
		b.WriteByte('\t')
		b.WriteString(value)
		b.WriteByte('\n')
		if err := walkA11y(ctx, conn, c, level+1, maxDepth, b); err != nil {
			return err
		}
	}
	return nil
}

// getChildren returns el's accessible children via the AT-SPI
// Accessible.GetChildren method.
func getChildren(ctx context.Context, conn *dbus.Conn, el axRef) ([]axRef, error) {
	var children []axRef
	obj := conn.Object(el.Name, el.Path)
	err := obj.CallWithContext(ctx, atspiAccessibleIface+".GetChildren", 0).Store(&children)
	return children, err
}

// getRoleName returns el's human-readable role (e.g. "push button",
// "frame"); falls back to "element" on error so output stays well-formed.
func getRoleName(ctx context.Context, conn *dbus.Conn, el axRef) string {
	var role string
	obj := conn.Object(el.Name, el.Path)
	if err := obj.CallWithContext(ctx, atspiAccessibleIface+".GetRoleName", 0).Store(&role); err != nil || role == "" {
		return "element"
	}
	return role
}

// getName reads only the Name property — used when matching the top-level
// application node.
func getName(ctx context.Context, conn *dbus.Conn, el axRef) (string, error) {
	return getProp(ctx, conn, el, atspiAccessibleIface, "Name")
}

// getNameValue reads the Name property and, best-effort, a numeric Value.
// AT-SPI exposes editable/range values via the org.a11y.atspi.Value
// interface (CurrentValue, a double); plain text content lives elsewhere,
// so the value column carries any numeric value when present.
func getNameValue(ctx context.Context, conn *dbus.Conn, el axRef) (name, value string) {
	name, _ = getProp(ctx, conn, el, atspiAccessibleIface, "Name")
	if raw, err := getProp(ctx, conn, el, "org.a11y.atspi.Value", "CurrentValue"); err == nil {
		value = strings.TrimSpace(raw)
	}
	return name, value
}

// getProp reads a property via the standard D-Bus Properties.Get. Properties
// come back wrapped in a variant, so the reply is stored into a Variant and
// its concrete value rendered as a string.
func getProp(ctx context.Context, conn *dbus.Conn, el axRef, iface, prop string) (string, error) {
	var variant dbus.Variant
	obj := conn.Object(el.Name, el.Path)
	if err := obj.CallWithContext(ctx, dbusPropertiesIface+".Get", 0,
		iface, prop).Store(&variant); err != nil {
		return "", err
	}
	return variantString(variant), nil
}

// variantString renders a property variant as a string: D-Bus strings pass
// through; numbers are formatted without trailing zeros; a zero numeric
// value (the common "no value" case for range widgets) renders as empty.
func variantString(v dbus.Variant) string {
	switch x := v.Value().(type) {
	case string:
		return x
	case float64:
		if x == 0 {
			return ""
		}
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", x), "0"), ".")
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", x)
	}
}
