package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/nebuk89/cdn_git/internal/canonical"
	"github.com/nebuk89/cdn_git/internal/model"
	"github.com/nebuk89/cdn_git/internal/node"
)

const (
	AdapterVersion = "workspace-layer-v0.1"
	rootMediaType  = "application/vnd.fabric.workspace-root+json"
	layerMediaType = "application/vnd.fabric.workspace-layer+json"
	chunkMediaType = "application/vnd.fabric.workspace-chunk"
	minChunkSize   = 256 << 10
	averageChunk   = 1 << 20
	maxChunkSize   = 4 << 20
)

type Entry struct {
	Path   string   `json:"path"`
	Mode   uint32   `json:"mode"`
	Type   string   `json:"type"`
	Size   int64    `json:"size"`
	Chunks []string `json:"chunks"`
}

type Layer struct {
	AdapterVersion string   `json:"adapter_version"`
	Entries        []Entry  `json:"entries"`
	Deleted        []string `json:"deleted"`
}

type Root struct {
	AdapterVersion string `json:"adapter_version"`
	Parent         string `json:"parent"`
	Layer          string `json:"layer"`
}

type CaptureResult struct {
	Root         string `json:"root"`
	Files        int    `json:"files"`
	ChangedFiles int    `json:"changed_files"`
	DeletedFiles int    `json:"deleted_files"`
	LogicalBytes int64  `json:"logical_bytes"`
	Chunks       int    `json:"chunks"`
}

func Capture(current *node.Node, directory, parent string, private bool) (CaptureResult, error) {
	root, err := canonicalDirectory(directory)
	if err != nil {
		return CaptureResult{}, err
	}
	dataRoot, err := canonicalDirectory(current.Root())
	if err != nil {
		return CaptureResult{}, err
	}
	if root == dataRoot {
		return CaptureResult{}, errors.New("fabric node data directory cannot be the workspace root")
	}
	if fabricData, err := isFabricDataRoot(root); err != nil {
		return CaptureResult{}, err
	} else if fabricData {
		return CaptureResult{}, errors.New("fabric node data directory cannot be the workspace root")
	}

	parentEntries := make(map[string]Entry)
	if parent != "" {
		parentEntries, err = Resolve(current, parent)
		if err != nil {
			return CaptureResult{}, fmt.Errorf("resolve parent workspace: %w", err)
		}
		private = private || strings.HasPrefix(parent, "priv:")
	}

	entries := make(map[string]Entry)
	var logicalBytes int64
	chunkSet := make(map[string]struct{})
	err = filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if !utf8.ValidString(relative) {
			return errors.New("workspace adapter does not support non-UTF-8 paths")
		}
		if item.IsDir() {
			if item.Name() == ".git" || item.Name() == ".fabric" || path == dataRoot {
				return filepath.SkipDir
			}
			fabricData, err := isFabricDataRoot(path)
			if err != nil {
				return err
			}
			if fabricData {
				return filepath.SkipDir
			}
			info, err := item.Info()
			if err != nil {
				return err
			}
			entries[filepath.ToSlash(relative)] = Entry{
				Path: filepath.ToSlash(relative),
				Mode: uint32(info.Mode().Perm()),
				Type: "directory",
			}
			return nil
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		entry := Entry{
			Path: filepath.ToSlash(relative),
			Mode: uint32(info.Mode().Perm()),
			Size: info.Size(),
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			entry.Type = "symlink"
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entry.Size = int64(len(target))
			id, err := putChunk(current, []byte(target), private)
			if err != nil {
				return err
			}
			entry.Chunks = []string{id}
			chunkSet[id] = struct{}{}
		case info.Mode().IsRegular():
			entry.Type = "file"
			chunks, size, err := chunkFile(current, path, info, private)
			if err != nil {
				return err
			}
			entry.Chunks = chunks
			entry.Size = size
			for _, id := range chunks {
				chunkSet[id] = struct{}{}
			}
		default:
			return fmt.Errorf("unsupported workspace entry %q with mode %s", relative, info.Mode())
		}
		logicalBytes += entry.Size
		entries[entry.Path] = entry
		return nil
	})
	if err != nil {
		return CaptureResult{}, err
	}

	changed := make([]Entry, 0, len(entries))
	for path, entry := range entries {
		if previous, ok := parentEntries[path]; !ok || !equalEntry(previous, entry) {
			changed = append(changed, entry)
		}
	}
	deleted := make([]string, 0)
	for path := range parentEntries {
		if _, ok := entries[path]; !ok {
			deleted = append(deleted, path)
		}
	}
	sort.Slice(changed, func(left, right int) bool {
		return changed[left].Path < changed[right].Path
	})
	sort.Strings(deleted)

	layer := Layer{
		AdapterVersion: AdapterVersion,
		Entries:        changed,
		Deleted:        deleted,
	}
	layerPayload, err := canonical.Marshal(layer)
	if err != nil {
		return CaptureResult{}, err
	}
	layerLinks := make([]string, 0, len(chunkSet))
	for _, entry := range changed {
		layerLinks = append(layerLinks, entry.Chunks...)
	}
	layerID, err := current.PutObject(
		model.NewGraphObject(model.KindWorkspace, layerMediaType, layerPayload, uniqueSorted(layerLinks)),
		private,
	)
	if err != nil {
		return CaptureResult{}, err
	}
	rootPayload, err := canonical.Marshal(Root{
		AdapterVersion: AdapterVersion,
		Parent:         parent,
		Layer:          layerID,
	})
	if err != nil {
		return CaptureResult{}, err
	}
	rootLinks := []string{layerID}
	if parent != "" {
		rootLinks = append(rootLinks, parent)
	}
	rootID, err := current.PutObject(
		model.NewGraphObject(model.KindWorkspace, rootMediaType, rootPayload, rootLinks),
		private,
	)
	if err != nil {
		return CaptureResult{}, err
	}
	fileCount := countFiles(entries)
	changedFileCount := countEntryFiles(changed)
	return CaptureResult{
		Root:         rootID,
		Files:        fileCount,
		ChangedFiles: changedFileCount,
		DeletedFiles: countDeletedFiles(parentEntries, deleted),
		LogicalBytes: logicalBytes,
		Chunks:       len(chunkSet),
	}, nil
}

