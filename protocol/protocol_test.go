package protocol

import (
	"testing"
	"time"
)

func TestNewProtocolAndDefaultConfig(t *testing.T) {
	cfg := DefaultDebateConfig()
	if cfg.Name == "" {
		t.Fatalf("DefaultDebateConfig.Name empty")
	}
	if cfg.Version == "" {
		t.Fatalf("DefaultDebateConfig.Version empty")
	}
	if cfg.Timeout != 30*time.Second {
		t.Fatalf("DefaultDebateConfig.Timeout = %v, want 30s", cfg.Timeout)
	}

	p, err := NewProtocol(cfg)
	if err != nil {
		t.Fatalf("NewProtocol: %v", err)
	}
	if p == nil {
		t.Fatalf("NewProtocol returned nil protocol")
	}
}
