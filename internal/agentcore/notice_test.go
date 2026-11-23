package agentcore

import (
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func TestConfigNoticeNilSeen(t *testing.T) {
	text, nu := configNotice(nil, true, domain.DefaultSettings())
	if text != "" || nu != (domain.SeenConfig{AgentCreatesCards: true, Capture: true}) {
		t.Fatalf("text=%q nu=%+v", text, nu)
	}
}

func TestConfigNoticeCardsOff(t *testing.T) {
	set := domain.DefaultSettings()
	set.AgentCreatesCards = false
	text, _ := configNotice(&domain.SeenConfig{AgentCreatesCards: true, Capture: true}, true, set)
	if !strings.Contains(text, "a configuração mudou") || !strings.Contains(text, "Só crie card quando o usuário pedir") {
		t.Fatalf("text = %q", text)
	}
}

func TestConfigNoticeCaptureOff(t *testing.T) {
	set := domain.DefaultSettings()
	set.AgentCreatesCards = false
	text, _ := configNotice(&domain.SeenConfig{AgentCreatesCards: true, Capture: true}, false, set)
	if !strings.Contains(text, "foi desligada") || strings.Contains(text, "Só crie card") {
		t.Fatalf("text = %q", text)
	}
}

func TestConfigNoticeCaptureOn(t *testing.T) {
	text, _ := configNotice(&domain.SeenConfig{AgentCreatesCards: true, Capture: false}, true, domain.DefaultSettings())
	if !strings.Contains(text, "foi ligada") || !strings.Contains(text, RegisteredPhrase) {
		t.Fatalf("text = %q", text)
	}
}

func TestConfigNoticeFlipBack(t *testing.T) {
	text, _ := configNotice(&domain.SeenConfig{AgentCreatesCards: true, Capture: true}, true, domain.DefaultSettings())
	if text != "" {
		t.Fatalf("text = %q", text)
	}
}
