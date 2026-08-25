package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/nebuk89/cdn_git/internal/demo"
	"github.com/nebuk89/cdn_git/internal/gitadapter"
	"github.com/nebuk89/cdn_git/internal/model"
	"github.com/nebuk89/cdn_git/internal/node"
	"github.com/nebuk89/cdn_git/internal/security"
	"github.com/nebuk89/cdn_git/internal/transport"
	"github.com/nebuk89/cdn_git/internal/workspace"
)

const version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fabric:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		printUsage(os.Stderr)
		return errors.New("a command is required")
	}
	switch arguments[0] {
	case "version":
		fmt.Println(version)
		return nil
	case "init":
		return runInit(arguments[1:])
	case "status":
		return runStatus(arguments[1:])
	case "domain-export":
		return runDomainExport(arguments[1:])
	case "capability-issue":
		return runCapabilityIssue(arguments[1:])
	case "put":
		return runPut(arguments[1:])
	case "git-snapshot":
		return runGitSnapshot(arguments[1:])
	case "export-source":
		return runExportSource(arguments[1:])
	case "workspace-capture":
		return runWorkspaceCapture(arguments[1:])
	case "workspace-fork":
		return runWorkspaceFork(arguments[1:])
	case "export-workspace":
		return runExportWorkspace(arguments[1:])
	case "transition":
		return runTransition(arguments[1:])
	case "finalize":
		return runFinalize(arguments[1:])
	case "sync":
		return runSync(arguments[1:])
	case "accept":
		return runAccept(arguments[1:])
	case "pull-authority":
		return runPullAuthority(arguments[1:])
	case "refs":
		return runRefs(arguments[1:])
	case "stats":
		return runStats(arguments[1:])
	case "verify":
		return runVerify(arguments[1:])
	case "gc":
		return runGC(arguments[1:])
	case "serve":
		return runServe(arguments[1:])
	case "demo":
		return runDemo(arguments[1:])
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return nil
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown command %q", arguments[0])
	}
}

func runInit(arguments []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "node data directory")
	authority := flags.Bool("authority", false, "initialize the namespace authority")
	domainPath := flags.String("domain", "", "edge domain bundle exported by an authority")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	var bundle *security.DomainConfig
	if *domainPath != "" {
		var loaded security.DomainConfig
		if err := security.Load(*domainPath, &loaded); err != nil {
			return err
		}
		bundle = &loaded
	}
	current, err := node.Initialize(*data, *authority, bundle)
	if err != nil {
		return err
	}
	return writeJSON(struct {
		Data        string `json:"data"`
		NodeID      string `json:"node_id"`
		PublicKey   string `json:"public_key"`
		TrustDomain string `json:"trust_domain"`
		Authority   bool   `json:"authority"`
	}{
		Data:        current.Root(),
		NodeID:      current.NodeID(),
		PublicKey:   current.PublicKey(),
		TrustDomain: current.Domain().ID,
		Authority:   current.IsAuthority(),
	})
}

func runStatus(arguments []string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "node data directory")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	refs, divergent, err := current.Refs()
	if err != nil {
		return err
	}
	return writeJSON(struct {
		NodeID        string            `json:"node_id"`
		PublicKey     string            `json:"public_key"`
		TrustDomain   string            `json:"trust_domain"`
		Authority     bool              `json:"authority"`
		Refs          map[string]string `json:"refs"`
		DivergentRefs map[string]string `json:"divergent_refs"`
	}{
		NodeID:        current.NodeID(),
		PublicKey:     current.PublicKey(),
		TrustDomain:   current.Domain().ID,
		Authority:     current.IsAuthority(),
		Refs:          refs,
		DivergentRefs: divergent,
	})
}

func runDomainExport(arguments []string) error {
	flags := flag.NewFlagSet("domain-export", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "authority data directory")
	output := flags.String("out", "domain.bundle.json", "sensitive edge domain bundle")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	if !current.IsAuthority() {
		return errors.New("domain bundles can only be exported from an authority node")
	}
	if err := security.Save(*output, current.Domain().EdgeBundle(), 0o600); err != nil {
		return err
	}
	fmt.Printf("wrote sensitive domain bundle to %s\n", *output)
	return nil
}

