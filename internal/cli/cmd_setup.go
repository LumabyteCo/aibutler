package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/vault"
	"gopkg.in/yaml.v3"
)

// CmdSetup bootstraps ~/.aibutler/ and config.yaml if they don't exist,
// runs the interactive setup wizard on first run, then prints current configuration.
func CmdSetup(app *App, _ []string, w io.Writer) error {
	created, err := ensureConfigFile(app.configPath)
	if err != nil {
		return fmt.Errorf("setup: %w", err)
	}

	fmt.Fprintln(w, "=== AI Butler Setup ===")
	fmt.Fprintln(w, "")

	if created {
		fmt.Fprintf(w, "Created config file: %s\n", app.configPath)
		fmt.Fprintln(w, "")
	}

	s := app.Config.Settings

	fmt.Fprintln(w, "Current configuration:")
	fmt.Fprintf(w, "  Persona:          %s\n", s.PersonaName)
	fmt.Fprintf(w, "  Language:         %s\n", s.Language)
	fmt.Fprintf(w, "  Timezone:         %s\n", s.Timezone)
	fmt.Fprintf(w, "  Model:            %s\n", s.Model)
	fmt.Fprintf(w, "  Agent Mode:       %s\n", s.AgentMode)
	fmt.Fprintf(w, "  Active Channels:  %s\n", strings.Join(s.ActiveChannels, ", "))
	fmt.Fprintf(w, "  Cost Strategy:    %s\n", s.Cost.Strategy)
	fmt.Fprintf(w, "  Monthly Budget:   $%.2f\n", s.Cost.MonthlyBudget)
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "Config file: %s\n", app.configPath)
	fmt.Fprintln(w, "")

	// On first run, run the interactive setup wizard.
	if created {
		reader := bufio.NewReader(os.Stdin)

		// Step 1: API provider + key.
		provider, err := promptAPIKey(app, reader, w)
		if err != nil {
			return err
		}

		// Step 2: Model selection (based on provider).
		if provider != "" {
			if err := promptModel(app, reader, provider, w); err != nil {
				return err
			}
		}

		// Step 3: Channel selection.
		if err := promptChannels(app, reader, w); err != nil {
			return err
		}

		// Step 4: Language.
		if err := promptLanguage(app, reader, w); err != nil {
			return err
		}

		// Persist config changes.
		if err := writeConfig(app); err != nil {
			return err
		}
	}

	// Show next steps.
	ctx := context.Background()
	hasKey, _ := app.Vault.Has(ctx, "anthropic_api_key")
	hasOpenAI, _ := app.Vault.Has(ctx, "openai_api_key")

	port := app.Config.Configurations.Web.Port
	if hasKey || hasOpenAI {
		fmt.Fprintln(w, "Ready! Start chatting:")
		fmt.Fprintln(w, "  ./aibutler start")
		fmt.Fprintf(w, "  Open http://localhost:%d in your browser\n", port)
	} else {
		fmt.Fprintln(w, "No API key found. Add one to get started:")
		fmt.Fprintln(w, "  ./aibutler vault set anthropic_api_key \"sk-ant-...\"")
		fmt.Fprintln(w, "  ./aibutler vault set openai_api_key \"sk-...\"")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Or use a local model (no key needed):")
		fmt.Fprintln(w, "  Edit model to \"ollama/llama3\" in config.yaml")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Then run: ./aibutler start")
	}

	return nil
}

