//go:build !windows

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nebuk89/cdn_git/internal/node"
	"github.com/nebuk89/cdn_git/internal/security"
	"github.com/nebuk89/cdn_git/internal/transport"
)

func TestGitNativeCloneFetchPushAndCheckpoint(t *testing.T) {
	root := t.TempDir()
	binaryDirectory := filepath.Join(root, "bin")
	if err := os.Mkdir(binaryDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	fabricBinary := filepath.Join(binaryDirectory, "fabric")
	build := exec.Command("go", "build", "-o", fabricBinary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Git-native binary: %v\n%s", err, output)
	}
	for _, name := range []string{"git-remote-fabric", "git-fabric"} {
		if err := os.Link(fabricBinary, filepath.Join(binaryDirectory, name)); err != nil {
			t.Fatal(err)
		}
	}

	data := filepath.Join(root, "authority")
	if _, err := node.Initialize(data, true, nil); err != nil {
		t.Fatal(err)
	}
	remoteURL := "fabric://" + filepath.ToSlash(data) + "?namespace=git-native"
	environment := append(os.Environ(), "PATH="+binaryDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))

	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "init", "-q")
	runGitNativeTest(t, environment, source, "config", "user.name", "Fabric Test")
	runGitNativeTest(t, environment, source, "config", "user.email", "fabric@example.com")
	if err := os.WriteFile(filepath.Join(source, "state.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "add", "state.txt")
	runGitNativeTest(t, environment, source, "commit", "-q", "-m", "one")
	runGitNativeTest(t, environment, source, "branch", "-M", "main")
	runGitNativeTest(t, environment, source, "remote", "add", "origin", remoteURL)
	runGitNativeTest(t, environment, source, "push", "-q", "origin", "HEAD:refs/heads/main")
	runGitNativeTest(t, environment, source, "tag", "-a", "v1", "-m", "annotated")
	tagFailure := runGitNativeTestFailure(t, environment, source, "push", "origin", "refs/tags/v1")
	if !bytes.Contains(tagFailure, []byte("only supports branch destinations")) {
		t.Fatalf("annotated tag push did not explain the branch-only limit:\n%s", tagFailure)
	}

	clone := filepath.Join(root, "clone")
	runGitNativeTest(t, environment, root, "clone", "-q", remoteURL, clone)
	first, err := os.ReadFile(filepath.Join(clone, "state.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != "one\n" {
		t.Fatalf("unexpected cloned content %q", first)
	}

	if err := os.WriteFile(filepath.Join(source, "state.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "commit", "-qam", "two")
	runGitNativeTest(t, environment, source, "push", "-q", "origin", "HEAD:refs/heads/main")
	runGitNativeTest(t, environment, clone, "pull", "-q", "--ff-only")
	second, err := os.ReadFile(filepath.Join(clone, "state.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != "two\n" {
		t.Fatalf("unexpected fetched content %q", second)
	}

	if err := os.WriteFile(filepath.Join(clone, "state.txt"), []byte("clone-divergence\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, clone, "config", "user.name", "Fabric Test")
	runGitNativeTest(t, environment, clone, "config", "user.email", "fabric@example.com")
	runGitNativeTest(t, environment, clone, "commit", "-qam", "clone divergence")
	if err := os.WriteFile(filepath.Join(source, "state.txt"), []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "commit", "-qam", "three")
	runGitNativeTest(t, environment, source, "push", "-q", "origin", "HEAD:refs/heads/main")
	runGitNativeTestFailure(t, environment, clone, "push", "-q", "origin", "HEAD:refs/heads/main")
	runGitNativeTest(t, environment, clone, "push", "-q", "--force", "origin", "HEAD:refs/heads/main")

	forcedClone := filepath.Join(root, "forced-clone")
	runGitNativeTest(t, environment, root, "clone", "-q", remoteURL, forcedClone)
	forced, err := os.ReadFile(filepath.Join(forcedClone, "state.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(forced) != "clone-divergence\n" {
		t.Fatalf("forced Fabric ref did not clone expected content %q", forced)
	}

	status := runGitNativeTest(t, environment, forcedClone, "fabric", "status")
	if !bytes.Contains(status, []byte("Authority ref refs/heads/main")) {
		t.Fatalf("git fabric status did not report the authority ref:\n%s", status)
	}
	checkpoint := runGitNativeTest(t, environment, forcedClone, "fabric", "checkpoint")
	var result gitPushResult
	if err := json.Unmarshal(checkpoint, &result); err != nil {
		t.Fatalf("decode checkpoint: %v\n%s", err, checkpoint)
	}
	if result.Receipt.ID == "" || result.Transition.ID == "" || result.Finalize != "" {
		t.Fatalf("checkpoint did not create local durability without finalization: %+v", result)
	}
}

func TestParseGitRemoteURLRequiresAbsoluteLocalNode(t *testing.T) {
	parsed := &url.URL{
		Scheme:   "fabric",
		Path:     filepath.Join(t.TempDir(), "node"),
		RawQuery: "namespace=demo",
	}
	config, err := parseGitRemoteURL(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	if config.Namespace != "demo" || !config.PrivateSource || !config.PrivateWorkspace {
		t.Fatalf("unexpected remote config: %+v", config)
	}
	if _, err := parseGitRemoteURL("fabric://example.com/node?namespace=demo"); err == nil {
		t.Fatal("remote helper accepted a network host before network fetch support exists")
	}
}

func TestGitNativeForcedPushUsesAdvertisedAuthorityState(t *testing.T) {
	root := t.TempDir()
	binaryDirectory := filepath.Join(root, "bin")
	if err := os.Mkdir(binaryDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	fabricBinary := filepath.Join(binaryDirectory, "fabric")
	build := exec.Command("go", "build", "-o", fabricBinary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Git-native binary: %v\n%s", err, output)
	}
	helperBinary := filepath.Join(binaryDirectory, "git-remote-fabric")
	if err := os.Link(fabricBinary, helperBinary); err != nil {
		t.Fatal(err)
	}
	environment := append(os.Environ(), "PATH="+binaryDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))

	authorityPath := filepath.Join(root, "authority")
	if _, err := node.Initialize(authorityPath, true, nil); err != nil {
		t.Fatal(err)
	}
	remoteURL := "fabric://" + filepath.ToSlash(authorityPath) + "?namespace=lease-race"
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "init", "-q")
	runGitNativeTest(t, environment, source, "config", "user.name", "Fabric Test")
	runGitNativeTest(t, environment, source, "config", "user.email", "fabric@example.com")
	if err := os.WriteFile(filepath.Join(source, "state.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "add", "state.txt")
	runGitNativeTest(t, environment, source, "commit", "-q", "-m", "base")
	runGitNativeTest(t, environment, source, "branch", "-M", "main")
	runGitNativeTest(t, environment, source, "remote", "add", "origin", remoteURL)
	runGitNativeTest(t, environment, source, "push", "-q", "origin", "HEAD:refs/heads/main")

	contender := filepath.Join(root, "contender")
	runGitNativeTest(t, environment, root, "clone", "-q", remoteURL, contender)
	runGitNativeTest(t, environment, contender, "config", "user.name", "Fabric Test")
	runGitNativeTest(t, environment, contender, "config", "user.email", "fabric@example.com")
	if err := os.WriteFile(filepath.Join(contender, "state.txt"), []byte("stale-force\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, contender, "commit", "-qam", "stale force")

	helper := exec.Command(helperBinary, "origin", remoteURL)
	helper.Dir = contender
	helper.Env = append(environment, "GIT_DIR="+filepath.Join(contender, ".git"))
	stdin, err := helper.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := helper.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	helper.Stderr = &stderr
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	if _, err := stdin.Write([]byte("capabilities\n")); err != nil {
		t.Fatal(err)
	}
	readGitRemoteResponse(t, reader)
	if _, err := stdin.Write([]byte("list for-push\n")); err != nil {
		t.Fatal(err)
	}
	readGitRemoteResponse(t, reader)

	if err := os.WriteFile(filepath.Join(source, "state.txt"), []byte("authority-winner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "commit", "-qam", "authority winner")
	runGitNativeTest(t, environment, source, "push", "-q", "origin", "HEAD:refs/heads/main")

	if _, err := stdin.Write([]byte("push +HEAD:refs/heads/main\n\n")); err != nil {
		t.Fatal(err)
	}
	pushResponse := strings.Join(readGitRemoteResponse(t, reader), "\n")
	if !strings.Contains(pushResponse, "error refs/heads/main") ||
		!strings.Contains(pushResponse, "divergent") {
		t.Fatalf("stale forced push was not rejected:\n%s\nstderr:\n%s", pushResponse, stderr.String())
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := helper.Wait(); err != nil {
		t.Fatalf("remote helper exited after reporting the rejected push: %v\n%s", err, stderr.String())
	}

	winnerClone := filepath.Join(root, "winner-clone")
	runGitNativeTest(t, environment, root, "clone", "-q", remoteURL, winnerClone)
	content, err := os.ReadFile(filepath.Join(winnerClone, "state.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "authority-winner\n" {
		t.Fatalf("stale forced push replaced the concurrent authority update: %q", content)
	}
}

func TestGitNativeFetchUsesAdvertisedSnapshot(t *testing.T) {
	root := t.TempDir()
	binaryDirectory := filepath.Join(root, "bin")
	if err := os.Mkdir(binaryDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	fabricBinary := filepath.Join(binaryDirectory, "fabric")
	build := exec.Command("go", "build", "-o", fabricBinary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Git-native binary: %v\n%s", err, output)
	}
	helperBinary := filepath.Join(binaryDirectory, "git-remote-fabric")
	if err := os.Link(fabricBinary, helperBinary); err != nil {
		t.Fatal(err)
	}
	environment := append(os.Environ(), "PATH="+binaryDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))

	authorityPath := filepath.Join(root, "authority")
	if _, err := node.Initialize(authorityPath, true, nil); err != nil {
		t.Fatal(err)
	}
	remoteURL := "fabric://" + filepath.ToSlash(authorityPath) + "?namespace=fetch-race"
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "init", "-q")
	runGitNativeTest(t, environment, source, "config", "user.name", "Fabric Test")
	runGitNativeTest(t, environment, source, "config", "user.email", "fabric@example.com")
	if err := os.WriteFile(filepath.Join(source, "state.txt"), []byte("advertised\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "add", "state.txt")
	runGitNativeTest(t, environment, source, "commit", "-q", "-m", "advertised")
	runGitNativeTest(t, environment, source, "branch", "-M", "main")
	runGitNativeTest(t, environment, source, "remote", "add", "origin", remoteURL)
	runGitNativeTest(t, environment, source, "push", "-q", "origin", "HEAD:refs/heads/main")

	target := filepath.Join(root, "target.git")
	runGitNativeTest(t, environment, root, "init", "-q", "--bare", target)
	helper := exec.Command(helperBinary, "origin", remoteURL)
	helper.Dir = root
	helper.Env = append(environment, "GIT_DIR="+target)
	stdin, err := helper.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := helper.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	helper.Stderr = &stderr
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	if _, err := stdin.Write([]byte("capabilities\n")); err != nil {
		t.Fatal(err)
	}
	readGitRemoteResponse(t, reader)
	if _, err := stdin.Write([]byte("list\n")); err != nil {
		t.Fatal(err)
	}
	listResponse := readGitRemoteResponse(t, reader)
	var advertisedOID string
	for _, line := range listResponse {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "refs/heads/main" {
			advertisedOID = fields[0]
			break
		}
	}
	if advertisedOID == "" {
		t.Fatalf("helper did not advertise main: %v", listResponse)
	}

	if err := os.WriteFile(filepath.Join(source, "state.txt"), []byte("advanced\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "commit", "-qam", "advanced")
	runGitNativeTest(t, environment, source, "push", "-q", "origin", "HEAD:refs/heads/main")

	if _, err := stdin.Write([]byte("fetch " + advertisedOID + " refs/heads/main\n\n")); err != nil {
		t.Fatal(err)
	}
	if response := readGitRemoteResponse(t, reader); len(response) != 0 {
		t.Fatalf("fetch returned an unexpected response: %v", response)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := helper.Wait(); err != nil {
		t.Fatalf("remote helper failed to fetch its advertised object: %v\n%s", err, stderr.String())
	}
	verify := exec.Command("git", "--git-dir", target, "cat-file", "-e", advertisedOID+"^{commit}")
	verify.Env = environment
	if output, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("advertised object was not imported: %v\n%s", err, output)
	}
}

func TestGitNativeRemoteCacheHydratesFromAuthority(t *testing.T) {
	root := t.TempDir()
	binaryDirectory := filepath.Join(root, "bin")
	if err := os.Mkdir(binaryDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	fabricBinary := filepath.Join(binaryDirectory, "fabric")
	build := exec.Command("go", "build", "-o", fabricBinary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Git-native binary: %v\n%s", err, output)
	}
	for _, name := range []string{"git-remote-fabric", "git-fabric"} {
		if err := os.Link(fabricBinary, filepath.Join(binaryDirectory, name)); err != nil {
			t.Fatal(err)
		}
	}
	environment := append(os.Environ(), "PATH="+binaryDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))

	authority, err := node.Initialize(filepath.Join(root, "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	bundle := authority.Domain().EdgeBundle()
	pushCache, err := node.Initialize(filepath.Join(root, "push-cache"), false, &bundle)
	if err != nil {
		t.Fatal(err)
	}
	cloneCache, err := node.Initialize(filepath.Join(root, "clone-cache"), false, &bundle)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := authority.IssueCapability(
		pushCache.PublicKey(),
		"remote-cache",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	capabilityPath := filepath.Join(root, "push-capability.json")
	if err := security.Save(capabilityPath, capability, 0o600); err != nil {
		t.Fatal(err)
	}
	cloneCapability, err := authority.IssueCapability(
		cloneCache.PublicKey(),
		"remote-cache",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	cloneCapabilityPath := filepath.Join(root, "clone-capability.json")
	if err := security.Save(cloneCapabilityPath, cloneCapability, 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(transport.NewServer(authority).Handler())
	defer server.Close()

	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "init", "-q")
	runGitNativeTest(t, environment, source, "config", "user.name", "Fabric Test")
	runGitNativeTest(t, environment, source, "config", "user.email", "fabric@example.com")
	if err := os.WriteFile(filepath.Join(source, "state.txt"), []byte("remote-cache\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "add", "state.txt")
	runGitNativeTest(t, environment, source, "commit", "-q", "-m", "remote cache")
	runGitNativeTest(t, environment, source, "branch", "-M", "main")
	pushURL := fabricGitTestURL(pushCache.Root(), "remote-cache", server.URL, capabilityPath)
	runGitNativeTest(t, environment, source, "remote", "add", "origin", pushURL)
	runGitNativeTest(t, environment, source, "push", "-q", "origin", "HEAD:refs/heads/main")

	cloneURL := fabricGitTestURL(cloneCache.Root(), "remote-cache", server.URL, cloneCapabilityPath)
	clone := filepath.Join(root, "clone")
	runGitNativeTest(t, environment, root, "clone", "-q", cloneURL, clone)
	content, err := os.ReadFile(filepath.Join(clone, "state.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "remote-cache\n" {
		t.Fatalf("remote cache clone produced %q", content)
	}
	if err := os.WriteFile(filepath.Join(source, "state.txt"), []byte("remote-cache-updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "commit", "-qam", "remote cache update")
	runGitNativeTest(t, environment, source, "push", "-q", "origin", "HEAD:refs/heads/main")
	runGitNativeTest(t, environment, clone, "pull", "-q", "--ff-only")
	content, err = os.ReadFile(filepath.Join(clone, "state.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "remote-cache-updated\n" {
		t.Fatalf("peer-backed fetch produced %q", content)
	}
	status := runGitNativeTest(t, environment, clone, "fabric", "status")
	if !bytes.Contains(status, []byte("Authority ref refs/heads/main")) {
		t.Fatalf("peer-backed status did not report the authority ref:\n%s", status)
	}
	checkpoint := runGitNativeTest(t, environment, clone, "fabric", "checkpoint")
	var result gitPushResult
	if err := json.Unmarshal(checkpoint, &result); err != nil {
		t.Fatalf("decode peer-backed checkpoint: %v\n%s", err, checkpoint)
	}
	if result.Receipt.ID == "" || result.Transition.ID == "" || result.Finalize != "" {
		t.Fatalf("peer-backed checkpoint did not create local durability: %+v", result)
	}
}

func TestGitNativePushReportsSuccessAfterFinalizationWhenCacheRefreshFails(t *testing.T) {
	root := t.TempDir()
	binaryDirectory := filepath.Join(root, "bin")
	if err := os.Mkdir(binaryDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	fabricBinary := filepath.Join(binaryDirectory, "fabric")
	build := exec.Command("go", "build", "-o", fabricBinary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Git-native binary: %v\n%s", err, output)
	}
	if err := os.Link(fabricBinary, filepath.Join(binaryDirectory, "git-remote-fabric")); err != nil {
		t.Fatal(err)
	}
	environment := append(os.Environ(), "PATH="+binaryDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))

	authority, err := node.Initialize(filepath.Join(root, "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	bundle := authority.Domain().EdgeBundle()
	cache, err := node.Initialize(filepath.Join(root, "cache"), false, &bundle)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := authority.IssueCapability(
		cache.PublicKey(),
		"post-finalize",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	capabilityPath := filepath.Join(root, "capability.json")
	if err := security.Save(capabilityPath, capability, 0o600); err != nil {
		t.Fatal(err)
	}

	var failAuthority atomic.Bool
	authorityHandler := transport.NewServer(authority).Handler()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v0/authority" && failAuthority.Load() {
			http.Error(response, "injected post-finalize failure", http.StatusServiceUnavailable)
			return
		}
		authorityHandler.ServeHTTP(response, request)
		if request.URL.Path == "/v0/finalize" {
			failAuthority.Store(true)
		}
	}))
	defer server.Close()

	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "init", "-q")
	runGitNativeTest(t, environment, source, "config", "user.name", "Fabric Test")
	runGitNativeTest(t, environment, source, "config", "user.email", "fabric@example.com")
	if err := os.WriteFile(filepath.Join(source, "state.txt"), []byte("finalized\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitNativeTest(t, environment, source, "add", "state.txt")
	runGitNativeTest(t, environment, source, "commit", "-q", "-m", "finalized")
	runGitNativeTest(t, environment, source, "branch", "-M", "main")
	remoteURL := fabricGitTestURL(cache.Root(), "post-finalize", server.URL, capabilityPath)
	runGitNativeTest(t, environment, source, "remote", "add", "origin", remoteURL)
	output := runGitNativeTest(t, environment, source, "push", "origin", "HEAD:refs/heads/main")
	if !bytes.Contains(output, []byte("authority finalized the push")) {
		t.Fatalf("push did not surface the post-finalize cache warning:\n%s", output)
	}
	refs, _, err := authority.Refs()
	if err != nil {
		t.Fatal(err)
	}
	if refs["post-finalize:refs/heads/main"] == "" {
		t.Fatal("authority did not retain the finalized push")
	}
}

func fabricGitTestURL(data, namespace, peer, capability string) string {
	query := url.Values{
		"namespace": []string{namespace},
		"peer":      []string{peer},
	}
	if capability != "" {
		query.Set("capability", capability)
	}
	return "fabric://" + filepath.ToSlash(data) + "?" + query.Encode()
}

func runGitNativeTest(t *testing.T, environment []string, directory string, arguments ...string) []byte {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return output
}

func runGitNativeTestFailure(t *testing.T, environment []string, directory string, arguments ...string) []byte {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	command.Env = environment
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("git %s unexpectedly succeeded\n%s", strings.Join(arguments, " "), output)
	}
	return output
}

func readGitRemoteResponse(t *testing.T, reader *bufio.Reader) []string {
	t.Helper()
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read remote-helper response: %v", err)
		}
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			return lines
		}
		lines = append(lines, line)
	}
}
