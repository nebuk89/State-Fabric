package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nebuk89/cdn_git/internal/gitadapter"
	"github.com/nebuk89/cdn_git/internal/model"
	"github.com/nebuk89/cdn_git/internal/node"
	"github.com/nebuk89/cdn_git/internal/security"
)

type gitRemoteConfig struct {
	Data             string
	Namespace        string
	Capability       string
	Peer             string
	CA               string
	PrivateSource    bool
	PrivateWorkspace bool
}

type gitRemoteRef struct {
	Name         string
	OID          string
	TransitionID string
	SourceRoot   string
	Workspace    string
}

type gitPushResult struct {
	Ref          string            `json:"ref"`
	CommitOID    string            `json:"commit_oid"`
	Transition   model.Transition  `json:"transition"`
	Receipt      model.EdgeReceipt `json:"receipt"`
	Finalize     string            `json:"finalize_status,omitempty"`
	DivergentRef string            `json:"divergent_ref,omitempty"`
	Warning      string            `json:"warning,omitempty"`
}

func runGitRemote(arguments []string, input io.Reader, output io.Writer) error {
	if len(arguments) < 1 || len(arguments) > 2 {
		return errors.New("usage: git-remote-fabric <name> [<url>]")
	}
	rawURL := arguments[0]
	if len(arguments) == 2 {
		rawURL = arguments[1]
	}
	config, err := parseGitRemoteURL(rawURL)
	if err != nil {
		return err
	}
	current, err := node.Open(config.Data)
	if err != nil {
		return err
	}
	gitDirectory := os.Getenv("GIT_DIR")
	if gitDirectory == "" {
		return errors.New("Git did not provide GIT_DIR to the remote helper")
	}
	if absolute, err := filepath.Abs(gitDirectory); err == nil {
		gitDirectory = absolute
	}

	scanner := bufio.NewScanner(input)
	writer := bufio.NewWriter(output)
	defer writer.Flush()
	advertisedByOID := make(map[string]gitRemoteRef)
	var advertisedForPush map[string]gitRemoteRef
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "capabilities":
			fmt.Fprintln(writer, "fetch")
			fmt.Fprintln(writer, "push")
			fmt.Fprintln(writer)
			if err := writer.Flush(); err != nil {
				return err
			}
		case line == "list" || line == "list for-push":
			if err := refreshGitRemote(context.Background(), current, config); err != nil {
				return err
			}
			refs, err := loadGitRemoteRefs(current, config.Namespace)
			if err != nil {
				return err
			}
			if head := defaultGitRemoteHead(refs); head != "" {
				fmt.Fprintf(writer, "@%s HEAD\n", head)
			}
			names := make([]string, 0, len(refs))
			for name := range refs {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				fmt.Fprintf(writer, "%s %s\n", refs[name].OID, name)
				advertisedByOID[refs[name].OID] = refs[name]
			}
			if line == "list for-push" {
				advertisedForPush = cloneGitRemoteRefs(refs)
			}
			fmt.Fprintln(writer)
			if err := writer.Flush(); err != nil {
				return err
			}
		case strings.HasPrefix(line, "fetch "):
			commands, err := readGitRemoteBatch(scanner, line, "fetch ")
			if err != nil {
				return err
			}
			for _, command := range commands {
				fields := strings.Fields(command)
				if len(fields) != 3 {
					return fmt.Errorf("invalid remote-helper fetch command %q", command)
				}
				ref, ok := advertisedByOID[fields[1]]
				if !ok {
					return fmt.Errorf("Fabric ref for Git object %s is unavailable", fields[1])
				}
				if err := gitadapter.ImportGitBundle(current, ref.SourceRoot, gitDirectory); err != nil {
					return fmt.Errorf("fetch %s: %w", fields[2], err)
				}
			}
			fmt.Fprintln(writer)
			if err := writer.Flush(); err != nil {
				return err
			}
		case strings.HasPrefix(line, "push "):
			commands, err := readGitRemoteBatch(scanner, line, "push ")
			if err != nil {
				return err
			}
			if advertisedForPush == nil {
				if err := refreshGitRemote(context.Background(), current, config); err != nil {
					return err
				}
				refs, err := loadGitRemoteRefs(current, config.Namespace)
				if err != nil {
					return err
				}
				advertisedForPush = cloneGitRemoteRefs(refs)
			}
			for _, command := range commands {
				spec := strings.TrimPrefix(command, "push ")
				destination, err := pushDestination(spec)
				if err != nil {
					fmt.Fprintf(writer, "error %s %s\n", destination, quoteGitRemoteError(err))
					continue
				}
				result, err := publishGitPush(
					context.Background(),
					current,
					config,
					spec,
					advertisedForPush,
					true,
				)
				if err != nil {
					fmt.Fprintf(writer, "error %s %s\n", destination, quoteGitRemoteError(err))
					continue
				}
				fmt.Fprintf(writer, "ok %s\n", result.Ref)
				if result.Warning != "" {
					fmt.Fprintf(os.Stderr, "warning: %s\n", result.Warning)
				}
			}
			fmt.Fprintln(writer)
			if err := writer.Flush(); err != nil {
				return err
			}
		case line == "":
			return nil
		default:
			return fmt.Errorf("unsupported Git remote-helper command %q", line)
		}
	}
	return scanner.Err()
}

