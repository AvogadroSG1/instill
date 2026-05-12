package instill

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLibraryPathUsesEnvironmentFirst(t *testing.T) {
	library := createLibrary(t, "docker")
	t.Setenv(envLibraryPath, library)

	got, err := ResolveLibraryPath(ConfigResolverOptions{})
	if err != nil {
		t.Fatalf("ResolveLibraryPath() error = %v", err)
	}
	if got != library {
		t.Fatalf("ResolveLibraryPath() = %q, want %q", got, library)
	}
}

func TestResolveLibraryPathFailsWithoutTTYOrConfig(t *testing.T) {
	t.Setenv(envLibraryPath, "")
	t.Setenv("HOME", t.TempDir())

	_, err := ResolveLibraryPath(ConfigResolverOptions{})
	if err == nil {
		t.Fatal("ResolveLibraryPath() error = nil, want env error")
	}
	if got := ExitCode(err); got != ExitEnvironment {
		t.Fatalf("ExitCode(err) = %d, want %d", got, ExitEnvironment)
	}
}

func TestResolveLibraryPathReadsConfig(t *testing.T) {
	library := createLibrary(t, "docker")
	home := t.TempDir()
	t.Setenv(envLibraryPath, "")
	t.Setenv("HOME", home)

	configPath, err := userConfigPath()
	if err != nil {
		t.Fatalf("userConfigPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`{"library_path":"`+library+`"}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := ResolveLibraryPath(ConfigResolverOptions{})
	if err != nil {
		t.Fatalf("ResolveLibraryPath() error = %v", err)
	}
	if got != library {
		t.Fatalf("ResolveLibraryPath() = %q, want %q", got, library)
	}
}

func TestUserConfigPathIgnoresXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))

	got, err := userConfigPath()
	if err != nil {
		t.Fatalf("userConfigPath() error = %v", err)
	}

	want := filepath.Join(home, ".config", configDirName, configFileName)
	if got != want {
		t.Fatalf("userConfigPath() = %q, want %q", got, want)
	}
}

func TestPromptForLibraryPathUsesDefault(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	if _, err := write.WriteString("\n"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := write.Close(); err != nil {
		t.Fatalf("Close(write) error = %v", err)
	}
	t.Cleanup(func() {
		if err := read.Close(); err != nil {
			t.Fatalf("Close(read) error = %v", err)
		}
	})

	var stderr bytes.Buffer
	got, err := promptForLibraryPath(ConfigResolverOptions{
		Stdin:  read,
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("promptForLibraryPath() error = %v", err)
	}
	if got != defaultLibraryPath() {
		t.Fatalf("promptForLibraryPath() = %q, want default %q", got, defaultLibraryPath())
	}
	if !strings.Contains(stderr.String(), "Library path [") {
		t.Fatalf("stderr = %q, want prompt", stderr.String())
	}
}
