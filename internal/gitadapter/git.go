package gitadapter

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nebuk89/cdn_git/internal/canonical"
	"github.com/nebuk89/cdn_git/internal/filelock"
	"github.com/nebuk89/cdn_git/internal/model"
	"github.com/nebuk89/cdn_git/internal/node"
	"github.com/nebuk89/cdn_git/internal/workspace"
)

const (
	AdapterVersion   = "git-snapshot-v0"
	maxGitBlobSize   = 32 << 20
	maxGitBundleSize = 32 << 20
)

type SourceEntry struct {
	Path     string `json:"path"`
	Mode     string `json:"mode"`
	GitType  string `json:"git_type"`
	GitOID   string `json:"git_oid"`
	FabricID string `json:"fabric_id"`
}

type SourceSnapshot struct {
	AdapterVersion string        `json:"adapter_version"`
	ObjectFormat   string        `json:"object_format"`
	CommitOID      string        `json:"commit_oid"`
	TreeOID        string        `json:"tree_oid"`
	Parents        []string      `json:"parents"`
	CommitObject   []byte        `json:"commit_object"`
	Entries        []SourceEntry `json:"entries"`
	BundleID       string        `json:"bundle_id,omitempty"`
}

type Result struct {
	Roots                 model.Roots `json:"roots"`
	GitCommitOID          string      `json:"git_commit_oid"`
	GitTreeOID            string      `json:"git_tree_oid"`
	TrackedFiles          int         `json:"tracked_files"`
	WorkspaceFiles        int         `json:"workspace_files"`
	WorkspaceChangedFiles int         `json:"workspace_changed_files"`
	WorkspaceLogicalBytes int64       `json:"workspace_logical_bytes"`
	WorkspaceChunks       int         `json:"workspace_chunks"`
}

func SnapshotRepository(
	current *node.Node,
	repository string,
	ref string,
	provenance []byte,
	privateSource bool,
	privateWorkspace bool,
) (Result, error) {
	return SnapshotRepositoryWithWorkspaceParent(
		current,
		repository,
		ref,
		provenance,
		privateSource,
		privateWorkspace,
		"",
	)
}

func SnapshotRepositoryWithWorkspaceParent(
	current *node.Node,
	repository string,
	ref string,
	provenance []byte,
	privateSource bool,
	privateWorkspace bool,
	workspaceParent string,
) (Result, error) {
	absoluteRepository, err := canonicalDirectory(repository)
	if err != nil {
		return Result{}, err
	}
	fabricData, err := isFabricDataRoot(absoluteRepository)
	if err != nil {
		return Result{}, err
	}
	if fabricData {
		return Result{}, errors.New("fabric node data directory cannot be the workspace root")
	}
	commitOID, err := gitOutput(absoluteRepository, "rev-parse", ref+"^{commit}")
	if err != nil {
		return Result{}, err
	}
	treeOID, err := gitOutput(absoluteRepository, "rev-parse", commitOID+"^{tree}")
	if err != nil {
		return Result{}, err
	}
	objectFormat, err := gitOutput(absoluteRepository, "rev-parse", "--show-object-format")
	if err != nil {
		return Result{}, err
	}
	if objectFormat != "sha1" && objectFormat != "sha256" {
		return Result{}, fmt.Errorf("unsupported Git object format %q", objectFormat)
	}
	parentLine, err := gitOutput(absoluteRepository, "show", "-s", "--format=%P", commitOID)
	if err != nil {
		return Result{}, err
	}
	parents := strings.Fields(parentLine)
	sort.Strings(parents)
	commitObject, err := gitBytes(absoluteRepository, "cat-file", "commit", commitOID)
	if err != nil {
		return Result{}, err
	}
	entries, sourceLinks, err := importTrackedFiles(current, absoluteRepository, commitOID, privateSource)
	if err != nil {
		return Result{}, err
	}
	bundle, err := createGitBundle(absoluteRepository, commitOID)
	if err != nil {
		return Result{}, err
	}
	bundleID, err := current.PutObject(
		model.NewGraphObject(model.KindSource, "application/x-git-bundle", bundle, nil),
		privateSource,
	)
	if err != nil {
		return Result{}, err
	}
	sourceLinks = append(sourceLinks, bundleID)
	sort.Strings(sourceLinks)
	sourceLinks = unique(sourceLinks)
	sourcePayload, err := canonical.Marshal(SourceSnapshot{
		AdapterVersion: AdapterVersion,
		ObjectFormat:   objectFormat,
		CommitOID:      commitOID,
		TreeOID:        treeOID,
		Parents:        parents,
		CommitObject:   commitObject,
		Entries:        entries,
		BundleID:       bundleID,
	})
	if err != nil {
		return Result{}, err
	}
	sourceRoot, err := current.PutObject(
		model.NewGraphObject(
			model.KindSource,
			"application/vnd.fabric.git-source-snapshot+json",
			sourcePayload,
			sourceLinks,
		),
		privateSource,
	)
	if err != nil {
		return Result{}, err
	}

	workspaceResult, err := workspace.Capture(
		current,
		absoluteRepository,
		workspaceParent,
		privateWorkspace,
	)
	if err != nil {
		return Result{}, err
	}

	provenanceRoot, err := current.PutObject(
		model.NewGraphObject(
			model.KindProvenance,
			"application/vnd.fabric.provenance+json",
			provenance,
			nil,
		),
		privateWorkspace,
	)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Roots: model.Roots{
			Source:     sourceRoot,
			Workspace:  workspaceResult.Root,
			Provenance: provenanceRoot,
		},
		GitCommitOID:          commitOID,
		GitTreeOID:            treeOID,
		TrackedFiles:          len(entries),
		WorkspaceFiles:        workspaceResult.Files,
		WorkspaceChangedFiles: workspaceResult.ChangedFiles,
		WorkspaceLogicalBytes: workspaceResult.LogicalBytes,
		WorkspaceChunks:       workspaceResult.Chunks,
	}, nil
}

