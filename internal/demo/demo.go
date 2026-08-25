package demo

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nebuk89/cdn_git/internal/model"
	"github.com/nebuk89/cdn_git/internal/node"
	"github.com/nebuk89/cdn_git/internal/transport"
)

type Result struct {
	Directory        string `json:"directory"`
	BaseTransition   string `json:"base_transition"`
	FirstTransition  string `json:"first_transition"`
	SecondTransition string `json:"second_transition"`
	MergeTransition  string `json:"merge_transition"`
	DivergentRef     string `json:"divergent_ref"`
	FinalRef         string `json:"final_ref"`
}

func Run(ctx context.Context, directory string, output io.Writer) (Result, error) {
	if directory == "" {
		temporary, err := os.MkdirTemp("", "fabric-demo-*")
		if err != nil {
			return Result{}, err
		}
		directory = temporary
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Result{}, err
	}
	fmt.Fprintf(output, "State Fabric v0 three-node demo\n")
	fmt.Fprintf(output, "data: %s\n\n", directory)

	authority, err := node.Initialize(filepath.Join(directory, "authority"), true, nil)
	if err != nil {
		return Result{}, err
	}
	bundle := authority.Domain().EdgeBundle()
	edgeA, err := node.Initialize(filepath.Join(directory, "edge-a"), false, &bundle)
	if err != nil {
		return Result{}, err
	}
	edgeB, err := node.Initialize(filepath.Join(directory, "edge-b"), false, &bundle)
	if err != nil {
		return Result{}, err
	}
	capAuthority, err := authority.IssueCapability(authority.PublicKey(), "demo", []string{"transition.accept"}, time.Hour)
	if err != nil {
		return Result{}, err
	}
	capA, err := authority.IssueCapability(edgeA.PublicKey(), "demo", []string{"transition.accept"}, time.Hour)
	if err != nil {
		return Result{}, err
	}
	capB, err := authority.IssueCapability(edgeB.PublicKey(), "demo", []string{"transition.accept"}, time.Hour)
	if err != nil {
		return Result{}, err
	}

	base, _, err := authority.CreateTransition(node.TransitionRequest{
		Namespace:  "demo",
		Roots:      roots(authority, "base"),
		RefName:    "refs/heads/main",
		Capability: capAuthority,
	})
	if err != nil {
		return Result{}, err
	}
	if _, err := authority.Finalize(base.ID); err != nil {
		return Result{}, err
	}
	client := transport.NewClient(bundle.PeerToken)
	edgeAServer := httptest.NewServer(transport.NewServer(edgeA).Handler())
	edgeBServer := httptest.NewServer(transport.NewServer(edgeB).Handler())
	if err := client.SyncTransition(ctx, authority, edgeAServer.URL, base.ID); err != nil {
		edgeAServer.Close()
		edgeBServer.Close()
		return Result{}, err
	}
	if err := client.SyncTransition(ctx, authority, edgeBServer.URL, base.ID); err != nil {
		edgeAServer.Close()
		edgeBServer.Close()
		return Result{}, err
	}
	authorityBootstrapServer := httptest.NewServer(transport.NewServer(authority).Handler())
	if err := client.PullAuthority(ctx, edgeA, authorityBootstrapServer.URL); err != nil {
		authorityBootstrapServer.Close()
		edgeAServer.Close()
		edgeBServer.Close()
		return Result{}, err
	}
	if err := client.PullAuthority(ctx, edgeB, authorityBootstrapServer.URL); err != nil {
		authorityBootstrapServer.Close()
		edgeAServer.Close()
		edgeBServer.Close()
		return Result{}, err
	}
	authorityBootstrapServer.Close()
	edgeAServer.Close()
	edgeBServer.Close()
	fmt.Fprintf(output, "1. Authority finalized base state\n   %s\n\n", base.ID)

	first, receiptA, err := edgeA.CreateTransition(node.TransitionRequest{
		Namespace:         "demo",
		Roots:             roots(edgeA, "agent-a"),
		ParentTransitions: []string{base.ID},
		RefName:           "refs/heads/main",
		Expected:          base.ID,
		Capability:        capA,
	})
	if err != nil {
		return Result{}, err
	}
	second, receiptB, err := edgeB.CreateTransition(node.TransitionRequest{
		Namespace:         "demo",
		Roots:             roots(edgeB, "agent-b"),
		ParentTransitions: []string{base.ID},
		RefName:           "refs/heads/main",
		Expected:          base.ID,
		Capability:        capB,
	})
	if err != nil {
		return Result{}, err
	}
	fmt.Fprintf(output, "2. Authority offline: two edges durably accepted conflicting work\n")
	fmt.Fprintf(output, "   A receipt: %s\n", receiptA.ID)
	fmt.Fprintf(output, "   B receipt: %s\n\n", receiptB.ID)

	edgeA, err = node.Open(edgeA.Root())
	if err != nil {
		return Result{}, err
	}
	if _, err := edgeA.LoadTransition(first.ID); err != nil {
		return Result{}, fmt.Errorf("edge A restart lost accepted transition: %w", err)
	}
	fmt.Fprintf(output, "3. Edge A restarted; its acknowledged transition survived\n\n")

	authorityServer := httptest.NewServer(transport.NewServer(authority).Handler())
	defer authorityServer.Close()
	if err := client.SyncTransition(ctx, edgeA, authorityServer.URL, first.ID); err != nil {
		return Result{}, err
	}
	if err := client.SyncTransition(ctx, edgeB, authorityServer.URL, second.ID); err != nil {
		return Result{}, err
	}
	firstResult, err := client.Finalize(ctx, authorityServer.URL, first.ID)
	if err != nil {
		return Result{}, err
	}
	secondResult, err := client.Finalize(ctx, authorityServer.URL, second.ID)
	if err != nil {
		return Result{}, err
	}
	if firstResult.Status != "finalized" || secondResult.Status != "divergent" {
		return Result{}, fmt.Errorf("unexpected conflict outcomes: %s and %s", firstResult.Status, secondResult.Status)
	}
	fmt.Fprintf(output, "4. Connectivity restored; authority preserved both histories\n")
	fmt.Fprintf(output, "   shared:    %s\n", first.ID)
	fmt.Fprintf(output, "   divergent: %s -> %s\n\n", secondResult.DivergentRef, second.ID)

	edgeAServer = httptest.NewServer(transport.NewServer(edgeA).Handler())
	if err := client.SyncTransition(ctx, authority, edgeAServer.URL, second.ID); err != nil {
		edgeAServer.Close()
		return Result{}, err
	}
	edgeAServer.Close()
	mergeParents := []string{first.ID, second.ID}
	sort.Strings(mergeParents)
	merge, _, err := edgeA.CreateTransition(node.TransitionRequest{
		Namespace:         "demo",
		Roots:             roots(edgeA, "merged"),
		ParentTransitions: mergeParents,
		RefName:           "refs/heads/main",
		Expected:          first.ID,
		Capability:        capA,
		PolicyContext:     "explicit-demo-merge",
	})
	if err != nil {
		return Result{}, err
	}
	if err := client.SyncTransition(ctx, edgeA, authorityServer.URL, merge.ID); err != nil {
		return Result{}, err
	}
	mergeResult, err := client.Finalize(ctx, authorityServer.URL, merge.ID)
	if err != nil {
		return Result{}, err
	}
	if mergeResult.Status != "finalized" {
		return Result{}, fmt.Errorf("merge did not finalize: %s", mergeResult.Status)
	}
	if err := client.PullAuthority(ctx, edgeA, authorityServer.URL); err != nil {
		return Result{}, err
	}
	if err := client.PullAuthority(ctx, edgeB, authorityServer.URL); err != nil {
		return Result{}, err
	}
	refsAuthority, _, err := authority.Refs()
	if err != nil {
		return Result{}, err
	}
	refsA, divergentA, err := edgeA.Refs()
	if err != nil {
		return Result{}, err
	}
	refsB, divergentB, err := edgeB.Refs()
	if err != nil {
		return Result{}, err
	}
	refKey := "demo:refs/heads/main"
	if refsAuthority[refKey] != merge.ID || refsA[refKey] != merge.ID || refsB[refKey] != merge.ID {
		return Result{}, fmt.Errorf("nodes did not converge on merge transition")
	}
	if divergentA[secondResult.DivergentRef] != second.ID || divergentB[secondResult.DivergentRef] != second.ID {
		return Result{}, fmt.Errorf("divergent history disappeared after merge")
	}
	fmt.Fprintf(output, "5. Merge transition references both parents; all nodes converged\n")
	fmt.Fprintf(output, "   final: %s\n\n", merge.ID)
	fmt.Fprintf(output, "PASS: nearby durability, restart recovery, replication, conflict preservation, and convergence are real.\n")

	return Result{
		Directory:        directory,
		BaseTransition:   base.ID,
		FirstTransition:  first.ID,
		SecondTransition: second.ID,
		MergeTransition:  merge.ID,
		DivergentRef:     secondResult.DivergentRef,
		FinalRef:         merge.ID,
	}, nil
}

func roots(current *node.Node, prefix string) model.Roots {
	put := func(kind model.GraphKind, suffix string, private bool) string {
		id, err := current.PutObject(
			model.NewGraphObject(kind, "text/plain", []byte(prefix+"-"+suffix), nil),
			private,
		)
		if err != nil {
			panic(err)
		}
		return id
	}
	return model.Roots{
		Source:     put(model.KindSource, "source", false),
		Workspace:  put(model.KindWorkspace, "workspace", true),
		Provenance: put(model.KindProvenance, "provenance", true),
	}
}
