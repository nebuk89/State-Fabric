package journal

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/nebuk89/cdn_git/internal/canonical"
	"github.com/nebuk89/cdn_git/internal/model"
	"github.com/nebuk89/cdn_git/internal/security"
)

func TestJournalReplaysAndRejectsFork(t *testing.T) {
	identity, err := security.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "authority.log")
	log, err := Open(path, identity.PublicKey, identity.Sign)
	if err != nil {
		t.Fatal(err)
	}
	first, err := log.Append(model.JournalRecordBody{Type: "finalized", Namespace: "demo", RefName: "refs/heads/main", TransitionID: "txn:one"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := log.Append(model.JournalRecordBody{Type: "divergent", Namespace: "demo", RefName: "refs/heads/main", TransitionID: "txn:two"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Body.PreviousRecord != first.ID {
		t.Fatal("journal hash chain was not linked")
	}

	reopened, err := Open(path, identity.PublicKey, identity.Sign)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Records()) != 2 {
		t.Fatalf("got %d records after replay", len(reopened.Records()))
	}

	records := reopened.Records()
	records[1].Body.TransitionID = "txn:tampered"
	if err := reopened.Import(records); err == nil {
		t.Fatal("tampered journal unexpectedly imported")
	}
}

func TestReadOnlyJournalImportsAuthorityPrefix(t *testing.T) {
	identity, err := security.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	source, err := Open(filepath.Join(t.TempDir(), "source.log"), identity.PublicKey, identity.Sign)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Append(model.JournalRecordBody{Type: "finalized", TransitionID: "txn:one"}); err != nil {
		t.Fatal(err)
	}

	mirrorPath := filepath.Join(t.TempDir(), "mirror.log")
	mirror, err := Open(mirrorPath, identity.PublicKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := mirror.Import(source.Records()); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(mirrorPath, identity.PublicKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Records()) != 1 {
		t.Fatal("authority mirror did not persist")
	}

	privateKey, err := identity.Private()
	if err != nil {
		t.Fatal(err)
	}
	_ = base64.RawURLEncoding.EncodeToString(privateKey.Public().(ed25519.PublicKey))
}

func TestJournalRejectsRollbackToSignedPrefix(t *testing.T) {
	identity, err := security.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "authority.log")
	log, err := Open(path, identity.PublicKey, identity.Sign)
	if err != nil {
		t.Fatal(err)
	}
	first, err := log.Append(model.JournalRecordBody{Type: "finalized", TransitionID: "txn:one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append(model.JournalRecordBody{Type: "finalized", TransitionID: "txn:two"}); err != nil {
		t.Fatal(err)
	}
	encoded, err := canonical.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, identity.PublicKey, identity.Sign); err == nil {
		t.Fatal("journal accepted rollback to an older signed prefix")
	}
}
