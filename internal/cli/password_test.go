package cli

import (
	"testing"

	"github.com/terayu/pgwash/internal/config"
)

func TestApplyEnvPasswordFillsFromEnvWhenEmpty(t *testing.T) {
	t.Setenv("PGPASSWORD", "secret")

	cfg := applyEnvPassword(config.Config{})
	if cfg.Password != "secret" {
		t.Errorf("Password = %q, want secret", cfg.Password)
	}
}

func TestApplyEnvPasswordKeepsExistingValue(t *testing.T) {
	t.Setenv("PGPASSWORD", "from-env")

	cfg := applyEnvPassword(config.Config{Password: "already-set"})
	if cfg.Password != "already-set" {
		t.Errorf("Password = %q, want already-set (既存値を上書きしてはならない)", cfg.Password)
	}
}

func TestApplyEnvPasswordNoEnvLeavesEmpty(t *testing.T) {
	t.Setenv("PGPASSWORD", "")

	cfg := applyEnvPassword(config.Config{})
	if cfg.Password != "" {
		t.Errorf("Password = %q, want empty", cfg.Password)
	}
}