func Fork(current *node.Node, parent string) (string, error) {
	if parent == "" {
		return "", errors.New("parent workspace root is required")
	}
	parentObject, err := current.GetObject(parent)
	if err != nil {
		return "", err
	}
	if parentObject.Kind != model.KindWorkspace || parentObject.MediaType != rootMediaType {
		return "", errors.New("parent is not a workspace root")
	}
	var parentRoot Root
	if err := canonical.Decode(parentObject.Payload, &parentRoot); err != nil {
		return "", err
	}
	if parentRoot.AdapterVersion != AdapterVersion {
		return "", fmt.Errorf("unsupported workspace adapter version %q", parentRoot.AdapterVersion)
	}
	payload, err := canonical.Marshal(Root{
		AdapterVersion: AdapterVersion,
		Parent:         parent,
	})
	if err != nil {
		return "", err
	}
	return current.PutObject(
		model.NewGraphObject(model.KindWorkspace, rootMediaType, payload, []string{parent}),
		strings.HasPrefix(parent, "priv:"),
	)
}

func Resolve(current *node.Node, root string) (map[string]Entry, error) {
	result := make(map[string]Entry)
	if err := resolve(current, root, result, make(map[string]bool)); err != nil {
		return nil, err
	}
	return result, nil
}

func Materialize(current *node.Node, root, destination string) error {
	entries, err := Resolve(current, root)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("workspace destination must not already exist")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ordered := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		ordered = append(ordered, entry)
	}
	sort.Slice(ordered, func(left, right int) bool {
		return ordered[left].Path < ordered[right].Path
	})
	if err := validateEntries(ordered); err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	rootFS, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer rootFS.Close()
	for _, entry := range ordered {
		if entry.Type != "directory" {
			continue
		}
		relative, err := safeRelative(entry.Path)
		if err != nil {
			return err
		}
		if err := mkdirAllRoot(rootFS, filepath.Dir(relative)); err != nil {
			return err
		}
		temporaryMode := os.FileMode(entry.Mode) | 0o700
		if err := rootFS.Mkdir(relative, temporaryMode); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	for _, entry := range ordered {
		if entry.Type == "directory" {
			continue
		}
		relative, err := safeRelative(entry.Path)
		if err != nil {
			return err
		}
		if err := mkdirAllRoot(rootFS, filepath.Dir(relative)); err != nil {
			return err
		}
		switch entry.Type {
		case "symlink":
			payload, err := readChunks(current, entry)
			if err != nil {
				return err
			}
			if err := os.Symlink(string(payload), filepath.Join(destination, relative)); err != nil {
				return err
			}
		case "file":
			if err := writeChunkedRootFile(current, rootFS, relative, entry); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported workspace entry type %q", entry.Type)
		}
	}
	directories := make([]Entry, 0)
	for _, entry := range ordered {
		if entry.Type == "directory" {
			directories = append(directories, entry)
		}
	}
	sort.Slice(directories, func(left, right int) bool {
		leftDepth := strings.Count(filepath.Clean(directories[left].Path), string(filepath.Separator))
		rightDepth := strings.Count(filepath.Clean(directories[right].Path), string(filepath.Separator))
		if leftDepth != rightDepth {
			return leftDepth > rightDepth
		}
		return directories[left].Path > directories[right].Path
	})
	for _, entry := range directories {
		relative, err := safeRelative(entry.Path)
		if err != nil {
			return err
		}
		if err := os.Chmod(filepath.Join(destination, relative), os.FileMode(entry.Mode)); err != nil {
			return err
		}
	}
	return nil
}

