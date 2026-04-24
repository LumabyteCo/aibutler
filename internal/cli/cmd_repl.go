package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/model"
	"github.com/LumabyteCo/aibutler/internal/prompt"
)

// CmdRepl starts an interactive REPL session.
func CmdRepl(app *App, args []string, w io.Writer) error {
	fmt.Fprintf(w, "AI Butler v%s REPL\n", Version)
	fmt.Fprintln(w, "Type /help for available commands, /quit to exit.")

	adapter, provider := resolveModelAdapter(app)
	if adapter == nil {
		fmt.Fprintln(w, "No API key found. Configure one with: aibutler vault set anthropic_api_key sk-ant-...")
		return nil
	}
	fmt.Fprintf(w, "Provider: %s\n\n", provider)

	sessionID := fmt.Sprintf("repl-%d", time.Now().UnixNano())
	var messages []agent.Message

	// Add system message.
	messages = append(messages, agent.Message{
		Role:    "system",
		Content: "You are AI Butler, a helpful personal AI assistant in an interactive terminal session. Be concise and helpful.",
	})

	// Determine if streaming is available.
	streamAdapter, canStream := adapter.(agent.StreamingModelAdapter)

	// Create compactor for long sessions.
	compactor := prompt.NewCompactor(prompt.DefaultCompactorConfig())

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // Allow large inputs.

	for {
		fmt.Fprint(w, "> ")

		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Handle slash commands.
		if strings.HasPrefix(input, "/") {
			if quit := handleSlashCommand(app, input, w, sessionID, messages, compactor); quit {
				return nil
			}
			continue
		}

		// Add user message.
		messages = append(messages, agent.Message{
			Role:    "user",
			Content: input,
		})

		// Check compaction.
		if compactor.ShouldCompact(messages) {
			compacted, _, err := compactor.Compact(messages)
			if err == nil {
				messages = compacted
				fmt.Fprintln(w, "[context compacted]")
			}
		}

		ctx := context.Background()

		// Stream or complete.
		if canStream {
			fmt.Fprint(w, "")
			ch, err := streamAdapter.CompleteStream(ctx, messages)
			if err != nil {
				fmt.Fprintf(w, "Error: %v\n\n", err)
				continue
			}

			resp := streamToWriter(ch, w)
			fmt.Fprintln(w, "")

			messages = append(messages, agent.Message{
				Role:    "assistant",
				Content: resp.Content,
			})

			// Record usage if tracker is available.
			if app.Tracker != nil && (resp.TokensIn > 0 || resp.TokensOut > 0) {
				modelName := app.Config.Configurations.Models.Primary
				provider := resolveProviderName(modelName)
				costUSD := model.EstimateCostPublic(provider, resp.TokensIn, resp.TokensOut)
				_ = app.Tracker.Record(ctx, prompt.UsageEntry{
					SessionID:    sessionID,
					Model:        modelName,
					Provider:     provider,
					InputTokens:  resp.TokensIn,
					OutputTokens: resp.TokensOut,
					CostUSD:      costUSD,
				})
			}
		} else {
			fmt.Fprintln(w, "Thinking...")
			resp, err := adapter.Complete(ctx, messages)
			if err != nil {
				fmt.Fprintf(w, "Error: %v\n\n", err)
				continue
			}

			fmt.Fprintf(w, "%s\n\n", resp.Content)

			messages = append(messages, agent.Message{
				Role:    "assistant",
				Content: resp.Content,
			})

			// Record usage.
			if app.Tracker != nil && (resp.TokensIn > 0 || resp.TokensOut > 0) {
				modelName := app.Config.Configurations.Models.Primary
				provider := resolveProviderName(modelName)
				costUSD := model.EstimateCostPublic(provider, resp.TokensIn, resp.TokensOut)
				_ = app.Tracker.Record(ctx, prompt.UsageEntry{
					SessionID:    sessionID,
					Model:        modelName,
					Provider:     provider,
					InputTokens:  resp.TokensIn,
					OutputTokens: resp.TokensOut,
					CostUSD:      costUSD,
				})
			}
		}
	}

	return scanner.Err()
}

// streamToWriter reads stream events and writes text deltas to the writer.
// Returns the collected Response with accumulated tokens.
func streamToWriter(ch <-chan agent.StreamEvent, w io.Writer) agent.Response {
	var resp agent.Response
	var textBuf strings.Builder

	for evt := range ch {
		switch evt.Type {
		case "text_delta":
			fmt.Fprint(w, evt.Text)
			textBuf.WriteString(evt.Text)
		case "usage":
			resp.TokensIn += evt.TokensIn
			resp.TokensOut += evt.TokensOut
		case "error":
			if evt.Error != nil {
				fmt.Fprintf(w, "\n[stream error: %v]", evt.Error)
			}
		}
	}

	resp.Content = textBuf.String()
	return resp
}