func runGitFabric(arguments []string) error {
	if len(arguments) == 0 {
		printGitFabricUsage(os.Stderr)
		return errors.New("a command is required")
	}
	switch arguments[0] {
	case "checkpoint":
		return runGitFabricCheckpoint(arguments[1:])
	case "status":
		return runGitFabricStatus(arguments[1:])
	case "help", "-h", "--help":
		printGitFabricUsage(os.Stdout)
		return nil
	default:
		printGitFabricUsage(os.Stderr)
		return fmt.Errorf("unknown git fabric command %q", arguments[0])
	}
}

func runGitFabricCheckpoint(arguments []string) error {
	flags := flag.NewFlagSet("git fabric checkpoint", flag.ContinueOnError)
	remote := flags.String("remote", "origin", "Fabric Git remote")
	publish := flags.Bool("publish", false, "finalize the current branch at the authority")
	provenance := flags.String("provenance", `{"source":"git-fabric"}`, "provenance JSON or text")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	config, err := gitRemoteConfigFromGit(*remote)
	if err != nil {
		return err
	}
	current, err := node.Open(config.Data)
	if err != nil {
		return err
	}
	if err := refreshGitRemote(context.Background(), current, config); err != nil {
		return err
	}
	refs, err := loadGitRemoteRefs(current, config.Namespace)
	if err != nil {
		return err
	}
	ref, err := gitOutputNative("symbolic-ref", "-q", "HEAD")
	if err != nil {
		return errors.New("git fabric checkpoint requires an attached branch")
	}
	commit, err := gitOutputNative("rev-parse", "HEAD^{commit}")
	if err != nil {
		return err
	}
	result, err := checkpointGitCommit(
		context.Background(),
		current,
		config,
		commit,
		ref,
		false,
		[]byte(*provenance),
		refs[ref],
		*publish,
	)
	if err != nil {
		return err
	}
	return writeJSON(result)
}