func ExportTrackedFiles(current *node.Node, sourceRoot, destination string) error {
	snapshot, err := LoadSourceSnapshot(current, sourceRoot)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("export destination must not already exist")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := validateExportEntries(snapshot.Entries); err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	filesystemRoot, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer filesystemRoot.Close()
	for _, entry := range snapshot.Entries {
		relative, err := safeRelative(entry.Path)
		if err != nil {
			return err
		}
		object, err := current.GetObject(entry.FabricID)
		if err != nil {
			return err
		}
		if got, err := gitBlobOID(object.Payload, snapshot.ObjectFormat); err != nil {
			return err
		} else if got != entry.GitOID {
			return fmt.Errorf("Git blob identity mismatch for %s: got %s want %s", entry.Path, got, entry.GitOID)
		}
		if err := mkdirAllRoot(filesystemRoot, filepath.Dir(relative)); err != nil {
			return err
		}
		switch entry.Mode {
		case "120000":
			target := filepath.Join(destination, relative)
			if err := os.Symlink(string(object.Payload), target); err != nil {
				return err
			}
		case "100755":
			if err := writeRootFile(filesystemRoot, relative, object.Payload, 0o755); err != nil {
				return err
			}
		default:
			if err := writeRootFile(filesystemRoot, relative, object.Payload, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func LoadSourceSnapshot(current *node.Node, sourceRoot string) (SourceSnapshot, error) {
	root, err := current.GetObject(sourceRoot)
	if err != nil {
		return SourceSnapshot{}, err
	}
	if root.Kind != model.KindSource || root.MediaType != "application/vnd.fabric.git-source-snapshot+json" {
		return SourceSnapshot{}, errors.New("object is not a Git source snapshot")
	}
	var snapshot SourceSnapshot
	if err := canonical.Decode(root.Payload, &snapshot); err != nil {
		return SourceSnapshot{}, err
	}
	if snapshot.AdapterVersion != AdapterVersion {
		return SourceSnapshot{}, errors.New("unsupported Git snapshot adapter version")
	}
	if snapshot.ObjectFormat != "sha1" && snapshot.ObjectFormat != "sha256" {
		return SourceSnapshot{}, fmt.Errorf("unsupported Git object format %q", snapshot.ObjectFormat)
	}
	expectedLinks := make([]string, 0, len(snapshot.Entries)+1)
	for _, entry := range snapshot.Entries {
		expectedLinks = append(expectedLinks, entry.FabricID)
	}
	if snapshot.BundleID != "" {
		expectedLinks = append(expectedLinks, snapshot.BundleID)
	}
	sort.Strings(expectedLinks)
	expectedLinks = unique(expectedLinks)
	if strings.Join(root.Links, "\x00") != strings.Join(expectedLinks, "\x00") {
		return SourceSnapshot{}, errors.New("Git source snapshot links do not bind its object inventory")
	}
	return snapshot, nil
}

func ImportGitBundle(current *node.Node, sourceRoot, gitDirectory string) error {
	snapshot, err := LoadSourceSnapshot(current, sourceRoot)
	if err != nil {
		return err
	}
	if snapshot.BundleID == "" {
		return errors.New("Git source snapshot predates remote-helper bundle support")
	}
	bundle, err := current.GetObject(snapshot.BundleID)
	if err != nil {
		return err
	}
	if bundle.Kind != model.KindSource || bundle.MediaType != "application/x-git-bundle" {
		return errors.New("source snapshot bundle object has the wrong type")
	}
	file, err := os.CreateTemp("", "fabric-git-*.bundle")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(bundle.Payload); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if _, err := gitBytesWithDirectory(gitDirectory, "bundle", "unbundle", path); err != nil {
		return err
	}
	if _, err := gitBytesWithDirectory(gitDirectory, "cat-file", "-e", snapshot.CommitOID+"^{commit}"); err != nil {
		return fmt.Errorf("imported bundle does not contain commit %s: %w", snapshot.CommitOID, err)
	}
	return nil
}

func importTrackedFiles(current *node.Node, repository, commitOID string, private bool) ([]SourceEntry, []string, error) {
	output, err := gitBytes(repository, "ls-tree", "-r", "-z", "--full-tree", commitOID)
	if err != nil {
		return nil, nil, err
	}

	records := bytes.Split(output, []byte{0})
	entries := make([]SourceEntry, 0, len(records))
	links := make([]string, 0, len(records))
	for _, record := range records {
		if len(record) == 0 {
			continue
		}
		tab := bytes.IndexByte(record, '\t')
		if tab < 0 {
			return nil, nil, errors.New("unexpected git ls-tree output")
		}
		fields := strings.Fields(string(record[:tab]))
		if len(fields) != 3 {
			return nil, nil, errors.New("unexpected git ls-tree metadata")
		}
		if fields[1] == "commit" {
			return nil, nil, fmt.Errorf("submodule %q is not supported by the v0 Git adapter", string(record[tab+1:]))
		}
		pathBytes := record[tab+1:]
		if !utf8.Valid(pathBytes) {
			return nil, nil, errors.New("v0 Git adapter does not support non-UTF-8 paths")
		}
		sizeOutput, err := gitBytes(repository, "cat-file", "-s", fields[2])
		if err != nil {
			return nil, nil, err
		}
		size, err := strconv.ParseInt(strings.TrimSpace(string(sizeOutput)), 10, 64)
		if err != nil || size < 0 {
			return nil, nil, fmt.Errorf("invalid Git blob size for %q", string(pathBytes))
		}
		if size > maxGitBlobSize {
			return nil, nil, fmt.Errorf(
				"Git blob %q is %d bytes; v0.1 supports at most %d bytes per tracked blob",
				string(pathBytes),
				size,
				maxGitBlobSize,
			)
		}
		content, err := gitBytes(repository, "cat-file", "blob", fields[2])
		if err != nil {
			return nil, nil, err
		}
		fabricID, err := current.PutObject(
			model.NewGraphObject(model.KindSource, "application/octet-stream", content, nil),
			private,
		)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, SourceEntry{
			Path:     string(pathBytes),
			Mode:     fields[0],
			GitType:  fields[1],
			GitOID:   fields[2],
			FabricID: fabricID,
		})
		links = append(links, fabricID)
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Path < entries[right].Path
	})
	sort.Strings(links)
	return entries, unique(links), nil
}

func createGitBundle(repository, commitOID string) ([]byte, error) {
	commonDirectory, err := gitOutput(repository, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(commonDirectory) {
		commonDirectory = filepath.Join(repository, commonDirectory)
	}
	commonDirectory, err = filepath.Abs(commonDirectory)
	if err != nil {
		return nil, err
	}
	var bundle []byte
	err = filelock.With(filepath.Join(commonDirectory, "fabric-bundle.lock"), func() error {
		var createErr error
		bundle, createErr = createGitBundleLocked(repository, commitOID)
		return createErr
	})
	return bundle, err
}

func createGitBundleLocked(repository, commitOID string) (bundle []byte, resultErr error) {
	const bundleRef = "refs/fabric/bundle"
	if _, err := gitBytes(repository, "update-ref", bundleRef, commitOID); err != nil {
		return nil, err
	}
	defer func() {
		if _, err := gitBytes(repository, "update-ref", "-d", bundleRef, commitOID); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("remove temporary Git bundle ref: %w", err)
		}
	}()

	command := exec.Command("git", "-C", repository, "bundle", "create", "-", bundleRef)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	bundle, err = io.ReadAll(io.LimitReader(stdout, maxGitBundleSize+1))
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, err
	}
	if len(bundle) > maxGitBundleSize {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf(
			"Git history bundle exceeds the v0.1 limit of %d bytes",
			maxGitBundleSize,
		)
	}
	if err := command.Wait(); err != nil {
		return nil, fmt.Errorf(
			"git bundle create: %s",
			strings.TrimSpace(stderr.String()),
		)
	}
	return bundle, nil
}

func canonicalDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", path)
	}
	return canonical, nil
}

