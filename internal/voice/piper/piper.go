// Package piper provides a local TTS adapter using the Piper text-to-speech binary.
package piper

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

const synthesizeTimeout = 60 * time.Second

// Executor runs the Piper binary for local TTS synthesis.
type Executor struct {
	binaryPath string
	modelPath  string
}

// NewExecutor creates a Piper executor with the given binary and model paths.
func NewExecutor(binaryPath, modelPath string) *Executor {
	return &Executor{
		binaryPath: binaryPath,
		modelPath:  modelPath,
	}
}

// Available returns true if the Piper binary exists at the configured path.
func (e *Executor) Available() bool {
	if e.binaryPath == "" {
		return false
	}
	_, err := os.Stat(e.binaryPath)
	return err == nil
}

// Synthesize runs Piper to generate WAV audio from text.
// Returns an error if the binary is not available.
func (e *Executor) Synthesize(ctx context.Context, text string) ([]byte, error) {
	if !e.Available() {
		return nil, fmt.Errorf("piper: binary not found at %q", e.binaryPath)
	}

	execCtx, cancel := context.WithTimeout(ctx, synthesizeTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, e.binaryPath, //nolint:gosec
		"--model", e.modelPath,
		"--output-raw",
	)
	cmd.Stdin = bytes.NewBufferString(text)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if execCtx.Err() != nil {
			return nil, fmt.Errorf("piper: timeout after %s", synthesizeTimeout)
		}
		return nil, fmt.Errorf("piper: run: %w (stderr: %s)", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// RegisterPiperTool registers the voice.piper.synthesize tool.
func RegisterPiperTool(registry toolRegistry, executor *Executor) {
	registry.Register(
		"voice.piper.synthesize",
		"Synthesize text to speech locally using Piper. Returns base64-encoded WAV audio.",
		`{"type":"object","properties":{"text":{"type":"string","description":"Text to synthesize"}},"required":["text"]}`,
		"voice.speak",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("voice.piper.synthesize: invalid input: %w", err)
			}
			if args.Text == "" {
				return "", fmt.Errorf("voice.piper.synthesize: text is required")
			}
			audio, err := executor.Synthesize(ctx, args.Text)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]interface{}{
				"audio_base64": base64.StdEncoding.EncodeToString(audio),
				"format":       "wav",
				"bytes":        len(audio),
			})
			return string(out), nil
		},
	)
}
