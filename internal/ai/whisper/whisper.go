package whisper

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rwurtz/vastiva/internal/ai"
)

type Generator struct {
	provider ai.Provider
}

func NewGenerator(p ai.Provider) *Generator {
	return &Generator{provider: p}
}

// GenerateSRT extracts audio from a video and generates an SRT file
func (g *Generator) GenerateSRT(ctx context.Context, videoPath string) (string, error) {
	if g.provider == nil {
		return "", fmt.Errorf("AI provider not configured")
	}

	// 1. Extract audio to a temporary file
	audioPath := filepath.Join(os.TempDir(), fmt.Sprintf("vastiva_audio_%d.mp3", os.Getpid()))
	defer os.Remove(audioPath)

	log.Printf("[Whisper] Extracting audio for transcription: %s", videoPath)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-i", videoPath, "-vn", "-acodec", "libmp3lame", "-ar", "16000", "-ac", "1", "-b:a", "64k", "-y", audioPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to extract audio: %v (Output: %s)", err, string(output))
	}

	// 2. Call AI provider to transcribe
	log.Printf("[Whisper] Transcribing audio with %s...", g.provider.GetName())
	srtContent, err := g.provider.Transcribe(ctx, audioPath)
	if err != nil {
		return "", fmt.Errorf("transcription failed: %v", err)
	}

	// 3. Save SRT content to a file
	srtPath := strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + ".srt"
	if err := os.WriteFile(srtPath, []byte(srtContent), 0644); err != nil {
		return "", fmt.Errorf("failed to save SRT: %v", err)
	}

	return srtPath, nil
}
