package transport

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/nebuk89/cdn_git/internal/model"
	"github.com/nebuk89/cdn_git/internal/node"
)

func TestThreeNodeReplicationConflictAndConvergence(t *testing.T) {
	baseDirectory := t.TempDir()
	authority, err := node.Initialize(filepath.Join(baseDirectory, "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	domainBundle := authority.Domain().EdgeBundle()
	edgeA, err := node.Initialize(filepath.Join(baseDirectory, "edge-a"), false, &domainBundle)
	if err != nil {
		t.Fatal(err)
	}
	edgeB, err := node.Initialize(filepath.Join(baseDirectory, "edge-b"), false, &domainBundle)
	if err != nil {
		t.Fatal(err)
	}
	capabilityA, err := authority.IssueCapability(edgeA.PublicKey(), "demo", []string{"transition.accept"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	capabilityB, err := authority.IssueCapability(edgeB.PublicKey(), "demo", []string{"transition.accept"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	capabilityAuthority, err := authority.IssueCapability(authority.PublicKey(), "demo", []string{"transition.accept"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	base, _, err := authority.CreateTransition(node.TransitionRequest{
		Namespace:  "demo",
		Roots:      transportRoots(t, authority, "base"),
		RefName:    "refs/heads/main",
		Capability: capabilityAuthority,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Finalize(base.ID); err != nil {
		t.Fatal(err)
	}
	edgeAServer := httptest.NewServer(NewServer(edgeA).Handler())
	defer edgeAServer.Close()
	edgeBServer := httptest.NewServer(NewServer(edgeB).Handler())
	defer edgeBServer.Close()
	client := NewClient(domainBundle.PeerToken)
	ctx := context.Background()
	if err := client.SyncTransition(ctx, authority, edgeAServer.URL, base.ID); err != nil {
		t.Fatal(err)
	}
	if err := client.SyncTransition(ctx, authority, edgeBServer.URL, base.ID); err != nil {
		t.Fatal(err)
	}
	if err := client.PullAuthority(ctx, edgeA, httptestAuthorityURL(t, authority)); err != nil {
		t.Fatal(err)
	}
	if err := client.PullAuthority(ctx, edgeB, httptestAuthorityURL(t, authority)); err != nil {
		t.Fatal(err)
	}

	first, _, err := edgeA.CreateTransition(node.TransitionRequest{
		Namespace:         "demo",
		Roots:             transportRoots(t, edgeA, "first"),
		ParentTransitions: []string{base.ID},
		RefName:           "refs/heads/main",
		Expected:          base.ID,
		Capability:        capabilityA,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := edgeB.CreateTransition(node.TransitionRequest{
		Namespace:         "demo",
		Roots:             transportRoots(t, edgeB, "second"),
		ParentTransitions: []string{base.ID},
		RefName:           "refs/heads/main",
		Expected:          base.ID,
		Capability:        capabilityB,
	})
	if err != nil {
		t.Fatal(err)
	}

	authorityServer := httptest.NewServer(NewServer(authority).Handler())
	defer authorityServer.Close()
	if err := client.SyncTransition(ctx, edgeA, authorityServer.URL, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := client.SyncTransition(ctx, edgeB, authorityServer.URL, second.ID); err != nil {
		t.Fatal(err)
	}
	firstResult, err := client.Finalize(ctx, authorityServer.URL, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := client.Finalize(ctx, authorityServer.URL, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstResult.Status != "finalized" || secondResult.Status != "divergent" {
		t.Fatalf("unexpected results: first=%s second=%s", firstResult.Status, secondResult.Status)
	}
	if err := client.PullAuthority(ctx, edgeA, authorityServer.URL); err != nil {
		t.Fatal(err)
	}
	if err := client.PullAuthority(ctx, edgeB, authorityServer.URL); err != nil {
		t.Fatal(err)
	}
	refsA, divergentA, err := edgeA.Refs()
	if err != nil {
		t.Fatal(err)
	}
	refsB, divergentB, err := edgeB.Refs()
	if err != nil {
		t.Fatal(err)
	}
	if refsA["demo:refs/heads/main"] != first.ID || refsB["demo:refs/heads/main"] != first.ID {
		t.Fatal("edges did not converge on finalized ref")
	}
	if divergentA[secondResult.DivergentRef] != second.ID || divergentB[secondResult.DivergentRef] != second.ID {
		t.Fatal("edges did not preserve divergent head")
	}
}

func httptestAuthorityURL(t *testing.T, authority *node.Node) string {
	t.Helper()
	server := httptest.NewServer(NewServer(authority).Handler())
	t.Cleanup(server.Close)
	return server.URL
}

func TestPeerAuthenticationRequired(t *testing.T) {
	authority, err := node.Initialize(filepath.Join(t.TempDir(), "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(authority).Handler())
	defer server.Close()

	client := NewClient("wrong-token")
	if _, _, err := client.PeerRefs(context.Background(), server.URL); err == nil {
		t.Fatal("unauthenticated peer unexpectedly read private authority state")
	}
}

func transportRoots(t *testing.T, current *node.Node, prefix string) model.Roots {
	t.Helper()
	put := func(kind model.GraphKind, suffix string, private bool) string {
		id, err := current.PutObject(
			model.NewGraphObject(kind, "text/plain", []byte(prefix+"-"+suffix), nil),
			private,
		)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	return model.Roots{
		Source:     put(model.KindSource, "source", false),
		Workspace:  put(model.KindWorkspace, "workspace", true),
		Provenance: put(model.KindProvenance, "provenance", true),
	}
}