func resolve(current *node.Node, root string, result map[string]Entry, visiting map[string]bool) error {
	if visiting[root] {
		return errors.New("workspace parent cycle")
	}
	visiting[root] = true
	defer delete(visiting, root)
	object, err := current.GetObject(root)
	if err != nil {
		return err
	}
	if object.Kind != model.KindWorkspace || object.MediaType != rootMediaType {
		return errors.New("object is not a layered workspace root")
	}
	var workspaceRoot Root
	if err := canonical.Decode(object.Payload, &workspaceRoot); err != nil {
		return err
	}
	if workspaceRoot.AdapterVersion != AdapterVersion {
		return fmt.Errorf("unsupported workspace adapter version %q", workspaceRoot.AdapterVersion)
	}
	if workspaceRoot.Parent != "" {
		if !contains(object.Links, workspaceRoot.Parent) {
			return errors.New("workspace layer does not link its parent")
		}
		if err := resolve(current, workspaceRoot.Parent, result, visiting); err != nil {
			return err
		}
	}
	if workspaceRoot.Layer == "" {
		if len(object.Links) != 1 || workspaceRoot.Parent == "" {
			return errors.New("empty workspace fork has invalid links")
		}
		return nil
	}
	if !contains(object.Links, workspaceRoot.Layer) {
		return errors.New("workspace root does not link its layer")
	}
	layerObject, err := current.GetObject(workspaceRoot.Layer)
	if err != nil {
		return err
	}
	if layerObject.Kind != model.KindWorkspace || layerObject.MediaType != layerMediaType {
		return errors.New("workspace root links an invalid layer")
	}
	var layer Layer
	if err := canonical.Decode(layerObject.Payload, &layer); err != nil {
		return err
	}
	if err := validateLayer(layer, layerObject.Links); err != nil {
		return err
	}
	for _, path := range layer.Deleted {
		delete(result, path)
	}
	for _, entry := range layer.Entries {
		result[entry.Path] = entry
	}
	return nil
}

func validateLayer(layer Layer, links []string) error {
	if layer.AdapterVersion != AdapterVersion {
		return fmt.Errorf("unsupported workspace adapter version %q", layer.AdapterVersion)
	}
	if err := validateEntries(layer.Entries); err != nil {
		return err
	}
	for index, path := range layer.Deleted {
		if _, err := safeRelative(path); err != nil {
			return err
		}
		if index > 0 && layer.Deleted[index-1] >= path {
			return errors.New("deleted workspace paths must be sorted and unique")
		}
	}
	for _, entry := range layer.Entries {
		for _, chunk := range entry.Chunks {
			if !contains(links, chunk) {
				return fmt.Errorf("workspace entry %q references an unlinked chunk", entry.Path)
			}
		}
	}
	return nil
}

