package ui

import (
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

func TestVoiceAssistantPrompt(t *testing.T) {
	s := &shellUI{
		sys: shell.System{
			Voice: system.VoiceState{
				Available: false,
				Status:    "stopped",
				Error:     "the daemon is not running",
			},
		},
	}
	prompt := s.voiceAssistantPrompt()
	if prompt == "" {
		t.Fatal("voiceAssistantPrompt returned empty prompt")
	}
	if !strings.Contains(prompt, "voxtype") {
		t.Errorf("prompt should mention voxtype, got: %s", prompt)
	}
	if !strings.Contains(prompt, "daemon") {
		t.Errorf("prompt should mention daemon, got: %s", prompt)
	}
}

func TestHostDistro(t *testing.T) {
	_ = hostDistro()
}
