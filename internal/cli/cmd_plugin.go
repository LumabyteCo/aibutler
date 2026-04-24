package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/plugin/registry"
)

// CmdPlugin handles the "plugin" command and its subcommands.
func CmdPlugin(reg *registry.Registry, args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aibutler plugin <install|list|enable|disable|remove|info> [args]")
	}
	switch args[0] {
	case "install":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler plugin install <manifest-path>")
		}
		return cmdPluginInstall(reg, args[1], w)
	case "list", "ls":
		return cmdPluginList(reg, w)
	case "enable":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler plugin enable <name>")
		}
		return cmdPluginEnable(reg, args[1], w)
	case "disable":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler plugin disable <name>")
		}
		return cmdPluginDisable(reg, args[1], w)
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler plugin remove <name>")
		}
		return cmdPluginRemove(reg, args[1], w)
	case "info":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler plugin info <name>")
		}
		return cmdPluginInfo(reg, args[1], w)
	default:
		return fmt.Errorf("unknown plugin subcommand: %s\nusage: aibutler plugin <install|list|enable|disable|remove|info> [args]", args[0])
	}
}

func cmdPluginInstall(reg *registry.Registry, manifestPath string, w io.Writer) error {
	ctx := context.Background()
	info, warnings, err := reg.Install(ctx, manifestPath)
	if err != nil {
		return fmt.Errorf("install: %w", err)
	}

	fmt.Fprintf(w, "Installed plugin: %s v%s\n", info.Name, info.Version)
	fmt.Fprintf(w, "  Status: %s\n", info.Status)
	fmt.Fprintf(w, "  Capabilities: %s\n", strings.Join(info.Capabilities, ", "))

	if len(warnings) > 0 {
		fmt.Fprintln(w, "  Warnings:")
		for _, warn := range warnings {
			fmt.Fprintf(w, "    - %s\n", warn)
		}
	}

	fmt.Fprintf(w, "\nRun 'aibutler plugin enable %s' to activate.\n", info.Name)
	return nil
}

func cmdPluginList(reg *registry.Registry, w io.Writer) error {
	ctx := context.Background()
	plugins, err := reg.List(ctx)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}

	if len(plugins) == 0 {
		fmt.Fprintln(w, "No plugins installed.")
		return nil
	}

	fmt.Fprintln(w, "Installed plugins:")
	for _, p := range plugins {
		fmt.Fprintf(w, "  %-20s v%-10s [%s]\n", p.Name, p.Version, p.Status)
	}
	return nil
}

func cmdPluginEnable(reg *registry.Registry, name string, w io.Writer) error {
	if err := reg.Enable(context.Background(), name); err != nil {
		return fmt.Errorf("enable: %w", err)
	}
	fmt.Fprintf(w, "Enabled plugin: %s\n", name)
	return nil
}

func cmdPluginDisable(reg *registry.Registry, name string, w io.Writer) error {
	if err := reg.Disable(context.Background(), name); err != nil {
		return fmt.Errorf("disable: %w", err)
	}
	fmt.Fprintf(w, "Disabled plugin: %s\n", name)
	return nil
}

func cmdPluginRemove(reg *registry.Registry, name string, w io.Writer) error {
	if err := reg.Remove(context.Background(), name); err != nil {
		return fmt.Errorf("remove: %w", err)
	}
	fmt.Fprintf(w, "Removed plugin: %s\n", name)
	return nil
}

func cmdPluginInfo(reg *registry.Registry, name string, w io.Writer) error {
	info, err := reg.Get(context.Background(), name)
	if err != nil {
		return fmt.Errorf("info: %w", err)
	}

	fmt.Fprintf(w, "Plugin: %s\n", info.Name)
	fmt.Fprintf(w, "  Version:      %s\n", info.Version)
	fmt.Fprintf(w, "  Status:       %s\n", info.Status)
	fmt.Fprintf(w, "  Capabilities: %s\n", strings.Join(info.Capabilities, ", "))
	fmt.Fprintf(w, "  Manifest:     %s\n", truncHash(info.ManifestHash))
	fmt.Fprintf(w, "  WASM:         %s\n", truncHash(info.WASMHash))
	return nil
}

func truncHash(h string) string {
	if len(h) > 16 {
		return h[:16] + "..."
	}
	return h
}
