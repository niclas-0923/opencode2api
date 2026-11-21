package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestTightenFilePermissions checks that group and other access is removed
// while the owner's read/write bits are preserved.
func TestTightenFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows mode bits do not describe the ACL")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatalf("chmod config: %v", err)
	}
	before, tightened, err := TightenFilePermissions(path)
	if err != nil {
		t.Fatalf("TightenFilePermissions: %v", err)
	}
	if !tightened || before.Perm() != 0644 {
		t.Fatalf("before = %v tightened = %v, want 0644 and true", before, tightened)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Fatalf("mode after tightening = %v, want 0600", mode)
	}
}

// TestTightenFilePermissionsKeepsOwnerOnly checks that an already private file
// is reported as unchanged.
func TestTightenFilePermissionsKeepsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows mode bits do not describe the ACL")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	before, tightened, err := TightenFilePermissions(path)
	if err != nil {
		t.Fatalf("TightenFilePermissions: %v", err)
	}
	if tightened || before.Perm() != 0600 {
		t.Fatalf("before = %v tightened = %v, want 0600 and false", before, tightened)
	}
}
