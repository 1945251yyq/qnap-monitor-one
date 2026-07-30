package monitor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadCPU(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "stat")
	if err := os.WriteFile(path, []byte("cpu  100 20 30 400 50 5 6 0 0 0\ncpu0 1 1 1 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sample, err := readCPU(path)
	if err != nil {
		t.Fatal(err)
	}
	if sample.total != 611 || sample.idle != 450 {
		t.Fatalf("unexpected CPU sample: %#v", sample)
	}
}

func TestReadMemoryUsesAvailable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "meminfo")
	content := "MemTotal: 1000 kB\nMemFree: 100 kB\nMemAvailable: 400 kB\nBuffers: 20 kB\nCached: 30 kB\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	total, available, err := readMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1024000 || available != 409600 {
		t.Fatalf("unexpected memory: total=%d available=%d", total, available)
	}
}

func TestSensitiveArgumentsAreMasked(t *testing.T) {
	input := "server --token very-secret password=hello normal=yes"
	got := sensitiveArgument.ReplaceAllString(input, "$1$2***")
	if got != "server --token *** password=*** normal=yes" {
		t.Fatalf("unexpected masked command: %q", got)
	}
}