func runCapabilityIssue(arguments []string) error {
	flags := flag.NewFlagSet("capability-issue", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "authority data directory")
	subject := flags.String("subject", "", "subject Ed25519 public key")
	namespace := flags.String("namespace", "", "namespace")
	operations := flags.String("operations", "transition.accept", "comma-separated operations")
	ttl := flags.Duration("ttl", 24*time.Hour, "capability lifetime")
	output := flags.String("out", "capability.json", "capability output")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *subject == "" || *namespace == "" {
		return errors.New("--subject and --namespace are required")
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	capability, err := current.IssueCapability(*subject, *namespace, splitCSV(*operations), *ttl)
	if err != nil {
		return err
	}
	if err := security.Save(*output, capability, 0o600); err != nil {
		return err
	}
	fmt.Printf("wrote %s to %s\n", capability.ID, *output)
	return nil
}

func runPut(arguments []string) error {
	flags := flag.NewFlagSet("put", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "node data directory")
	kindValue := flags.String("kind", "", "source, workspace, provenance, or manifest")
	input := flags.String("file", "-", "payload file or - for stdin")
	mediaType := flags.String("media-type", "application/octet-stream", "payload media type")
	links := flags.String("links", "", "comma-separated child object IDs")
	private := flags.Bool("private", false, "store in the private trust domain")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	kind, err := parseKind(*kindValue)
	if err != nil {
		return err
	}
	payload, err := readInput(*input)
	if err != nil {
		return err
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	id, err := current.PutObject(model.NewGraphObject(kind, *mediaType, payload, splitCSV(*links)), *private)
	if err != nil {
		return err
	}
	fmt.Println(id)
	return nil
}

func runGitSnapshot(arguments []string) error {
	flags := flag.NewFlagSet("git-snapshot", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "node data directory")
	repository := flags.String("repo", ".", "Git working tree")
	ref := flags.String("ref", "HEAD", "Git commit or ref")
	provenancePath := flags.String("provenance", "-", "provenance JSON/text file or - for stdin")
	privateSource := flags.Bool("private-source", false, "store source graph privately")
	privateWorkspace := flags.Bool("private-workspace", true, "store workspace and provenance privately")
	workspaceParent := flags.String("workspace-parent", "", "optional parent Workspace Graph root")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	provenance, err := readInput(*provenancePath)
	if err != nil {
		return err
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	result, err := gitadapter.SnapshotRepositoryWithWorkspaceParent(
		current,
		*repository,
		*ref,
		provenance,
		*privateSource,
		*privateWorkspace,
		*workspaceParent,
	)
	if err != nil {
		return err
	}
	return writeJSON(result)
}

func runWorkspaceCapture(arguments []string) error {
	flags := flag.NewFlagSet("workspace-capture", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "node data directory")
	directory := flags.String("dir", ".", "workspace directory")
	parent := flags.String("parent", "", "optional parent Workspace Graph root")
	private := flags.Bool("private", true, "store workspace privately")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	result, err := workspace.Capture(current, *directory, *parent, *private)
	if err != nil {
		return err
	}
	return writeJSON(result)
}

func runWorkspaceFork(arguments []string) error {
	flags := flag.NewFlagSet("workspace-fork", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "node data directory")
	parent := flags.String("parent", "", "parent Workspace Graph root")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *parent == "" {
		return errors.New("--parent is required")
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	root, err := workspace.Fork(current, *parent)
	if err != nil {
		return err
	}
	return writeJSON(struct {
		Root string `json:"root"`
	}{Root: root})
}

func runExportWorkspace(arguments []string) error {
	flags := flag.NewFlagSet("export-workspace", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "node data directory")
	root := flags.String("workspace", "", "Workspace Graph root")
	output := flags.String("out", "", "destination directory")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *root == "" || *output == "" {
		return errors.New("--workspace and --out are required")
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	return workspace.Materialize(current, *root, *output)
}

func runExportSource(arguments []string) error {
	flags := flag.NewFlagSet("export-source", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "node data directory")
	source := flags.String("source", "", "Source Graph root")
	output := flags.String("out", "", "destination directory")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *source == "" || *output == "" {
		return errors.New("--source and --out are required")
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	return gitadapter.ExportTrackedFiles(current, *source, *output)
}

func runTransition(arguments []string) error {
	flags := flag.NewFlagSet("transition", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "node data directory")
	namespace := flags.String("namespace", "", "namespace")
	source := flags.String("source", "", "Source Graph root")
	workspace := flags.String("workspace", "", "Workspace Graph root")
	provenance := flags.String("provenance", "", "Provenance Graph root")
	parents := flags.String("parents", "", "comma-separated parent transition IDs")
	refName := flags.String("ref", "refs/heads/main", "target ref")
	expected := flags.String("expected", "", "expected finalized transition")
	capabilityPath := flags.String("capability", "", "signed capability file")
	policy := flags.String("policy", "", "opaque policy context")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *namespace == "" || *source == "" || *workspace == "" || *provenance == "" || *capabilityPath == "" {
		return errors.New("--namespace, --source, --workspace, --provenance, and --capability are required")
	}
	var capability model.Capability
	if err := security.Load(*capabilityPath, &capability); err != nil {
		return err
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	transition, receipt, err := current.CreateTransition(node.TransitionRequest{
		Namespace:         *namespace,
		Roots:             model.Roots{Source: *source, Workspace: *workspace, Provenance: *provenance},
		ParentTransitions: splitCSV(*parents),
		RefName:           *refName,
		Expected:          *expected,
		Capability:        capability,
		PolicyContext:     *policy,
	})
	if err != nil {
		return err
	}
	return writeJSON(struct {
		Transition model.Transition  `json:"transition"`
		Receipt    model.EdgeReceipt `json:"receipt"`
	}{Transition: transition, Receipt: receipt})
}

func runFinalize(arguments []string) error {
	flags := flag.NewFlagSet("finalize", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "authority data directory")
	transitionID := flags.String("transition", "", "transition ID")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *transitionID == "" {
		return errors.New("--transition is required")
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	result, err := current.Finalize(*transitionID)
	if err != nil {
		return err
	}
	return writeJSON(result)
}

func runSync(arguments []string) error {
	flags := flag.NewFlagSet("sync", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "source node data directory")
	peer := flags.String("peer", "", "peer base URL")
	transitionID := flags.String("transition", "", "transition ID")
	ca := flags.String("ca", "", "optional PEM CA certificate for the peer")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *peer == "" || *transitionID == "" {
		return errors.New("--peer and --transition are required")
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	client, err := peerClient(current.Domain().PeerToken, *ca)
	if err != nil {
		return err
	}
	return client.SyncTransition(context.Background(), current, strings.TrimRight(*peer, "/"), *transitionID)
}

func runAccept(arguments []string) error {
	flags := flag.NewFlagSet("accept", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "actor node data directory")
	peer := flags.String("peer", "", "accepting edge base URL")
	transitionID := flags.String("transition", "", "transition ID")
	capabilityPath := flags.String("capability", "", "accepting edge receipt.issue capability file")
	ca := flags.String("ca", "", "optional PEM CA certificate for the peer")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *peer == "" || *transitionID == "" || *capabilityPath == "" {
		return errors.New("--peer, --transition, and --capability are required")
	}
	var capability model.Capability
	if err := security.Load(*capabilityPath, &capability); err != nil {
		return err
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	client, err := peerClient(current.Domain().PeerToken, *ca)
	if err != nil {
		return err
	}
	receipt, err := client.AcceptTransition(
		context.Background(),
		current,
		strings.TrimRight(*peer, "/"),
		*transitionID,
		capability,
	)
	if err != nil {
		return err
	}
	return writeJSON(receipt)
}

func runPullAuthority(arguments []string) error {
	flags := flag.NewFlagSet("pull-authority", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "edge node data directory")
	peer := flags.String("peer", "", "authority base URL")
	ca := flags.String("ca", "", "optional PEM CA certificate for the peer")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *peer == "" {
		return errors.New("--peer is required")
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	client, err := peerClient(current.Domain().PeerToken, *ca)
	if err != nil {
		return err
	}
	return client.PullAuthority(context.Background(), current, strings.TrimRight(*peer, "/"))
}

func runRefs(arguments []string) error {
	flags := flag.NewFlagSet("refs", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "node data directory")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	refs, divergent, err := current.Refs()
	if err != nil {
		return err
	}
	return writeJSON(struct {
		Refs      map[string]string `json:"refs"`
		Divergent map[string]string `json:"divergent"`
	}{Refs: refs, Divergent: divergent})
}

func runStats(arguments []string) error {
	flags := flag.NewFlagSet("stats", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "node data directory")
	peer := flags.String("peer", "", "optional peer base URL")
	ca := flags.String("ca", "", "optional PEM CA certificate for the peer")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	if *peer == "" {
		stats, err := current.OperationalStats()
		if err != nil {
			return err
		}
		return writeJSON(stats)
	}
	client, err := peerClient(current.Domain().PeerToken, *ca)
	if err != nil {
		return err
	}
	stats, err := client.PeerStats(context.Background(), strings.TrimRight(*peer, "/"))
	if err != nil {
		return err
	}
	return writeJSON(stats)
}

func runVerify(arguments []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "node data directory")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	report, err := current.Audit()
	if err != nil {
		return err
	}
	return writeJSON(report)
}

func runGC(arguments []string) error {
	flags := flag.NewFlagSet("gc", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "node data directory")
	grace := flags.Duration("grace", 24*time.Hour, "minimum age for unreachable objects")
	apply := flags.Bool("apply", false, "delete candidates; default is dry-run")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	report, err := current.GarbageCollect(*grace, !*apply)
	if err != nil {
		return err
	}
	return writeJSON(report)
}

func runServe(arguments []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	data := flags.String("data", ".fabric", "node data directory")
	listen := flags.String("listen", "127.0.0.1:7337", "HTTP listen address")
	tlsCertificate := flags.String("tls-cert", "", "PEM TLS certificate")
	tlsKey := flags.String("tls-key", "", "PEM TLS private key")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	current, err := node.Open(*data)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              *listen,
		Handler:           transport.NewServer(current).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()
	if (*tlsCertificate == "") != (*tlsKey == "") {
		return errors.New("--tls-cert and --tls-key must be provided together")
	}
	scheme := "http"
	if *tlsCertificate != "" {
		scheme = "https"
	}
	fmt.Printf("fabric %s listening on %s://%s as %s\n", version, scheme, *listen, current.NodeID())
	if *tlsCertificate != "" {
		err = server.ListenAndServeTLS(*tlsCertificate, *tlsKey)
	} else {
		err = server.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func runDemo(arguments []string) error {
	flags := flag.NewFlagSet("demo", flag.ContinueOnError)
	directory := flags.String("dir", "", "persistent demo directory; defaults to a temporary directory")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	_, err := demo.Run(context.Background(), *directory, os.Stdout)
	return err
}

func parseKind(value string) (model.GraphKind, error) {
	switch model.GraphKind(value) {
	case model.KindSource, model.KindWorkspace, model.KindProvenance, model.KindManifest:
		return model.GraphKind(value), nil
	default:
		return "", fmt.Errorf("invalid graph kind %q", value)
	}
}

func readInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(filepath.Clean(path))
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{})
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	sort.Strings(result)
	return result
}

func peerClient(peerToken, caPath string) (*transport.Client, error) {
	if caPath == "" {
		return transport.NewClient(peerToken), nil
	}
	return transport.NewClientWithCA(peerToken, caPath)
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, `State Fabric v0 - agent-native distributed state

Usage:
  fabric init               initialize an authority or edge node
  fabric status             inspect node identity and refs
  fabric domain-export      export a sensitive edge domain bundle
  fabric capability-issue   issue a transition capability
  fabric put                store a graph object
  fabric git-snapshot       project Git source and complete workspace state
  fabric export-source      materialize tracked files from a Source Graph root
  fabric workspace-capture  capture a layered, chunked workspace
  fabric workspace-fork     create a constant-time metadata workspace fork
  fabric export-workspace   materialize a Workspace Graph root
  fabric transition         durably accept an authorized state transition
  fabric accept             obtain independent durability from an edge
  fabric finalize           finalize or preserve a divergent ref
  fabric sync               replicate a transition closure to a peer
  fabric pull-authority     mirror authority journal state
  fabric refs               display shared and divergent refs
  fabric stats              display local or remote operational counters
  fabric verify             verify stored objects, transitions, and receipts
  fabric gc                 report or delete unreachable aged objects
  fabric serve              run the peer HTTP daemon
  fabric demo               run the three-node end-to-end proof
  fabric version            print the version`)
}
