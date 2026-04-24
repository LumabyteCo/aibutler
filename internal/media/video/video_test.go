package video_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/media/video"
)

func TestExtractKeyframes_NoFFmpeg(t *testing.T) {
	proc := video.NewProcessor()
	proc.SetFFmpegPath("") // Force unavailable.

	_, err := proc.ExtractKeyframes(context.Background(), "/tmp/test.mp4", 5)
	if err == nil {
		t.Fatal("expected error when ffmpeg not available")
	}
	if !strings.Contains(err.Error(), "ffmpeg not available") {
		t.Errorf("expected ffmpeg-not-available error, got: %v", err)
	}
}

func TestExtractAudio_NoFFmpeg(t *testing.T) {
	proc := video.NewProcessor()
	proc.SetFFmpegPath("") // Force unavailable.

	_, err := proc.ExtractAudio(context.Background(), "/tmp/test.mp4")
	if err == nil {
		t.Fatal("expected error when ffmpeg not available")
	}
	if !strings.Contains(err.Error(), "ffmpeg not available") {
		t.Errorf("expected ffmpeg-not-available error, got: %v", err)
	}
}

func TestSummarize(t *testing.T) {
	proc := video.NewProcessor()

	keyframes := []string{"/tmp/frame_0001.jpg", "/tmp/frame_0002.jpg", "/tmp/frame_0003.jpg"}
	transcript := "This is a test video about AI processing with multiple scenes."

	summary, err := proc.Summarize(context.Background(), keyframes, transcript)
	if err != nil {
		t.Fatalf("Summarize: unexpected error: %v", err)
	}
	if summary.KeyframeCount != 3 {
		t.Errorf("expected KeyframeCount=3, got %d", summary.KeyframeCount)
	}
	if !summary.HasTranscript {
		t.Error("expected HasTranscript=true")
	}
	if !strings.Contains(summary.Description, "3 keyframes") {
		t.Errorf("expected description to mention keyframes, got: %s", summary.Description)
	}
	if !strings.Contains(summary.Description, "words") {
		t.Errorf("expected description to mention words, got: %s", summary.Description)
	}
}