func runGitFabricStatus(arguments []string) error {
	flags := flag.NewFlagSet("git fabric status", flag.ContinueOnError)
	remote := flags.String("remote", "origin", "Fabric Git remote")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	config, err := gitRemoteConfigFromGit(*remote)
	if err != nil {
		return err
	}
	current, err := node.Open(config.Data)
	if err != nil {
		return err
	}
	if err := refreshGitRemote(context.Background(), current, config); err != nil {
		return err
	}
	branch, err := gitOutputNative("symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		branch = "(detached)"
	}
	refs, err := loadGitRemoteRefs(current, config.Namespace)
	if err != nil {
		return err
	}
	refName := "refs/heads/" + branch
	fmt.Printf("On branch %s\n", branch)
	fmt.Printf("Fabric namespace %s\n", config.Namespace)
	if remoteRef, ok := refs[refName]; ok {
		fmt.Printf("Authority ref %s at %s\n", refName, remoteRef.OID)
		fmt.Printf("Transition %s\n", remoteRef.TransitionID)
	} else {
		fmt.Printf("Authority ref %s is unpublished\n", refName)
	}
	fmt.Printf("Node %s\n", current.NodeID())
	return nil
}

func publishGitPush(
	ctx context.Context,
	current *node.Node,
	config gitRemoteConfig,
	spec string,
	advertised map[string]gitRemoteRef,
	finalize bool,
) (gitPushResult, error) {
	force := strings.HasPrefix(spec, "+")
	if force {
		spec = strings.TrimPrefix(spec, "+")
	}
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return gitPushResult{}, errors.New("push requires <source>:<destination>")
	}
	if parts[0] == "" {
		return gitPushResult{}, errors.New("deleting Fabric refs through Git is not supported")
	}
	if !strings.HasPrefix(parts[1], "refs/heads/") {
		return gitPushResult{}, errors.New("git-remote-fabric v0.1 only supports branch destinations under refs/heads/")
	}
	commit, err := gitOutputNative("rev-parse", parts[0])
	if err != nil {
		return gitPushResult{}, err
	}
	objectType, err := gitOutputNative("cat-file", "-t", commit)
	if err != nil {
		return gitPushResult{}, err
	}
	if objectType != "commit" {
		return gitPushResult{}, fmt.Errorf("Fabric branch source %s resolves to a %s object, not a commit", parts[0], objectType)
	}
	return checkpointGitCommit(
		ctx,
		current,
		config,
		commit,
		parts[1],
		force,
		[]byte(`{"source":"git-push"}`),
		advertised[parts[1]],
		finalize,
	)
}

func checkpointGitCommit(
	ctx context.Context,
	current *node.Node,
	config gitRemoteConfig,
	commit string,
	refName string,
	force bool,
	provenance []byte,
	remote gitRemoteRef,
	finalize bool,
) (gitPushResult, error) {
	repository, err := gitOutputNative("rev-parse", "--show-toplevel")
	if err != nil {
		return gitPushResult{}, errors.New("Fabric Git publication requires a non-bare working tree")
	}
	if remote.OID != "" && !force {
		if err := runGitNative("merge-base", "--is-ancestor", remote.OID, commit); err != nil {
			return gitPushResult{}, errors.New("non-fast-forward Fabric push; fetch and merge or force explicitly")
		}
	}
	snapshot, err := gitadapter.SnapshotRepositoryWithWorkspaceParent(
		current,
		repository,
		commit,
		provenance,
		config.PrivateSource,
		config.PrivateWorkspace,
		remote.Workspace,
	)
	if err != nil {
		return gitPushResult{}, err
	}
	capability, err := gitTransitionCapability(current, config)
	if err != nil {
		return gitPushResult{}, err
	}
	parents := []string(nil)
	if remote.TransitionID != "" {
		parents = []string{remote.TransitionID}
	}
	transition, receipt, err := current.CreateTransition(node.TransitionRequest{
		Namespace:         config.Namespace,
		Roots:             snapshot.Roots,
		ParentTransitions: parents,
		RefName:           refName,
		Expected:          remote.TransitionID,
		Capability:        capability,
		PolicyContext:     "git-native",
	})
	if err != nil {
		return gitPushResult{}, err
	}
	result := gitPushResult{
		Ref:        refName,
		CommitOID:  commit,
		Transition: transition,
		Receipt:    receipt,
	}
	if !finalize {
		return result, nil
	}
	finalized, warning, err := finalizeGitTransition(ctx, current, config, transition.ID)
	if err != nil {
		return gitPushResult{}, err
	}
	result.Finalize = finalized.Status
	result.DivergentRef = finalized.DivergentRef
	result.Warning = warning
	if finalized.Status != "finalized" {
		return result, fmt.Errorf("authority preserved push as divergent ref %s", finalized.DivergentRef)
	}
	return result, nil
}

