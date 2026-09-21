package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvFileReadsValuesWithoutOverridingProcessEnv(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, ".env")
	if err := os.WriteFile(path, []byte("# comment\nTEST_DOTENV_VALUE=from-file\nexport TEST_DOTENV_QUOTED=\"hello world\"\nTEST_DOTENV_EXISTING=from-file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_DOTENV_EXISTING", "from-process")
	if err := loadDotEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TEST_DOTENV_VALUE"); got != "from-file" {
		t.Fatalf("file value = %q", got)
	}
	if got := os.Getenv("TEST_DOTENV_QUOTED"); got != "hello world" {
		t.Fatalf("quoted value = %q", got)
	}
	if got := os.Getenv("TEST_DOTENV_EXISTING"); got != "from-process" {
		t.Fatalf("process value was overridden: %q", got)
	}
}

func TestEnvChoiceAcceptsKnownValuesAndFallsBack(t *testing.T) {
	t.Setenv("TEST_AI_REASONING", "LOW")
	if got := envChoice("TEST_AI_REASONING", "none", "none", "low", "high", "max"); got != "low" {
		t.Fatalf("expected normalized choice, got %q", got)
	}
	t.Setenv("TEST_AI_REASONING", "unexpected")
	if got := envChoice("TEST_AI_REASONING", "none", "none", "low", "high", "max"); got != "none" {
		t.Fatalf("expected fallback choice, got %q", got)
	}
}
