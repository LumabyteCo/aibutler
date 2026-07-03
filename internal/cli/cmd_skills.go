package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/audit"
	"github.com/LumabyteCo/aibutler/internal/changelog"
	"github.com/LumabyteCo/aibutler/internal/skillsynth"
)

// CmdSkills handles `aibutler skills` — review and decide on self-authored
// skill proposals. Approval here is THE activation path: staged proposals
// have no other way to become live skills.
func CmdSkills(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(w, "Usage: aibutler skills <subcommand>")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Subcommands:")
		fmt.Fprintln(w, "  pending              List self-authored skill proposals awaiting review")
		fmt.Fprintln(w, "  show <id>            Print a proposal's full content")
		fmt.Fprintln(w, "  approve <id>         Activate a proposal (moves it into the live skills dir)")
		fmt.Fprintln(w, "  reject <id>          Discard a proposal")
		return nil
	}

	ledger := changelog.New(app.DB.Conn(), audit.NewSQLiteAuditor(app.DB.Conn()))
	synth := skillsynth.New(skillsynth.Config{
		SkillsDir: app.Config.SkillsDir(),
	}, nil, app.DB.Conn(), ledger, nil)
	ctx := context.Background()

	switch args[0] {
	case "pending":
		pending, err := synth.Pending(ctx)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			fmt.Fprintln(w, "No skill proposals pending review.")
			return nil
		}
		for _, p := range pending {
			fmt.Fprintf(w, "#%d  %s  (staged %s)\n", p.ID, p.Title, p.Created)
		}
		fmt.Fprintf(w, "\n%d pending. Inspect with `aibutler skills show <id>`, then approve or reject.\n", len(pending))
		return nil

	case "show":
		id, err := parseProposalID(args)
		if err != nil {
			return err
		}
		pending, err := synth.Pending(ctx)
		if err != nil {
			return err
		}
		for _, p := range pending {
			if p.ID == id {
				fmt.Fprintf(w, "# Proposal %d — %s\n\n%s\n", p.ID, p.Title, p.Body)
				return nil
			}
		}
		return fmt.Errorf("no pending proposal %d", id)

	case "approve":
		id, err := parseProposalID(args)
		if err != nil {
			return err
		}
		path, err := synth.Approve(ctx, id, "user", 0)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Approved — skill activated at %s\n", path)
		fmt.Fprintln(w, "Note: skills load at startup — restart the daemon to activate it in a running instance.")
		fmt.Fprintln(w, "Note: effectiveness is unmeasured until an eval comparison exists (aibutler eval run / compare).")
		return nil

	case "reject":
		id, err := parseProposalID(args)
		if err != nil {
			return err
		}
		if err := synth.Reject(ctx, id, "user"); err != nil {
			return err
		}
		fmt.Fprintf(w, "Rejected proposal %d.\n", id)
		return nil

	default:
		return fmt.Errorf("unknown skills subcommand: %s", args[0])
	}
}

func parseProposalID(args []string) (int64, error) {
	if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
		return 0, fmt.Errorf("usage: aibutler skills %s <id>", args[0])
	}
	id, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad proposal id %q", args[1])
	}
	return id, nil
}