func finalizeGitTransition(
	ctx context.Context,
	current *node.Node,
	config gitRemoteConfig,
	transitionID string,
) (model.FinalizeResult, string, error) {
	if current.IsAuthority() {
		result, err := current.Finalize(transitionID)
		return result, "", err
	}

	if config.Peer == "" {
		return model.FinalizeResult{}, "", errors.New("edge Fabric remote requires a peer URL to finalize git push")
	}
	client, err := peerClient(current.Domain().PeerToken, config.CA)
	if err != nil {
		return model.FinalizeResult{}, "", err
	}
	peer := strings.TrimRight(config.Peer, "/")
	if err := client.SyncTransition(ctx, current, peer, transitionID); err != nil {
		return model.FinalizeResult{}, "", err
	}
	result, err := client.Finalize(ctx, peer, transitionID)
	if err != nil {
		return model.FinalizeResult{}, "", err
	}
	if err := client.PullAuthority(ctx, current, peer); err != nil {
		return result, "authority finalized the push, but refreshing the local cache failed: " + err.Error(), nil
	}
	return result, "", nil
}

func refreshGitRemote(ctx context.Context, current *node.Node, config gitRemoteConfig) error {
	if config.Peer == "" {
		return nil
	}
	client, err := peerClient(current.Domain().PeerToken, config.CA)
	if err != nil {
		return err
	}
	peer := strings.TrimRight(config.Peer, "/")
	return client.PullAuthority(ctx, current, peer)
}

func cloneGitRemoteRefs(refs map[string]gitRemoteRef) map[string]gitRemoteRef {
	result := make(map[string]gitRemoteRef, len(refs))
	for name, ref := range refs {
		result[name] = ref
	}
	return result
}

func gitTransitionCapability(current *node.Node, config gitRemoteConfig) (model.Capability, error) {
	if config.Capability != "" {
		var capability model.Capability
		if err := security.Load(config.Capability, &capability); err != nil {
			return model.Capability{}, err
		}

		return capability, nil
	}
	if current.IsAuthority() {
		return current.IssueCapability(
			current.PublicKey(),
			config.Namespace,
			[]string{"transition.accept"},
			time.Hour,
		)
	}
	return model.Capability{}, errors.New("edge Fabric remote URL requires a capability query parameter")
}

func loadGitRemoteRefs(current *node.Node, namespace string) (map[string]gitRemoteRef, error) {
	refs, _, err := current.Refs()
	if err != nil {
		return nil, err
	}
	result := make(map[string]gitRemoteRef)
	prefix := namespace + ":"
	for key, transitionID := range refs {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		name := strings.TrimPrefix(key, prefix)
		transition, err := current.LoadTransition(transitionID)
		if err != nil {
			return nil, err
		}
		snapshot, err := gitadapter.LoadSourceSnapshot(current, transition.Body.Roots.Source)
		if err != nil {
			return nil, err
		}
		if snapshot.ObjectFormat != "sha1" {
			return nil, errors.New("git-remote-fabric currently supports SHA-1 Git repositories")
		}
		result[name] = gitRemoteRef{
			Name:         name,
			OID:          snapshot.CommitOID,
			TransitionID: transition.ID,
			SourceRoot:   transition.Body.Roots.Source,
			Workspace:    transition.Body.Roots.Workspace,
		}
	}
	return result, nil
}

