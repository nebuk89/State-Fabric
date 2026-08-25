package node

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nebuk89/cdn_git/internal/model"
)

func TestAcceptedTransitionSurvivesRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "authority")
	authority, err := Initialize(root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := authority.IssueCapability(
		authority.PublicKey(),
		"demo",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	roots := putRoots(t, authority, "base")
	transition, receipt, err := authority.CreateTransition(TransitionRequest{
		Namespace:  "demo",
		Roots:      roots,
		RefName:    "refs/heads/main",
		Capability: capability,
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.ID == "" || receipt.ID == "" {
		t.Fatal("transition or receipt ID is empty")
	}

	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.LoadTransition(transition.ID); err != nil {
		t.Fatalf("transition did not survive restart: %v", err)
	}
	if _, err := reopened.LoadReceipt(receipt.ID); err != nil {
		t.Fatalf("receipt did not survive restart: %v", err)
	}
	result, err := reopened.Finalize(transition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "finalized" {
		t.Fatalf("got finalization status %q", result.Status)
	}
}

func TestConcurrentTransitionsPreserveDivergentHead(t *testing.T) {
	authority, err := Initialize(filepath.Join(t.TempDir(), "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := authority.IssueCapability(
		authority.PublicKey(),
		"demo",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	base, _, err := authority.CreateTransition(TransitionRequest{
		Namespace:  "demo",
		Roots:      putRoots(t, authority, "base"),
		RefName:    "refs/heads/main",
		Capability: capability,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Finalize(base.ID); err != nil {
		t.Fatal(err)
	}

	first, _, err := authority.CreateTransition(TransitionRequest{
		Namespace:         "demo",
		Roots:             putRoots(t, authority, "first"),
		ParentTransitions: []string{base.ID},
		RefName:           "refs/heads/main",
		Expected:          base.ID,
		Capability:        capability,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := authority.CreateTransition(TransitionRequest{
		Namespace:         "demo",
		Roots:             putRoots(t, authority, "second"),
		ParentTransitions: []string{base.ID},
		RefName:           "refs/heads/main",
		Expected:          base.ID,
		Capability:        capability,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstResult, err := authority.Finalize(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := authority.Finalize(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstResult.Status != "finalized" || secondResult.Status != "divergent" {
		t.Fatalf("unexpected outcomes: first=%s second=%s", firstResult.Status, secondResult.Status)
	}

	reopened, err := Open(authority.Root())
	if err != nil {
		t.Fatal(err)
	}
	refs, divergent, err := reopened.Refs()
	if err != nil {
		t.Fatal(err)
	}
	if refs["demo:refs/heads/main"] != first.ID {
		t.Fatal("finalized ref was not reconstructed from journal")
	}
	if divergent[secondResult.DivergentRef] != second.ID {
		t.Fatal("divergent head was not reconstructed from journal")
	}
}

func TestRevokedCapabilityCannotAcceptTransition(t *testing.T) {
	authority, err := Initialize(filepath.Join(t.TempDir(), "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := authority.IssueCapability(
		authority.PublicKey(),
		"demo",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.RevokeCapability(capability.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := authority.CreateTransition(TransitionRequest{
		Namespace:  "demo",
		Roots:      putRoots(t, authority, "blocked"),
		RefName:    "refs/heads/main",
		Capability: capability,
	}); err == nil {
		t.Fatal("revoked capability unexpectedly accepted a transition")
	}
}

func TestAcceptedBeforeRevocationCanStillFinalize(t *testing.T) {
	authority, err := Initialize(filepath.Join(t.TempDir(), "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := authority.IssueCapability(
		authority.PublicKey(),
		"demo",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	transition, _, err := authority.CreateTransition(TransitionRequest{
		Namespace:  "demo",
		Roots:      putRoots(t, authority, "accepted-before-revocation"),
		RefName:    "refs/heads/main",
		Capability: capability,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.RevokeCapability(capability.ID); err != nil {
		t.Fatal(err)
	}
	result, err := authority.Finalize(transition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "finalized" {
		t.Fatalf("accepted transition did not finalize after revocation: %s", result.Status)
	}
}

func TestFinalizationIsIdempotent(t *testing.T) {
	authority, err := Initialize(filepath.Join(t.TempDir(), "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := authority.IssueCapability(
		authority.PublicKey(),
		"demo",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	transition, _, err := authority.CreateTransition(TransitionRequest{
		Namespace:  "demo",
		Roots:      putRoots(t, authority, "idempotent"),
		RefName:    "refs/heads/main",
		Capability: capability,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := authority.Finalize(transition.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := authority.Finalize(transition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "finalized" || second.Status != "finalized" {
		t.Fatalf("retry changed finalization result: first=%s second=%s", first.Status, second.Status)
	}
	if first.JournalRecord != second.JournalRecord {
		t.Fatal("idempotent finalization appended another authority record")
	}
}

func TestCrossProcessStyleFinalizationSerializes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "authority")
	firstNode, err := Initialize(root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := firstNode.IssueCapability(
		firstNode.PublicKey(),
		"demo",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	base, _, err := firstNode.CreateTransition(TransitionRequest{
		Namespace:  "demo",
		Roots:      putRoots(t, firstNode, "base"),
		RefName:    "refs/heads/main",
		Capability: capability,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := firstNode.Finalize(base.ID); err != nil {
		t.Fatal(err)
	}
	left, _, err := firstNode.CreateTransition(TransitionRequest{
		Namespace:         "demo",
		Roots:             putRoots(t, firstNode, "left"),
		ParentTransitions: []string{base.ID},
		RefName:           "refs/heads/main",
		Expected:          base.ID,
		Capability:        capability,
	})
	if err != nil {
		t.Fatal(err)
	}
	right, _, err := firstNode.CreateTransition(TransitionRequest{
		Namespace:         "demo",
		Roots:             putRoots(t, firstNode, "right"),
		ParentTransitions: []string{base.ID},
		RefName:           "refs/heads/main",
		Expected:          base.ID,
		Capability:        capability,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondNode, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}

	results := make(chan model.FinalizeResult, 2)
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for _, input := range []struct {
		current *Node
		id      string
	}{{firstNode, left.ID}, {secondNode, right.ID}} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := input.current.Finalize(input.id)
			results <- result
			errs <- err
		}()
	}
	wait.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	statuses := map[string]int{}
	for result := range results {
		statuses[result.Status]++
	}
	if statuses["finalized"] != 1 || statuses["divergent"] != 1 {
		t.Fatalf("cross-process-style finalization outcomes: %v", statuses)
	}
	if _, err := Open(root); err != nil {
		t.Fatalf("authority journal was corrupted: %v", err)
	}
}

func TestTransitionRequiresCorrectGraphRoles(t *testing.T) {
	authority, err := Initialize(filepath.Join(t.TempDir(), "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := authority.IssueCapability(
		authority.PublicKey(),
		"demo",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	roots := putRoots(t, authority, "roles")
	roots.Source, roots.Workspace = roots.Workspace, roots.Source
	if _, _, err := authority.CreateTransition(TransitionRequest{
		Namespace:  "demo",
		Roots:      roots,
		RefName:    "refs/heads/main",
		Capability: capability,
	}); err == nil {
		t.Fatal("transition with swapped graph roles was accepted")
	}
}

func TestAcceptanceReloadsCrossProcessRevocation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "authority")
	first, err := Initialize(root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := first.IssueCapability(
		first.PublicKey(),
		"demo",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	staleProcess, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.RevokeCapability(capability.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := staleProcess.CreateTransition(TransitionRequest{
		Namespace:  "demo",
		Roots:      putRoots(t, staleProcess, "stale-process"),
		RefName:    "refs/heads/main",
		Capability: capability,
	}); err == nil {
		t.Fatal("stale process accepted a capability revoked by another process")
	}
}

func TestRevocationIsIdempotentAndKeepsEarliestSequence(t *testing.T) {
	authority, err := Initialize(filepath.Join(t.TempDir(), "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := authority.IssueCapability(
		authority.PublicKey(),
		"demo",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.RevokeCapability(capability.ID); err != nil {
		t.Fatal(err)
	}
	if err := authority.RevokeCapability(capability.ID); err != nil {
		t.Fatal(err)
	}
	records, err := authority.AuthorityRecords()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, record := range records {
		if record.Body.Type == "capability_revoked" && record.Body.Result == capability.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("got %d revocation records, want 1", count)
	}
}

func TestRefsReloadCrossProcessAuthorityChanges(t *testing.T) {
	root := filepath.Join(t.TempDir(), "authority")
	writer, err := Initialize(root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := writer.IssueCapability(
		writer.PublicKey(),
		"demo",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	transition, _, err := writer.CreateTransition(TransitionRequest{
		Namespace:  "demo",
		Roots:      putRoots(t, writer, "cross-process-read"),
		RefName:    "refs/heads/main",
		Capability: capability,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Finalize(transition.ID); err != nil {
		t.Fatal(err)
	}
	refs, _, err := reader.Refs()
	if err != nil {
		t.Fatal(err)
	}
	if refs["demo:refs/heads/main"] != transition.ID {
		t.Fatal("long-running reader did not reload authority journal")
	}
}

func TestNamespaceCannotCollideWithRefKeyEncoding(t *testing.T) {
	authority, err := Initialize(filepath.Join(t.TempDir(), "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := authority.IssueCapability(
		authority.PublicKey(),
		"a:b",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := authority.CreateTransition(TransitionRequest{
		Namespace:  "a:b",
		Roots:      putRoots(t, authority, "invalid-namespace"),
		RefName:    "refs/heads/main",
		Capability: capability,
	}); err == nil {
		t.Fatal("ambiguous namespace was accepted")
	}
}

func TestExpectedRefMustBeCausalParent(t *testing.T) {
	authority, err := Initialize(filepath.Join(t.TempDir(), "authority"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := authority.IssueCapability(
		authority.PublicKey(),
		"demo",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	base, _, err := authority.CreateTransition(TransitionRequest{
		Namespace:  "demo",
		Roots:      putRoots(t, authority, "base"),
		RefName:    "refs/heads/main",
		Capability: capability,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Finalize(base.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := authority.CreateTransition(TransitionRequest{
		Namespace:  "demo",
		Roots:      putRoots(t, authority, "missing-parent"),
		RefName:    "refs/heads/main",
		Expected:   base.ID,
		Capability: capability,
	}); err == nil {
		t.Fatal("transition omitted its expected ref from causal parents")
	}
}

func TestJournaledStagedReceiptRecoversAfterRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "authority")
	authority, err := Initialize(root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := authority.IssueCapability(
		authority.PublicKey(),
		"demo",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, receipt, err := authority.CreateTransition(TransitionRequest{
		Namespace:  "demo",
		Roots:      putRoots(t, authority, "receipt-recovery"),
		RefName:    "refs/heads/main",
		Capability: capability,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(authority.stagedReceiptPath(receipt.ID)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(authority.receiptPath(receipt.ID), authority.stagedReceiptPath(receipt.ID)); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.LoadReceipt(receipt.ID); err != nil {
		t.Fatalf("journaled staged receipt was not recovered: %v", err)
	}
}

func TestReceiptLookupReloadsAuthorityCheckpoint(t *testing.T) {
	root := filepath.Join(t.TempDir(), "authority")
	writer, err := Initialize(root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	staleReader, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := writer.IssueCapability(
		writer.PublicKey(),
		"demo",
		[]string{"transition.accept"},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	base, _, err := writer.CreateTransition(TransitionRequest{
		Namespace:  "demo",
		Roots:      putRoots(t, writer, "base"),
		RefName:    "refs/heads/main",
		Capability: capability,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Finalize(base.ID); err != nil {
		t.Fatal(err)
	}
	child, _, err := writer.CreateTransition(TransitionRequest{
		Namespace:         "demo",
		Roots:             putRoots(t, writer, "child"),
		ParentTransitions: []string{base.ID},
		RefName:           "refs/heads/main",
		Expected:          base.ID,
		Capability:        capability,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := staleReader.ReceiptForTransition(child.ID); err != nil {
		t.Fatalf("stale receipt reader did not reload authority checkpoint: %v", err)
	}
}

func putRoots(t *testing.T, node *Node, prefix string) model.Roots {
	t.Helper()
	put := func(kind model.GraphKind, suffix string, private bool) string {
		id, err := node.PutObject(
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