// promptAPIKey asks for AI provider and API key. Returns the provider name.
func promptAPIKey(app *App, reader *bufio.Reader, w io.Writer) (string, error) {
	fmt.Fprintln(w, "Which AI provider will you use?")
	fmt.Fprintln(w, "  1. Claude (Anthropic) — default")
	fmt.Fprintln(w, "  2. GPT (OpenAI)")
	fmt.Fprintln(w, "  3. Ollama (local, no key needed)")
	fmt.Fprintln(w, "  4. Skip for now")
	fmt.Fprintln(w, "")
	fmt.Fprint(w, "Choice [1]: ")

	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	if choice == "" {
		choice = "1"
	}

	switch choice {
	case "1":
		fmt.Fprint(w, "Paste your Anthropic API key (sk-ant-...): ")
		key, _ := reader.ReadString('\n')
		key = strings.TrimSpace(key)
		if key == "" {
			fmt.Fprintln(w, "Skipped. You can add it later:")
			fmt.Fprintln(w, "  ./aibutler vault set anthropic_api_key \"sk-ant-...\"")
			fmt.Fprintln(w, "")
			return "anthropic", nil
		}
		if err := storeKey(app, "anthropic_api_key", key, w); err != nil {
			return "", err
		}
		return "anthropic", nil

	case "2":
		fmt.Fprint(w, "Paste your OpenAI API key (sk-...): ")
		key, _ := reader.ReadString('\n')
		key = strings.TrimSpace(key)
		if key == "" {
			fmt.Fprintln(w, "Skipped. You can add it later:")
			fmt.Fprintln(w, "  ./aibutler vault set openai_api_key \"sk-...\"")
			fmt.Fprintln(w, "")
			return "openai", nil
		}
		if err := storeKey(app, "openai_api_key", key, w); err != nil {
			return "", err
		}
		return "openai", nil

	case "3":
		fmt.Fprintln(w, "Using Ollama (local). No API key needed.")
		fmt.Fprintln(w, "Make sure Ollama is running: ollama serve")
		fmt.Fprintln(w, "")
		return "ollama", nil

	default:
		fmt.Fprintln(w, "Skipped. Add your API key later:")
		fmt.Fprintln(w, "  ./aibutler vault set anthropic_api_key \"sk-ant-...\"")
		fmt.Fprintln(w, "")
		return "", nil
	}
}

// promptModel asks which model to use based on the chosen provider.
func promptModel(app *App, reader *bufio.Reader, provider string, w io.Writer) error {
	fmt.Fprintln(w, "")
	switch provider {
	case "anthropic":
		fmt.Fprintln(w, "Which Claude model?")
		fmt.Fprintln(w, "  1. claude-sonnet-4-6 (recommended)")
		fmt.Fprintln(w, "  2. claude-haiku-4-5 (fastest, cheapest)")
		fmt.Fprintln(w, "  3. claude-opus-4-6 (most capable)")
		fmt.Fprint(w, "Choice [1]: ")

		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)
		switch choice {
		case "2":
			app.Config.Settings.Model = "claude-haiku-4-5"
		case "3":
			app.Config.Settings.Model = "claude-opus-4-6"
		default:
			app.Config.Settings.Model = "claude-sonnet-4-6"
		}

	case "openai":
		fmt.Fprintln(w, "Which GPT model?")
		fmt.Fprintln(w, "  1. gpt-4o (recommended)")
		fmt.Fprintln(w, "  2. gpt-4o-mini (fastest, cheapest)")
		fmt.Fprint(w, "Choice [1]: ")

		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)
		switch choice {
		case "2":
			app.Config.Settings.Model = "gpt-4o-mini"
		default:
			app.Config.Settings.Model = "gpt-4o"
		}

	case "ollama":
		fmt.Fprint(w, "Ollama model name [llama3]: ")
		name, _ := reader.ReadString('\n')
		name = strings.TrimSpace(name)
		if name == "" {
			name = "llama3"
		}
		app.Config.Settings.Model = name
	}

	fmt.Fprintf(w, "Model set to: %s\n", app.Config.Settings.Model)
	return nil
}

