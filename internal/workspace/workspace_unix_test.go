//go:build !windows

package workspace

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/nebuk89/cdn_git/internal/node"
)

func TestCaptureRejectsUnsupportedFilesystemEntries(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "workspace")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(directory, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	current, err := node.Initialize(filepath.Join(root, "fabric"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(current, directory, "", false); err == nil {
		t.Fatal("capture silently ignored an unsupported FIFO")
	}
}

func TestMaterializeAppliesReadOnlyDirectoryModesAfterChildren(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "workspace")
	readOnly := filepath.Join(directory, "readonly")
	if err := os.MkdirAll(readOnly, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(readOnly, "child.txt"), []byte("state"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o755) })
	current, err := node.Initialize(filepath.Join(root, "fabric"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := Capture(current, directory, "", false)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "materialized")
	if err := Materialize(current, capture.Root, destination); err != nil {
		t.Fatal(err)
	}
	materializedDirectory := filepath.Join(destination, "readonly")
	t.Cleanup(func() { _ = os.Chmod(materializedDirectory, 0o755) })
	info, err := os.Stat(materializedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o555 {
		t.Fatalf("read-only directory mode did not round-trip: %s", info.Mode())
	}
	content, err := os.ReadFile(filepath.Join(materializedDirectory, "child.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "state" {
		t.Fatalf("unexpected materialized child %q", content)
	}
}