func validateEntries(entries []Entry) error {
	types := make(map[string]string, len(entries))
	for index, entry := range entries {
		relative, err := safeRelative(entry.Path)
		if err != nil {
			return err
		}
		if entry.Type != "file" && entry.Type != "symlink" && entry.Type != "directory" {
			return fmt.Errorf("unsupported workspace entry type %q", entry.Type)
		}
		if entry.Type == "directory" {
			if entry.Size != 0 || len(entry.Chunks) != 0 {
				return fmt.Errorf("workspace directory %q has content metadata", entry.Path)
			}
		} else if entry.Size < 0 || len(entry.Chunks) == 0 {
			return fmt.Errorf("workspace entry %q has invalid content metadata", entry.Path)
		}
		if index > 0 && entries[index-1].Path >= entry.Path {
			return errors.New("workspace entries must be sorted and unique")
		}
		types[relative] = entry.Type
	}
	for path, entryType := range types {
		if entryType != "symlink" {
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

func readChunks(current *node.Node, entry Entry) ([]byte, error) {
	var payload bytes.Buffer
	for _, id := range entry.Chunks {
		chunk, err := loadChunk(current, id)
		if err != nil {
			return nil, err
		}
		payload.Write(chunk)
	}
	if int64(payload.Len()) != entry.Size {
		return nil, fmt.Errorf("workspace entry %q size mismatch", entry.Path)
	}
	return payload.Bytes(), nil
}

func writeChunkedRootFile(current *node.Node, root *os.Root, path string, entry Entry) error {
	file, err := root.OpenFile(
		path,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		os.FileMode(entry.Mode),
	)
	if err != nil {
		return err
	}
	var written int64
	for _, id := range entry.Chunks {
		chunk, err := loadChunk(current, id)
		if err != nil {
			file.Close()
			return err
		}
		count, err := file.Write(chunk)
		written += int64(count)
		if err != nil {
			file.Close()
			return err
		}
		if count != len(chunk) {
			file.Close()
			return io.ErrShortWrite
		}
	}
	if written != entry.Size {
		file.Close()
		return fmt.Errorf("workspace entry %q size mismatch", entry.Path)
	}
	return file.Close()
}

func loadChunk(current *node.Node, id string) ([]byte, error) {
	object, err := current.GetObject(id)
	if err != nil {
		return nil, err
	}
	if object.Kind != model.KindWorkspace || object.MediaType != chunkMediaType || len(object.Links) != 0 {
		return nil, fmt.Errorf("workspace chunk %s has invalid shape", id)
	}
	return object.Payload, nil
}

func chunkFile(
	current *node.Node,
	path string,
	expected fs.FileInfo,
	private bool,
) ([]string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !sameFileState(expected, before) {
		return nil, 0, fmt.Errorf("workspace file %q changed before capture", path)
	}
	readBuffer := make([]byte, 64<<10)
	chunk := make([]byte, 0, averageChunk)
	var rolling uint64
	var window [64]byte
	windowIndex := 0
	windowCount := 0
	var chunks []string
	var total int64
	flush := func() error {
		id, err := putChunk(current, chunk, private)
		if err != nil {
			return err
		}
		chunks = append(chunks, id)
		chunk = make([]byte, 0, averageChunk)
		return nil
	}
	for {
		count, readErr := file.Read(readBuffer)
		total += int64(count)
		for _, value := range readBuffer[:count] {
			chunk = append(chunk, value)
			if windowCount < len(window) {
				rolling = bits.RotateLeft64(rolling, 1) ^ gear(value)
				windowCount++
			} else {
				rolling = bits.RotateLeft64(rolling, 1) ^
					gear(value) ^
					bits.RotateLeft64(gear(window[windowIndex]), len(window))
			}
			window[windowIndex] = value
			windowIndex = (windowIndex + 1) % len(window)
			if len(chunk) >= minChunkSize &&
				((rolling&(averageChunk-1)) == 0 || len(chunk) >= maxChunkSize) {
				if err := flush(); err != nil {
					return nil, 0, err
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, 0, readErr
		}
	}
	if len(chunk) > 0 || len(chunks) == 0 {
		if err := flush(); err != nil {
			return nil, 0, err
		}
	}
	after, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !sameFileState(before, after) || total != after.Size() {
		return nil, 0, fmt.Errorf("workspace file %q changed during capture", path)
	}
	return chunks, total, nil
}

func putChunk(current *node.Node, payload []byte, private bool) (string, error) {
	return current.PutObject(
		model.NewGraphObject(model.KindWorkspace, chunkMediaType, payload, nil),
		private,
	)
}

func gear(value byte) uint64 {
	return gearTable[value]
}

var gearTable = func() [256]uint64 {
	var table [256]uint64
	for index := range table {
		digest := sha256.Sum256([]byte{byte(index)})
		table[index] = binary.LittleEndian.Uint64(digest[:8])
	}
	return table
}()

func equalEntry(left, right Entry) bool {
	return left.Path == right.Path &&
		left.Mode == right.Mode &&
		left.Type == right.Type &&
		left.Size == right.Size &&
		strings.Join(left.Chunks, "\x00") == strings.Join(right.Chunks, "\x00")
}

func sameFileState(left, right fs.FileInfo) bool {
	return os.SameFile(left, right) &&
		left.Size() == right.Size() &&
		left.Mode() == right.Mode() &&
		left.ModTime().Equal(right.ModTime())
}

func countFiles(entries map[string]Entry) int {
	count := 0
	for _, entry := range entries {
		if entry.Type != "directory" {
			count++
		}
	}
	return count
}

func countEntryFiles(entries []Entry) int {
	count := 0
	for _, entry := range entries {
		if entry.Type != "directory" {
			count++
		}
	}
	return count
}

func countDeletedFiles(entries map[string]Entry, deleted []string) int {
	count := 0
	for _, path := range deleted {
		if entries[path].Type != "directory" {
			count++
		}
	}
	return count
}

func canonicalDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	canonicalPath, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonicalPath)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", path)
	}
	return canonicalPath, nil
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

func safeRelative(path string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe workspace path %q", path)
	}
	return clean, nil
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

func uniqueSorted(values []string) []string {
	sort.Strings(values)
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

func contains(values []string, want string) bool {
	index := sort.SearchStrings(values, want)
	return index < len(values) && values[index] == want
}
