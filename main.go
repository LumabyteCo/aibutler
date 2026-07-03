package main

import (
	"fmt"
	"os"

	"github.com/LumabyteCo/aibutler/internal/cli"
)

func main() {
	if len(os.Args) < 2 {
		runDefault()
		return
	}

	switch os.Args[1] {
	case "version", "--version", "-v":
		cli.CmdVersion(os.Stdout)
	case "setup":
		runWithApp(func(app *cli.App) error {
			return cli.CmdSetup(app, os.Args[2:], os.Stdout)
		})
	case "config":
		runWithApp(func(app *cli.App) error {
			return cli.CmdConfig(app, os.Args[2:], os.Stdout)
		})
	case "skill":
		runWithApp(func(app *cli.App) error {
			return cli.CmdSkill(app, os.Args[2:], os.Stdout)
		})
	case "cost":
		runWithApp(func(app *cli.App) error {
			return cli.CmdCost(app, os.Args[2:], os.Stdout)
		})
	case "agent":
		runWithApp(func(app *cli.App) error {
			return cli.CmdAgent(app, os.Args[2:], os.Stdout)
		})
	case "mode":
		runWithApp(func(app *cli.App) error {
			return cli.CmdMode(app, os.Args[2:], os.Stdout)
		})
	case "vault":
		runWithApp(func(app *cli.App) error {
			return cli.CmdVault(app, os.Args[2:], os.Stdout)
		})
	case "auth":
		runWithApp(func(app *cli.App) error {
			return cli.CmdAuth(app, os.Args[2:], os.Stdout)
		})
	case "voice":
		runWithApp(func(app *cli.App) error {
			return cli.CmdVoice(app, os.Args[2:], os.Stdout)
		})
	case "backup":
		runWithApp(func(app *cli.App) error {
			return cli.CmdBackup(app, os.Args[2:], os.Stdout)
		})
	case "integrity":
		runWithApp(func(app *cli.App) error {
			return cli.CmdIntegrity(app, os.Args[2:], os.Stdout)
		})
	case "cleanup":
		runWithApp(func(app *cli.App) error {
			return cli.CmdCleanup(app, os.Args[2:], os.Stdout)
		})
	case "plugin":
		runWithApp(func(app *cli.App) error {
			return cli.CmdPlugin(app.PluginRegistry, os.Args[2:], os.Stdout)
		})
	case "mcp":
		runWithApp(func(app *cli.App) error {
			return cli.CmdMCP(app, os.Args[2:], os.Stdout)
		})
	case "memory":
		runWithApp(func(app *cli.App) error {
			return cli.CmdMemory(app, os.Args[2:], os.Stdout)
		})
	case "eval":
		runWithApp(func(app *cli.App) error {
			return cli.CmdEval(app, os.Args[2:], os.Stdout)
		})
	case "skills":
		runWithApp(func(app *cli.App) error {
			return cli.CmdSkills(app, os.Args[2:], os.Stdout)
		})
	case "repl":
		runWithApp(func(app *cli.App) error {
			return cli.CmdRepl(app, os.Args[2:], os.Stdout)
		})
	case "resume":
		runWithApp(func(app *cli.App) error {
			return cli.CmdResume(app, os.Args[2:], os.Stdout)
		})
	case "user":
		runWithApp(func(app *cli.App) error {
			return cli.CmdUser(app, os.Args[2:], os.Stdout)
		})
	case "gdpr":
		runWithApp(func(app *cli.App) error {
			return cli.CmdGDPR(app, os.Args[2:], os.Stdout)
		})
	case "start", "run":
		runDefault()
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func runDefault() {
	runWithApp(func(app *cli.App) error {
		return cli.CmdRun(app, nil, os.Stdout)
	})
}

func runWithApp(fn func(app *cli.App) error) {
	// Compute the exit code inside a closure so `defer app.Shutdown()` runs on
	// the error path and on panic, then os.Exit afterwards. Calling os.Exit
	// inline (the previous shape) skips deferreds, which would drop queued
	// vector-index work and skip the rest of App.Shutdown's cleanup.
	os.Exit(func() int {
		dataDir := cli.DefaultDataDir()
		app, err := cli.Bootstrap(dataDir, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		defer app.Shutdown()

		if err := fn(app); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}())
}

func printUsage() {
	fmt.Println(`AI Butler — Personal AI Agent Runtime

Usage:
  aibutler start             Start AI Butler (channels + scheduler)
  aibutler                   Same as 'start'
  aibutler setup            Show setup configuration
  aibutler config show      Display configuration
  aibutler skill <cmd>      Manage skills (list, enable, disable)
  aibutler skills <cmd>     Review self-authored skill proposals (pending, show, approve, reject)
  aibutler eval <cmd>       Internal benchmark suite (run, list, compare)
  aibutler cost <cmd>       Cost tracking (status, history, breakdown, strategy, budget)
  aibutler agent <cmd>      Agent management (list, status, history)
  aibutler mode [name]      Show or switch agent mode (auto, single)
  aibutler vault <cmd>      Credential vault (set, get, list, delete)
  aibutler auth <cmd>       Credential management (list, status, revoke)
  aibutler voice <cmd>      Voice pipeline (status, providers)
  aibutler backup <cmd>     Backup management (now, list, verify, export, import)
  aibutler integrity        Run database integrity check
  aibutler plugin <cmd>     Plugin management (install, list, enable, disable, remove, info)
  aibutler mcp <cmd>        MCP server (serve, tools)
  aibutler memory <cmd>     Memory management (import, digest, digests)
  aibutler repl             Interactive REPL with streaming and slash commands
  aibutler resume [ID]      Resume a previous session from disk
  aibutler user <cmd>       RBAC user management (create, list, assign)
  aibutler gdpr <cmd>       GDPR operations (delete-user, export, purge)
  aibutler cleanup          Remove expired sessions (default: >7 days)
  aibutler version          Print version
  aibutler help             Show this help`)
}
