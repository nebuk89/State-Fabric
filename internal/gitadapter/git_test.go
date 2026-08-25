package gitadapter

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nebuk89/cdn_git/internal/node"
)

func TestSnapshotAndExportTrackedFiles(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "init", "-q")
	runGit(t, repository, "config", "user.name", "Fabric Test")
	runGit(t, repository, "config", "user.email", "fabric@example.com")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "script.sh"), []byte("#!/bin/sh\necho fabric\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("README.md", filepath.Join(repository, "link")); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(repository, "ignored.tmp"), []byte("workspace-only"), 0o644); err != nil {
		t.Fatal(err)
	}

	current, err := node.Initialize(filepath.Join(repository, "edge-data"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := node.Initialize(filepath.Join(repository, "other-node"), true, nil); err != nil {
		t.Fatal(err)
	}
	result, err := SnapshotRepository(current, repository, "HEAD", []byte(`{"agent":"test"}`), false, true)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "update-ref", "refs/fabric/bundle", "HEAD")
	repeated, err := SnapshotRepository(current, repository, "HEAD", []byte(`{"agent":"test"}`), false, true)
	if err != nil {
		t.Fatal(err)
	}
	firstSnapshot, err := LoadSourceSnapshot(current, result.Roots.Source)
	if err != nil {
		t.Fatal(err)
	}
	repeatedSnapshot, err := LoadSourceSnapshot(current, repeated.Roots.Source)
	if err != nil {
		t.Fatal(err)
	}
	if repeatedSnapshot.BundleID != firstSnapshot.BundleID || repeated.Roots.Source != result.Roots.Source {
		t.Fatalf(
			"unchanged Git state did not deduplicate: first bundle/source=%s/%s repeated=%s/%s",
			firstSnapshot.BundleID,
			result.Roots.Source,
			repeatedSnapshot.BundleID,
			repeated.Roots.Source,
		)
	}
	bundleRefs := exec.Command("git", "-C", repository, "for-each-ref", "--format=%(refname)", "refs/fabric")
	if output, err := bundleRefs.CombinedOutput(); err != nil {
		t.Fatalf("inspect temporary bundle refs: %v\n%s", err, output)
	} else if len(bytes.TrimSpace(output)) != 0 {
		t.Fatalf("snapshot left temporary bundle refs behind: %s", output)
	}
	if result.TrackedFiles != 3 || result.WorkspaceFiles != 4 {
		t.Fatalf("unexpected file counts: tracked=%d workspace=%d", result.TrackedFiles, result.WorkspaceFiles)
	}
	bare := filepath.Join(t.TempDir(), "clone.git")
	runGit(t, "", "init", "--bare", "-q", bare)
	if err := ImportGitBundle(current, result.Roots.Source, bare); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "--git-dir", bare, "cat-file", "-e", result.GitCommitOID+"^{commit}")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("verify imported bundle: %v\n%s", err, output)
	}

	exported := filepath.Join(t.TempDir(), "exported")
	if err := ExportTrackedFiles(current, result.Roots.Source, exported); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"README.md", "script.sh"} {
		original, err := os.ReadFile(filepath.Join(repository, path))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(exported, path))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, original) {
			t.Fatalf("%s did not round-trip", path)
		}
	}
	target, err := os.Readlink(filepath.Join(exported, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "README.md" {
		t.Fatalf("unexpected symlink target %q", target)
	}
	if _, err := os.Stat(filepath.Join(exported, "ignored.tmp")); !os.IsNotExist(err) {
		t.Fatal("untracked workspace file leaked into source projection")
	}
}

func TestSHA256RepositorySnapshotRoundTrip(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "-C", repository, "init", "-q", "--object-format=sha256")
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("Git SHA-256 repositories are unavailable: %v\n%s", err, output)
	}
	runGit(t, repository, "config", "user.name", "Fabric Test")
	runGit(t, repository, "config", "user.email", "fabric@example.com")
	if err := os.WriteFile(filepath.Join(repository, "state.txt"), []byte("sha256-state\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-q", "-m", "sha256")

	current, err := node.Initialize(filepath.Join(t.TempDir(), "fabric"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := SnapshotRepository(current, repository, "HEAD", []byte("{}"), false, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.GitCommitOID) != 64 {
		t.Fatalf("expected SHA-256 Git OID, got %q", result.GitCommitOID)
	}
	exported := filepath.Join(t.TempDir(), "exported")
	if err := ExportTrackedFiles(current, result.Roots.Source, exported); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(exported, "state.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "sha256-state\n" {
		t.Fatalf("unexpected exported content %q", got)
	}
}

func TestSnapshotResolvesSymlinkedRepository(t *testing.T) {
	parent := t.TempDir()
	repository := filepath.Join(parent, "repo")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "init", "-q")
	runGit(t, repository, "config", "user.name", "Fabric Test")
	runGit(t, repository, "config", "user.email", "fabric@example.com")
	if err := os.WriteFile(filepath.Join(repository, "state.txt"), []byte("linked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-q", "-m", "linked")

	link := filepath.Join(parent, "repo-link")
	if err := os.Symlink(repository, link); err != nil {
		t.Fatal(err)
	}
	current, err := node.Initialize(filepath.Join(parent, "fabric"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := SnapshotRepository(current, link, "HEAD", []byte("{}"), false, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkspaceFiles != 1 {
		t.Fatalf("expected symlinked repository workspace file, got %d", result.WorkspaceFiles)
	}
}

func TestSnapshotRejectsFabricDataRootRepository(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "fabric-repository")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "init", "-q")
	runGit(t, repository, "config", "user.name", "Fabric Test")
	runGit(t, repository, "config", "user.email", "fabric@example.com")
	if err := os.WriteFile(filepath.Join(repository, "state.txt"), []byte("state\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "state.txt")
	runGit(t, repository, "commit", "-q", "-m", "state")
	if _, err := node.Initialize(repository, true, nil); err != nil {
		t.Fatal(err)
	}
	current, err := node.Initialize(filepath.Join(t.TempDir(), "edge"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SnapshotRepository(current, repository, "HEAD", []byte("{}"), false, true); err == nil {
		t.Fatal("snapshot unexpectedly accepted a Fabric data root as its repository")
	}
}

func TestSnapshotRejectsTrackedBlobAboveTransportLimit(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "init", "-q")
	runGit(t, repository, "config", "user.name", "Fabric Test")
	runGit(t, repository, "config", "user.email", "fabric@example.com")
	if err := os.WriteFile(
		filepath.Join(repository, "oversized.bin"),
		bytes.Repeat([]byte{'x'}, maxGitBlobSize+1),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "oversized.bin")
	runGit(t, repository, "commit", "-q", "-m", "oversized")
	current, err := node.Initialize(filepath.Join(t.TempDir(), "fabric"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SnapshotRepository(current, repository, "HEAD", []byte("{}"), false, true); err == nil {
		t.Fatal("snapshot accepted a tracked blob larger than the transport-safe limit")
	}
}

func TestExportRejectsExistingDestination(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "init", "-q")
	runGit(t, repository, "config", "user.name", "Fabric Test")
	runGit(t, repository, "config", "user.email", "fabric@example.com")
	if err := os.WriteFile(filepath.Join(repository, "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-q", "-m", "initial")
	current, err := node.Initialize(filepath.Join(t.TempDir(), "fabric"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := SnapshotRepository(current, repository, "HEAD", []byte("{}"), false, true)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "existing")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ExportTrackedFiles(current, result.Roots.Source, destination); err == nil {
		t.Fatal("export unexpectedly wrote into an existing destination")
	}
}

func runGit(t *testing.T, repository string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", arguments, err, output)
	}
}