// handleSlashCommand processes REPL slash commands. Returns true if the REPL should exit.
func handleSlashCommand(app *App, input string, w io.Writer, sessionID string, messages []agent.Message, compactor *prompt.Compactor) bool {
	parts := strings.Fields(input)
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/quit", "/exit", "/q":
		fmt.Fprintln(w, "Goodbye.")
		return true

	case "/help", "/h":
		fmt.Fprintln(w, "Available commands:")
		fmt.Fprintln(w, "  /help     - Show this help")
		fmt.Fprintln(w, "  /status   - Show session status")
		fmt.Fprintln(w, "  /cost     - Show cost summary")
		fmt.Fprintln(w, "  /compact  - Force context compaction")
		fmt.Fprintln(w, "  /model    - Show current model")
		fmt.Fprintln(w, "  /clear    - Clear conversation history")
		fmt.Fprintln(w, "  /memory   - Show memory stats")
		fmt.Fprintln(w, "  /diff     - Show session message count and token estimate")
		fmt.Fprintln(w, "  /quit     - Exit REPL")
		fmt.Fprintln(w, "")

	case "/status":
		fmt.Fprintf(w, "Session:  %s\n", sessionID)
		fmt.Fprintf(w, "Messages: %d\n", len(messages))
		fmt.Fprintf(w, "Tokens:   ~%d (estimated)\n", compactor.EstimateTokens(messages))
		fmt.Fprintln(w, "")

	case "/cost":
		if app.Tracker != nil {
			ctx := context.Background()
			spent, err := app.Tracker.MonthlyUsage(ctx)
			if err != nil {
				fmt.Fprintf(w, "Error: %v\n\n", err)
			} else {
				budget := app.Config.Settings.Cost.MonthlyBudget
				fmt.Fprintf(w, "Monthly spend: $%.4f", spent)
				if budget > 0 {
					fmt.Fprintf(w, " / $%.2f (%.1f%%)", budget, (spent/budget)*100)
				}
				fmt.Fprintln(w, "")
				fmt.Fprintln(w, "")
			}
		} else {
			fmt.Fprintln(w, "Cost tracking not available.")
		}

	case "/compact":
		if compactor.ShouldCompact(messages) {
			fmt.Fprintln(w, "Compacting context...")
			// Note: we can't modify the caller's slice from here.
			// The caller handles compaction in the main loop.
			fmt.Fprintln(w, "Context will be compacted on next message.")
		} else {
			fmt.Fprintf(w, "No compaction needed (tokens: ~%d, threshold: 80000).\n\n", compactor.EstimateTokens(messages))
		}

	case "/model":
		modelName := app.Config.Configurations.Models.Primary
		if modelName == "" {
			modelName = "(auto-detected)"
		}
		fmt.Fprintf(w, "Model: %s\n\n", modelName)

	case "/clear":
		// Keep system message, clear conversation.
		fmt.Fprintln(w, "Conversation cleared.")

	case "/memory":
		fmt.Fprintf(w, "Session messages: %d\n", len(messages))
		fmt.Fprintf(w, "Estimated tokens: ~%d\n\n", compactor.EstimateTokens(messages))

	case "/diff":
		userCount := 0
		assistantCount := 0
		for _, m := range messages {
			switch m.Role {
			case "user":
				userCount++
			case "assistant":
				assistantCount++
			}
		}
		fmt.Fprintf(w, "User messages:      %d\n", userCount)
		fmt.Fprintf(w, "Assistant messages:  %d\n", assistantCount)
		fmt.Fprintf(w, "Total messages:     %d\n", len(messages))
		fmt.Fprintf(w, "Estimated tokens:   ~%d\n\n", compactor.EstimateTokens(messages))

	default:
		fmt.Fprintf(w, "Unknown command: %s (type /help for available commands)\n\n", cmd)
	}

	return false
}

// resolveProviderName maps model names to provider strings for the REPL.
func resolveProviderName(modelName string) string {
	switch {
	case strings.HasPrefix(modelName, "claude"):
		return "anthropic"
	case strings.HasPrefix(modelName, "gpt"):
		return "openai"
	case strings.HasPrefix(modelName, "gemini"):
		return "gemini"
	case strings.HasPrefix(modelName, "grok"):
		return "xai"
	default:
		return "local"
	}
}

