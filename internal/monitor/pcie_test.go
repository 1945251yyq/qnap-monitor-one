package monitor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQPKGInstallPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qpkg.conf")
	content := "[Other]\nInstall_Path = /share/other\n[NVIDIA_GPU_DRV]\nInstall_Path = \"/share/CACHEDEV1_DATA/.qpkg/NVIDIA_GPU_DRV\"\nEnabled = TRUE\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got := qpkgInstallPath(path, "NVIDIA_GPU_DRV")
	want := "/share/CACHEDEV1_DATA/.qpkg/NVIDIA_GPU_DRV"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNormalizeNVIDIABDF(t *testing.T) {
	cases := map[string]string{
		"00000000:01:00.0": "0000:01:00.0",
		"0000:02:00.0":     "0000:02:00.0",
		"03:00.0":          "0000:03:00.0",
	}
	for input, want := range cases {
		if got := normalizeNVIDIABDF(input); got != want {
			t.Fatalf("%q: got %q want %q", input, got, want)
		}
	}
}
