package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/prompt"
)

// CmdSkill handles the "skill" command and its subcommands.
func CmdSkill(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aibutler skill <list|enable|disable> [name]")
	}
	switch args[0] {
	case "list":
		return cmdSkillList(app, w)
	case "enable":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler skill enable <name>")
		}
		return cmdSkillEnable(app, args[1], w)
	case "disable":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler skill disable <name>")
		}
		return cmdSkillDisable(app, args[1], w)
	default:
		return fmt.Errorf("unknown skill subcommand: %s", args[0])
	}
}

func cmdSkillList(app *App, w io.Writer) error {
	skills, err := prompt.LoadDefaultSkills()
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}

	// Also try loading from skills directory.
	if dir := app.Config.SkillsDir(); dir != "" {
		userSkills, _ := prompt.LoadSkillsDir(dir)
		skills = append(skills, userSkills...)
	}

	if len(skills) == 0 {
		fmt.Fprintln(w, "No skills installed.")
		return nil
	}

	fmt.Fprintf(w, "%-20s %-10s %s\n", "NAME", "STATUS", "TRIGGERS")
	fmt.Fprintf(w, "%-20s %-10s %s\n", "----", "------", "--------")

	for _, s := range skills {
		status := "enabled"
		if !s.Enabled {
			status = "disabled"
		}
		triggers := strings.Join(s.Triggers, ", ")
		if len(triggers) > 40 {
			triggers = triggers[:37] + "..."
		}
		fmt.Fprintf(w, "%-20s %-10s %s\n", s.Name, status, triggers)
	}
	return nil
}

func cmdSkillEnable(app *App, name string, w io.Writer) error {
	skills := app.Config.Settings.Skills
	for _, s := range skills {
		if s == name {
			fmt.Fprintf(w, "Skill %q is already enabled.\n", name)
			return nil
		}
	}
	app.Config.Settings.Skills = append(app.Config.Settings.Skills, name)
	fmt.Fprintf(w, "Enabled skill: %s\n", name)
	return nil
}

func cmdSkillDisable(app *App, name string, w io.Writer) error {
	skills := app.Config.Settings.Skills
	found := false
	var filtered []string
	for _, s := range skills {
		if s == name {
			found = true
			continue
		}
		filtered = append(filtered, s)
	}
	if !found {
		fmt.Fprintf(w, "Skill %q is not in the enabled list.\n", name)
		return nil
	}
	app.Config.Settings.Skills = filtered
	fmt.Fprintf(w, "Disabled skill: %s\n", name)
	return nil
}
