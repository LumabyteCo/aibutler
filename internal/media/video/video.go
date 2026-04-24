// Package video provides video processing tools using ffmpeg.
package video

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// toolRegistry is the interface for registering tools. Using a local narrow interface
// avoids import cycles with the tool package.
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Processor provides video processing operations backed by ffmpeg.
type Processor struct {
	ffmpegPath string
}

// NewProcessor creates a video processor. It looks for ffmpeg in PATH.
func NewProcessor() *Processor {
	path, _ := exec.LookPath("ffmpeg")
	return &Processor{ffmpegPath: path}
}

// Available returns true if ffmpeg is found in PATH.
func (p *Processor) Available() bool {
	return p.ffmpegPath != ""
}

// SetFFmpegPath overrides the ffmpeg binary path (for testing).
func (p *Processor) SetFFmpegPath(path string) { p.ffmpegPath = path }

// ExtractKeyframes extracts keyframes from a video at the given interval.
// Returns paths to the extracted keyframe images.
func (p *Processor) ExtractKeyframes(ctx context.Context, path string, intervalSecs int) ([]string, error) {
	if !p.Available() {
		return nil, fmt.Errorf("video: ffmpeg not available — install ffmpeg to use video processing")
	}
	if path == "" {
		return nil, fmt.Errorf("video: path is required")
	}
	if intervalSecs <= 0 {
		intervalSecs = 5
	}

	dir := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	outputPattern := filepath.Join(dir, base+"_keyframe_%04d.jpg")

	filter := fmt.Sprintf("fps=1/%d", intervalSecs)
	args := []string{
		"-i", path,
		"-vf", filter,
		"-vsync", "vfn",
		"-q:v", "2",
		outputPattern,
	}

	cmd := exec.CommandContext(ctx, p.ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("video: ffmpeg keyframe extraction failed: %w\nOutput: %s", err, string(output))
	}

	// List extracted files by matching the pattern.
	matches, err := filepath.Glob(filepath.Join(dir, base+"_keyframe_*.jpg"))
	if err != nil {
		return nil, fmt.Errorf("video: listing keyframes: %w", err)
	}
	return matches, nil
}

// ExtractAudio extracts the audio track from a video to WAV format.
// Returns the path to the extracted audio file.
func (p *Processor) ExtractAudio(ctx context.Context, path string) (string, error) {
	if !p.Available() {
		return "", fmt.Errorf("video: ffmpeg not available — install ffmpeg to use video processing")
	}
	if path == "" {
		return "", fmt.Errorf("video: path is required")
	}

	dir := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	outputPath := filepath.Join(dir, base+"_audio.wav")

	args := []string{
		"-i", path,
		"-vn",
		"-acodec", "pcm_s16le",
		"-ar", "16000",
		"-ac", "1",
		"-y",
		outputPath,
	}

	cmd := exec.CommandContext(ctx, p.ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("video: ffmpeg audio extraction failed: %w\nOutput: %s", err, string(output))
	}
	return outputPath, nil
}

// Summary holds a structured video summary.
type Summary struct {
	KeyframeCount int      `json:"keyframe_count"`
	HasTranscript bool     `json:"has_transcript"`
	Keyframes     []string `json:"keyframes,omitempty"`
	Transcript    string   `json:"transcript,omitempty"`
	Description   string   `json:"description"`
}

// Summarize combines keyframe descriptions and a transcript into a structured summary.
// This does not call an LLM — it returns a structured summary for further processing.
func (p *Processor) Summarize(_ context.Context, keyframePaths []string, transcript string) (*Summary, error) {
	if len(keyframePaths) == 0 && transcript == "" {
		return nil, fmt.Errorf("video: at least keyframes or transcript is required")
	}

	desc := fmt.Sprintf("Video with %d keyframes", len(keyframePaths))
	if transcript != "" {
		words := len(strings.Fields(transcript))
		desc += fmt.Sprintf(" and transcript (%d words)", words)
	}

	return &Summary{
		KeyframeCount: len(keyframePaths),
		HasTranscript: transcript != "",
		Keyframes:     keyframePaths,
		Transcript:    transcript,
		Description:   desc,
	}, nil
}

// RegisterVideoTools registers media.video.keyframes, media.video.audio, and media.video.summarize tools.
func RegisterVideoTools(registry toolRegistry, proc *Processor) {
	registry.Register(
		"media.video.keyframes",
		"Extract keyframes from a video at regular intervals (requires ffmpeg).",
		`{"type":"object","properties":{"path":{"type":"string","description":"Path to the video file"},"interval_secs":{"type":"integer","description":"Interval between keyframes in seconds (default: 5)"}},"required":["path"]}`,
		"tool.media.video",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Path         string `json:"path"`
				IntervalSecs int    `json:"interval_secs"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("media.video.keyframes: invalid input: %w", err)
			}
			if args.Path == "" {
				return "", fmt.Errorf("media.video.keyframes: path is required")
			}
			paths, err := proc.ExtractKeyframes(ctx, args.Path, args.IntervalSecs)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]interface{}{"keyframes": paths, "count": len(paths)})
			return string(out), nil
		},
	)

	registry.Register(
		"media.video.audio",
		"Extract the audio track from a video file to WAV (requires ffmpeg).",
		`{"type":"object","properties":{"path":{"type":"string","description":"Path to the video file"}},"required":["path"]}`,
		"tool.media.video",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("media.video.audio: invalid input: %w", err)
			}
			if args.Path == "" {
				return "", fmt.Errorf("media.video.audio: path is required")
			}
			audioPath, err := proc.ExtractAudio(ctx, args.Path)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]string{"audio_path": audioPath})
			return string(out), nil
		},
	)

	registry.Register(
		"media.video.summarize",
		"Combine keyframe paths and transcript into a structured video summary.",
		`{"type":"object","properties":{"keyframe_paths":{"type":"array","items":{"type":"string"},"description":"Paths to keyframe images"},"transcript":{"type":"string","description":"Transcript text"}},"required":[]}`,
		"tool.media.video",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				KeyframePaths []string `json:"keyframe_paths"`
				Transcript    string   `json:"transcript"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("media.video.summarize: invalid input: %w", err)
			}
			summary, err := proc.Summarize(ctx, args.KeyframePaths, args.Transcript)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(summary)
			return string(out), nil
		},
	)
}