func isFabricDataRoot(path string) (bool, error) {
	for _, name := range []string{"config.json", "identity.json", "domain.json"} {
		info, err := os.Stat(filepath.Join(path, name))
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, nil
		}
	}
	return true, nil
}

func gitOutput(repository string, arguments ...string) (string, error) {
	output, err := gitBytes(repository, arguments...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func gitBytes(repository string, arguments ...string) ([]byte, error) {
	commandArguments := append([]string{"-C", repository}, arguments...)
	command := exec.Command("git", commandArguments...)
	return commandOutput(command, arguments)
}

func gitBytesWithDirectory(gitDirectory string, arguments ...string) ([]byte, error) {
	commandArguments := append([]string{"--git-dir", gitDirectory}, arguments...)
	command := exec.Command("git", commandArguments...)
	return commandOutput(command, arguments)
}

func commandOutput(command *exec.Cmd, arguments []string) ([]byte, error) {
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return nil, fmt.Errorf("git %s: %s", strings.Join(arguments, " "), strings.TrimSpace(string(exitError.Stderr)))
		}
		return nil, fmt.Errorf("git %s: %w", strings.Join(arguments, " "), err)
	}
	return output, nil
}

func gitBlobOID(payload []byte, objectFormat string) (string, error) {
	header := []byte("blob " + strconv.Itoa(len(payload)) + "\x00")
	var digest hash.Hash
	switch objectFormat {
	case "sha1":
		digest = sha1.New()
	case "sha256":
		digest = sha256.New()
	default:
		return "", fmt.Errorf("unsupported Git object format %q", objectFormat)
	}
	digest.Write(header)
	digest.Write(payload)
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func safeRelative(relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe snapshot path %q", relative)
	}
	return clean, nil
}

func validateExportEntries(entries []SourceEntry) error {
	types := make(map[string]string, len(entries))
	for _, entry := range entries {
		relative, err := safeRelative(entry.Path)
		if err != nil {
			return err
		}
		if _, exists := types[relative]; exists {
			return fmt.Errorf("duplicate snapshot path %q", entry.Path)
		}
		types[relative] = entry.Mode
	}
	for path, mode := range types {
		if mode != "120000" {
			continue
		}
		prefix := path + string(filepath.Separator)
		for other := range types {
			if strings.HasPrefix(other, prefix) {
				return fmt.Errorf("symlink path %q is an ancestor of %q", path, other)
			}
		}
	}
	return nil
}

func mkdirAllRoot(root *os.Root, directory string) error {
	if directory == "." {
		return nil
	}
	current := ""
	for _, component := range strings.Split(filepath.Clean(directory), string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		if current == "" {
			current = component
		} else {
			current = filepath.Join(current, component)
		}
		if err := root.Mkdir(current, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	return nil
}

func writeRootFile(root *os.Root, path string, payload []byte, mode os.FileMode) error {
	file, err := root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(payload); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func unique(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}