// promptChannels asks which channels to enable and collects tokens if needed.
func promptChannels(app *App, reader *bufio.Reader, w io.Writer) error {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Which channels do you want to enable?")
	fmt.Fprintln(w, "  1. WebChat only (default)")
	fmt.Fprintln(w, "  2. WebChat + Telegram")
	fmt.Fprintln(w, "  3. WebChat + Slack")
	fmt.Fprintln(w, "  4. WebChat + Discord")
	fmt.Fprint(w, "Choice [1]: ")

	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	channels := []string{"webchat"}

	switch choice {
	case "2":
		channels = append(channels, "telegram")
		fmt.Fprint(w, "Telegram bot token: ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		if token != "" {
			if err := storeKey(app, "telegram_bot_token", token, w); err != nil {
				return err
			}
		} else {
			fmt.Fprintln(w, "  Add later: ./aibutler vault set telegram_bot_token \"...\"")
		}

	case "3":
		channels = append(channels, "slack")
		fmt.Fprint(w, "Slack bot token: ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		if token != "" {
			if err := storeKey(app, "slack_bot_token", token, w); err != nil {
				return err
			}
		} else {
			fmt.Fprintln(w, "  Add later: ./aibutler vault set slack_bot_token \"...\"")
		}
		fmt.Fprint(w, "Slack signing secret: ")
		secret, _ := reader.ReadString('\n')
		secret = strings.TrimSpace(secret)
		if secret != "" {
			if err := storeKey(app, "slack_signing_secret", secret, w); err != nil {
				return err
			}
		}

	case "4":
		channels = append(channels, "discord")
		fmt.Fprint(w, "Discord bot token: ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		if token != "" {
			if err := storeKey(app, "discord_bot_token", token, w); err != nil {
				return err
			}
		} else {
			fmt.Fprintln(w, "  Add later: ./aibutler vault set discord_bot_token \"...\"")
		}
	}

	app.Config.Settings.ActiveChannels = channels
	fmt.Fprintf(w, "Active channels: %s\n", strings.Join(channels, ", "))
	return nil
}

// promptLanguage asks the user's preferred language.
func promptLanguage(app *App, reader *bufio.Reader, w io.Writer) error {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Preferred language?")
	fmt.Fprintln(w, "  1. English (default)")
	fmt.Fprintln(w, "  2. Arabic")
	fmt.Fprintln(w, "  3. Spanish")
	fmt.Fprintln(w, "  4. Other (edit config.yaml)")
	fmt.Fprint(w, "Choice [1]: ")

	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	switch choice {
	case "2":
		app.Config.Settings.Language = "ar"
	case "3":
		app.Config.Settings.Language = "es"
	case "4":
		fmt.Fprintln(w, "  Edit 'language' in config.yaml (supported: en, ar, es, fr, de, zh, ja, ko, pt, ru, tr, hi, it, nl)")
	default:
		app.Config.Settings.Language = "en"
	}

	fmt.Fprintf(w, "Language set to: %s\n", app.Config.Settings.Language)
	return nil
}

func storeKey(app *App, name, value string, w io.Writer) error {
	ctx := context.Background()
	cred := vault.Credential{
		Key:   name,
		Type:  vault.CredAPIKey,
		Value: []byte(value),
	}
	if err := app.Vault.Store(ctx, cred); err != nil {
		return fmt.Errorf("vault store: %w", err)
	}
	fmt.Fprintf(w, "Stored %s in vault.\n", name)
	fmt.Fprintln(w, "")
	return nil
}

// writeConfig persists the current in-memory config back to the YAML file.
func writeConfig(app *App) error {
	data, err := yaml.Marshal(app.Config)
	if err != nil {
		return fmt.Errorf("setup: marshal config: %w", err)
	}

	header := "# AI Butler configuration\n# Docs: docs/configuration/CONFIG-REFERENCE.md\n\n"
	return os.WriteFile(app.configPath, append([]byte(header), data...), 0600)
}

// starterConfig is a minimal, readable config written on first run.
const starterConfig = `# AI Butler configuration
# Docs: docs/configuration/CONFIG-REFERENCE.md

settings:
  persona_name: Butler
  language: en
  timezone: UTC
  model: claude-sonnet-4-6
  agent_mode: auto
  active_channels:
    - webchat
  cost:
    strategy: balanced
    monthly_budget: 10.00

configurations:
  web:
    port: 3377
  channels:
    webchat:
      enabled: true
      typing_indicators: true
`

// ensureConfigFile creates ~/.aibutler/ and config.yaml with starter
// defaults if they don't already exist. Returns true if the file was created.
func ensureConfigFile(configPath string) (bool, error) {
	if _, err := os.Stat(configPath); err == nil {
		return false, nil // already exists
	}

	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return false, fmt.Errorf("create directory %s: %w", dir, err)
	}

	if err := os.WriteFile(configPath, []byte(starterConfig), 0600); err != nil {
		return false, fmt.Errorf("write config %s: %w", configPath, err)
	}

	return true, nil
}