func parseGitRemoteURL(raw string) (gitRemoteConfig, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return gitRemoteConfig{}, err
	}
	if parsed.Scheme != "fabric" {
		return gitRemoteConfig{}, fmt.Errorf("unsupported Fabric Git URL %q", raw)
	}
	if parsed.Host != "" && parsed.Host != "localhost" {
		return gitRemoteConfig{}, errors.New("v0.1 git-remote-fabric supports local node paths only")
	}
	data := filepath.FromSlash(parsed.Path)
	if data == "" {
		return gitRemoteConfig{}, errors.New("Fabric Git URL requires an absolute node data path")
	}
	if !filepath.IsAbs(data) {
		return gitRemoteConfig{}, errors.New("Fabric Git node data path must be absolute")
	}
	namespace := parsed.Query().Get("namespace")
	if namespace == "" {
		return gitRemoteConfig{}, errors.New("Fabric Git URL requires ?namespace=<name>")
	}
	privateSource, err := queryBool(parsed, "private-source", true)
	if err != nil {
		return gitRemoteConfig{}, err
	}
	privateWorkspace, err := queryBool(parsed, "private-workspace", true)
	if err != nil {
		return gitRemoteConfig{}, err
	}
	return gitRemoteConfig{
		Data:             data,
		Namespace:        namespace,
		Capability:       parsed.Query().Get("capability"),
		Peer:             parsed.Query().Get("peer"),
		CA:               parsed.Query().Get("ca"),
		PrivateSource:    privateSource,
		PrivateWorkspace: privateWorkspace,
	}, nil
}

func gitRemoteConfigFromGit(remote string) (gitRemoteConfig, error) {
	raw, err := gitOutputNative("config", "--get", "remote."+remote+".url")
	if err != nil {
		return gitRemoteConfig{}, fmt.Errorf("Git remote %q is not configured", remote)
	}
	return parseGitRemoteURL(raw)
}

func queryBool(parsed *url.URL, name string, fallback bool) (bool, error) {
	value := parsed.Query().Get(name)
	if value == "" {
		return fallback, nil
	}
	result, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s query value %q", name, value)
	}
	return result, nil
}

func readGitRemoteBatch(scanner *bufio.Scanner, first, prefix string) ([]string, error) {
	commands := []string{first}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			return commands, nil
		}
		if !strings.HasPrefix(line, prefix) {
			return nil, fmt.Errorf("unexpected remote-helper batch command %q", line)
		}
		commands = append(commands, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("unterminated Git remote-helper command batch")
}

func defaultGitRemoteHead(refs map[string]gitRemoteRef) string {
	if _, ok := refs["refs/heads/main"]; ok {
		return "refs/heads/main"
	}
	names := make([]string, 0, len(refs))
	for name := range refs {
		if strings.HasPrefix(name, "refs/heads/") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func pushDestination(spec string) (string, error) {
	spec = strings.TrimPrefix(spec, "+")
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "refs/heads/unknown", errors.New("invalid push refspec")
	}
	return parts[1], nil
}

func quoteGitRemoteError(err error) string {
	encoded, marshalErr := json.Marshal(err.Error())
	if marshalErr != nil {
		return `"Fabric push failed"`
	}
	return string(encoded)
}

func gitOutputNative(arguments ...string) (string, error) {
	command := exec.Command("git", arguments...)
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return "", fmt.Errorf("git %s: %s", strings.Join(arguments, " "), strings.TrimSpace(string(exitError.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func runGitNative(arguments ...string) error {
	command := exec.Command("git", arguments...)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %s", strings.Join(arguments, " "), strings.TrimSpace(string(output)))
	}
	return nil
}

func printGitFabricUsage(output io.Writer) {
	fmt.Fprintln(output, `Git-native State Fabric commands

Usage:
  git fabric checkpoint [--remote origin] [--publish]
  git fabric status [--remote origin]

Normal source operations remain ordinary Git:
  git clone fabric:///absolute/node/path?namespace=demo
  git fetch
  git push`)
}
