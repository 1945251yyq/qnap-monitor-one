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

func TestReadMemoryMatchesQNAPReclaimableMemory(t *testing.T) {
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
	if total != 1024000 || available != 153600 {
		t.Fatalf("unexpected memory: total=%d available=%d", total, available)
	}
}

func TestReadHostMemoryIncludesEvictableARC(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "spl/kstat/zfs"), 0o700); err != nil {
		t.Fatal(err)
	}
	meminfo := filepath.Join(root, "meminfo")
	content := "MemTotal: 1000 kB\nMemFree: 100 kB\nBuffers: 20 kB\nCached: 30 kB\n"
	if err := os.WriteFile(meminfo, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	arcstats := "13 1 0x01 100 4800 1\nname type data\nsize 4 307200\nevictable 4 204800\n"
	if err := os.WriteFile(filepath.Join(root, "spl/kstat/zfs/arcstats"), []byte(arcstats), 0o600); err != nil {
		t.Fatal(err)
	}
	total, available, size, evictable, err := readHostMemory(meminfo, root)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1024000 || available != 358400 || size != 307200 || evictable != 204800 {
		t.Fatalf("unexpected host memory: total=%d available=%d size=%d evictable=%d", total, available, size, evictable)
	}
}

func TestCollectSharesFiltersTemplatesAndDeduplicatesRealFolders(t *testing.T) {
	root := t.TempDir()
	shareRoot := filepath.Join(root, "share")
	configRoot := filepath.Join(root, "config")
	realFolder := filepath.Join(shareRoot, "ZFS1_DATA", "Public")
	if err := os.MkdirAll(realFolder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realFolder, filepath.Join(shareRoot, "Public")); err != nil {
		t.Fatal(err)
	}
	smb := "[global]\npath = /tmp\n[home]\npath = %H\n[Public]\npath = /share/Public\n[Public Alias]\npath = /share/Public\n"
	if err := os.WriteFile(filepath.Join(configRoot, "smb.conf"), []byte(smb), 0o600); err != nil {
		t.Fatal(err)
	}
	collector := NewCollector(Config{ShareRoot: shareRoot, ConfigRoot: configRoot})
	shares := collector.collectShares()
	if len(shares) != 1 || shares[0].Name != "Public" || shares[0].VolumeName != "ZFS1_DATA" {
		t.Fatalf("unexpected shares: %#v", shares)
	}
}

func TestSensitiveArgumentsAreMasked(t *testing.T) {
	input := "server --token very-secret password=hello normal=yes"
	got := sensitiveArgument.ReplaceAllString(input, "$1$2***")
	if got != "server --token *** password=*** normal=yes" {
		t.Fatalf("unexpected masked command: %q", got)
	}
}
