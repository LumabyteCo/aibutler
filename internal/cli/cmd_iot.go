package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/iot"
)

// CmdIoT manages the smart-home surface: the tier-3 safety PIN and
// adapter status. The PIN gates lock/alarm/garage commands — it is stored
// as a bcrypt hash in the vault and can never be read back.
func CmdIoT(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aibutler iot <set-pin|verify-pin|status>")
	}
	switch args[0] {
	case "set-pin":
		pin := ""
		if len(args) >= 2 {
			pin = strings.TrimSpace(args[1])
		} else {
			fmt.Fprint(w, "Enter new IoT safety PIN: ")
			pin = promptLine()
		}
		if err := validatePIN(pin); err != nil {
			return err
		}
		verifier := iot.NewPINVerifier(app.Vault)
		if err := verifier.SetPIN(context.Background(), pin); err != nil {
			return fmt.Errorf("iot set-pin: %w", err)
		}
		fmt.Fprintln(w, "IoT safety PIN set. Tier-3 devices (locks, alarms, garages) will require it.")
		fmt.Fprintln(w, "The PIN is stored as a bcrypt hash and cannot be recovered if forgotten.")
		return nil

	case "verify-pin":
		pin := ""
		if len(args) >= 2 {
			pin = strings.TrimSpace(args[1])
		} else {
			fmt.Fprint(w, "Enter PIN to verify: ")
			pin = promptLine()
		}
		verifier := iot.NewPINVerifier(app.Vault)
		ok, err := verifier.Verify(context.Background(), pin)
		if err != nil {
			// No PIN set yet is the common case — make it explicit.
			fmt.Fprintln(w, "No IoT safety PIN is set. Configure one with: aibutler iot set-pin")
			return nil
		}
		if ok {
			fmt.Fprintln(w, "PIN verified.")
		} else {
			fmt.Fprintln(w, "PIN incorrect.")
		}
		return nil

	case "status":
		ctx := context.Background()
		fmt.Fprintln(w, "=== IoT Status ===")
		fmt.Fprintf(w, "  Adapter:      %s\n", app.Config.Configurations.IoT.Adapter)
		if app.Config.Configurations.IoT.Adapter == "homeassistant" {
			fmt.Fprintf(w, "  HA URL:       %s\n", app.Config.Configurations.IoT.HAURL)
			token := "not set"
			if cred, err := app.Vault.Get(ctx, "homeassistant_token"); err == nil && len(cred.Value) > 0 {
				token = "set"
			}
			fmt.Fprintf(w, "  HA token:     %s\n", token)
		}
		pin := "not set"
		if _, err := app.Vault.Get(ctx, "iot_pin"); err == nil {
			pin = "set"
		}
		fmt.Fprintf(w, "  Safety PIN:   %s\n", pin)
		return nil

	default:
		return fmt.Errorf("unknown iot subcommand: %s (use set-pin, verify-pin, or status)", args[0])
	}
}

// validatePIN enforces the minimum policy: 4-12 digits.
func validatePIN(pin string) error {
	if len(pin) < 4 || len(pin) > 12 {
		return fmt.Errorf("iot set-pin: PIN must be 4-12 digits")
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return fmt.Errorf("iot set-pin: PIN must contain digits only")
		}
	}
	return nil
}

// promptLine reads a trimmed line from stdin (interactive prompt).
func promptLine() string {
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimSpace(line)
}