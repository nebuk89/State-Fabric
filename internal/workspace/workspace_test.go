package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/nebuk89/cdn_git/internal/node"
)

func TestCaptureForkLayerAndMaterialize(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "workspace")
	if err := os.MkdirAll(filepath.Join(directory, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	large := bytes.Repeat([]byte("abcdefghij"), 700000)
	if err := os.WriteFile(filepath.Join(directory, "large.bin"), large, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "nested", "state.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nested/state.txt", filepath.Join(directory, "link")); err != nil {
		t.Fatal(err)
	}
	current, err := node.Initialize(filepath.Join(root, "fabric"), true, nil)
	if err != nil {
		t.Fatal(err)
	}

	first, err := Capture(current, directory, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if first.Files != 3 || first.ChangedFiles != 3 || first.Chunks < 3 {
		t.Fatalf("unexpected first capture: %+v", first)
	}
	fork, err := Fork(current, first.Root)
	if err != nil {
		t.Fatal(err)
	}
	if fork == first.Root {
		t.Fatal("fork reused the parent root")
	}

	if err := os.WriteFile(filepath.Join(directory, "nested", "state.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Capture(current, directory, fork, true)
	if err != nil {
		t.Fatal(err)
	}
	if second.Files != 3 || second.ChangedFiles != 2 || second.DeletedFiles != 1 {
		t.Fatalf("unexpected layered capture: %+v", second)
	}

	destination := filepath.Join(root, "materialized")
	if err := Materialize(current, second.Root, destination); err != nil {
		t.Fatal(err)
	}
	gotLarge, err := os.ReadFile(filepath.Join(destination, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotLarge, large) {
		t.Fatal("large file did not round-trip")
	}
	gotState, err := os.ReadFile(filepath.Join(destination, "nested", "state.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotState) != "second\n" {
		t.Fatalf("unexpected state %q", gotState)
	}
	if _, err := os.Lstat(filepath.Join(destination, "link")); !os.IsNotExist(err) {
		t.Fatal("deleted path was materialized")
	}
	empty, err := os.Stat(filepath.Join(destination, "empty"))
	if err != nil {
		t.Fatal(err)
	}
	if !empty.IsDir() || empty.Mode().Perm() != 0o700 {
		t.Fatalf("empty directory metadata did not round-trip: %s", empty.Mode())
	}
}

func TestCaptureReusesChunksAfterInsertion(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "workspace")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	original := deterministicBytes(8 << 20)
	path := filepath.Join(directory, "state.bin")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	current, err := node.Initialize(filepath.Join(root, "fabric"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Capture(current, directory, "", false)
	if err != nil {
		t.Fatal(err)
	}
	firstEntries, err := Resolve(current, first.Root)
	if err != nil {
		t.Fatal(err)
	}
	updated := append([]byte("prefix"), original...)
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Capture(current, directory, first.Root, false)
	if err != nil {
		t.Fatal(err)
	}
	secondEntries, err := Resolve(current, second.Root)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]struct{})
	for _, id := range firstEntries["state.bin"].Chunks {
		seen[id] = struct{}{}
	}
	reused := 0
	for _, id := range secondEntries["state.bin"].Chunks {
		if _, ok := seen[id]; ok {
			reused++
		}
	}
	if reused == 0 {
		t.Fatal("content-defined chunking reused no chunks after a prefix insertion")
	}
}

func deterministicBytes(size int) []byte {
	result := make([]byte, 0, size)
	var counter uint64
	for len(result) < size {
		var input [8]byte
		binary.LittleEndian.PutUint64(input[:], counter)
		digest := sha256.Sum256(input[:])
		remaining := size - len(result)
		if remaining < len(digest) {
			result = append(result, digest[:remaining]...)
		} else {
			result = append(result, digest[:]...)
		}
		counter++
	}
	return result
}

func TestMaterializeRejectsExistingDestination(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "workspace")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(directory, "state.txt"), []byte("state"), 0o644); err != nil {
		t.Fatal(err)
	}
	current, err := node.Initialize(filepath.Join(root, "fabric"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Capture(current, directory, "", false)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "existing")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Materialize(current, result.Root, destination); err == nil {
		t.Fatal("materialize unexpectedly accepted an existing destination")
	}
}

func TestChunkFileRejectsChangedMetadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after with a different size"), 0o644); err != nil {
		t.Fatal(err)
	}
	current, err := node.Initialize(filepath.Join(root, "fabric"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := chunkFile(current, path, expected, false); err == nil {
		t.Fatal("chunk capture accepted a file that changed before it was opened")
	}
}
