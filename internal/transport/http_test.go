package transport

import (
	"context"
	"encoding/pem"
	"net/http/httptest"
	"os"
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

func TestIndependentEdgeAcceptsActorTransition(t *testing.T) {
	baseDirectory := t.TempDir()
	authority, err := node.Initialize(filepath.Join(baseDirectory, "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	bundle := authority.Domain().EdgeBundle()
	actor, err := node.Initialize(filepath.Join(baseDirectory, "actor"), false, &bundle)
	if err != nil {
		t.Fatal(err)
	}
	acceptingEdge, err := node.Initialize(filepath.Join(baseDirectory, "accepting-edge"), false, &bundle)
	if err != nil {
		t.Fatal(err)
	}
	actorCapability, err := authority.IssueCapability(
		actor.PublicKey(),
		"independent",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	edgeCapability, err := authority.IssueCapability(
		acceptingEdge.PublicKey(),
		"independent",
		[]string{"receipt.issue"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	transition, _, err := actor.CreateTransition(node.TransitionRequest{
		Namespace:  "independent",
		Roots:      transportRoots(t, actor, "remote"),
		RefName:    "refs/heads/main",
		Capability: actorCapability,
	})
	if err != nil {
		t.Fatal(err)
	}
	edgeServer := httptest.NewServer(NewServer(acceptingEdge).Handler())
	defer edgeServer.Close()
	client := NewClient(bundle.PeerToken)
	receipt, err := client.AcceptTransition(
		context.Background(),
		actor,
		edgeServer.URL,
		transition.ID,
		edgeCapability,
	)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.NodePublicKey != acceptingEdge.PublicKey() ||
		receipt.NodePublicKey == transition.Body.ActorPublicKey {
		t.Fatal("receipt was not signed by an independent accepting edge")
	}
	authorityServer := httptest.NewServer(NewServer(authority).Handler())
	defer authorityServer.Close()
	if err := client.SyncTransition(context.Background(), acceptingEdge, authorityServer.URL, transition.ID); err != nil {
		t.Fatal(err)
	}
	result, err := client.Finalize(context.Background(), authorityServer.URL, transition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "finalized" {
		t.Fatalf("independently accepted transition was not finalized: %+v", result)
	}
}

func TestIndependentAcceptanceImportsNewerEdgeAuthorityJournal(t *testing.T) {
	baseDirectory := t.TempDir()
	authority, err := node.Initialize(filepath.Join(baseDirectory, "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	bundle := authority.Domain().EdgeBundle()
	actor, err := node.Initialize(filepath.Join(baseDirectory, "actor"), false, &bundle)
	if err != nil {
		t.Fatal(err)
	}
	acceptingEdge, err := node.Initialize(filepath.Join(baseDirectory, "accepting-edge"), false, &bundle)
	if err != nil {
		t.Fatal(err)
	}
	authorityCapability, err := authority.IssueCapability(
		authority.PublicKey(),
		"authority-progress",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	seed, _, err := authority.CreateTransition(node.TransitionRequest{
		Namespace:  "authority-progress",
		Roots:      transportRoots(t, authority, "seed"),
		RefName:    "refs/heads/main",
		Capability: authorityCapability,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Finalize(seed.ID); err != nil {
		t.Fatal(err)
	}
	records, err := authority.AuthorityRecords()
	if err != nil {
		t.Fatal(err)
	}
	if err := acceptingEdge.ImportAuthorityRecords(records); err != nil {
		t.Fatal(err)
	}
	actorCapability, err := authority.IssueCapability(
		actor.PublicKey(),
		"edge-ahead",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	edgeCapability, err := authority.IssueCapability(
		acceptingEdge.PublicKey(),
		"edge-ahead",
		[]string{"receipt.issue"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	transition, _, err := actor.CreateTransition(node.TransitionRequest{
		Namespace:  "edge-ahead",
		Roots:      transportRoots(t, actor, "work"),
		RefName:    "refs/heads/main",
		Capability: actorCapability,
	})
	if err != nil {
		t.Fatal(err)
	}
	edgeServer := httptest.NewServer(NewServer(acceptingEdge).Handler())
	defer edgeServer.Close()
	client := NewClient(bundle.PeerToken)
	if _, err := client.AcceptTransition(
		context.Background(),
		actor,
		edgeServer.URL,
		transition.ID,
		edgeCapability,
	); err != nil {
		t.Fatal(err)
	}
	actorRecords, err := actor.AuthorityRecords()
	if err != nil {
		t.Fatal(err)
	}
	if len(actorRecords) != len(records) || actorRecords[len(actorRecords)-1].ID != records[len(records)-1].ID {
		t.Fatal("actor did not import the accepting edge's newer authority journal")
	}
}

func TestIndependentAcceptanceKeepsNewerActorAuthorityJournal(t *testing.T) {
	baseDirectory := t.TempDir()
	authority, err := node.Initialize(filepath.Join(baseDirectory, "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	bundle := authority.Domain().EdgeBundle()
	actor, err := node.Initialize(filepath.Join(baseDirectory, "actor"), false, &bundle)
	if err != nil {
		t.Fatal(err)
	}
	acceptingEdge, err := node.Initialize(filepath.Join(baseDirectory, "accepting-edge"), false, &bundle)
	if err != nil {
		t.Fatal(err)
	}
	authorityCapability, err := authority.IssueCapability(
		authority.PublicKey(),
		"authority-progress",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	seed, _, err := authority.CreateTransition(node.TransitionRequest{
		Namespace:  "authority-progress",
		Roots:      transportRoots(t, authority, "seed"),
		RefName:    "refs/heads/main",
		Capability: authorityCapability,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Finalize(seed.ID); err != nil {
		t.Fatal(err)
	}
	records, err := authority.AuthorityRecords()
	if err != nil {
		t.Fatal(err)
	}
	if err := actor.ImportAuthorityRecords(records); err != nil {
		t.Fatal(err)
	}
	actorCapability, err := authority.IssueCapability(
		actor.PublicKey(),
		"actor-ahead",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	edgeCapability, err := authority.IssueCapability(
		acceptingEdge.PublicKey(),
		"actor-ahead",
		[]string{"receipt.issue"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	transition, _, err := actor.CreateTransition(node.TransitionRequest{
		Namespace:  "actor-ahead",
		Roots:      transportRoots(t, actor, "work"),
		RefName:    "refs/heads/main",
		Capability: actorCapability,
	})
	if err != nil {
		t.Fatal(err)
	}
	edgeServer := httptest.NewServer(NewServer(acceptingEdge).Handler())
	defer edgeServer.Close()
	client := NewClient(bundle.PeerToken)
	if _, err := client.AcceptTransition(
		context.Background(),
		actor,
		edgeServer.URL,
		transition.ID,
		edgeCapability,
	); err != nil {
		t.Fatal(err)
	}
	actorRecords, err := actor.AuthorityRecords()
	if err != nil {
		t.Fatal(err)
	}
	if len(actorRecords) != len(records) || actorRecords[len(actorRecords)-1].ID != records[len(records)-1].ID {
		t.Fatal("actor lost its newer authority journal while accepting an older edge receipt")
	}
}

func TestPutObjectRejectsPrivacyMismatchBeforePersistence(t *testing.T) {
	authority, err := node.Initialize(filepath.Join(t.TempDir(), "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	bundle := authority.Domain().EdgeBundle()
	target, err := node.Initialize(filepath.Join(t.TempDir(), "target"), false, &bundle)
	if err != nil {
		t.Fatal(err)
	}
	object := model.NewGraphObject(model.KindSource, "text/plain", []byte("private"), nil)
	privateID, err := authority.PutObject(object, true)
	if err != nil {
		t.Fatal(err)
	}
	publicID, err := target.ExpectedObjectID(object, false)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(target).Handler())
	defer server.Close()
	client := NewClient(bundle.PeerToken)
	if err := client.post(context.Background(), server.URL+"/v0/objects", objectEnvelope{
		ID:      privateID,
		Private: false,
		Object:  object,
	}, nil); err == nil {
		t.Fatal("privacy-mismatched upload unexpectedly succeeded")
	}
	if _, err := target.GetObject(publicID); err == nil {
		t.Fatal("privacy-mismatched upload left a public plaintext object")
	}
}

func TestTLSClientWithCustomCAReadsAuthenticatedStats(t *testing.T) {
	authority, err := node.Initialize(filepath.Join(t.TempDir(), "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(NewServer(authority).Handler())
	defer server.Close()
	certificate := server.Certificate()
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certificate.Raw,
	}), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := NewClientWithCA(authority.Domain().PeerToken, caPath)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := client.PeerStats(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if stats.NodeID != authority.NodeID() || !stats.Authority {
		t.Fatalf("unexpected TLS stats response: %+v", stats)
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
