package cli

import (
	"bytes"
	"testing"
)

func TestBootstrapCommandRunsEnsureAPMOnly(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	calls := []string{}
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"bootstrap"},
		runner: recordingRunner(&calls),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if len(calls) != 1 || calls[0] != "apm --version" {
		t.Fatalf("calls = %#v, want only apm version check", calls)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want silent success", stdout.String())
	}
}
