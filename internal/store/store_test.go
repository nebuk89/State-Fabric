package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/nebuk89/cdn_git/internal/model"
	"github.com/nebuk89/cdn_git/internal/security"
)

func TestPublicAndPrivateObjectIdentity(t *testing.T) {
	domain, err := security.NewAuthorityDomain()
	if err != nil {
		t.Fatal(err)
	}

	first, err := Open(t.TempDir(), domain)
	if err != nil {
		t.Fatal(err)
	}
	object := model.NewGraphObject(model.KindSource, "text/plain", []byte("hello"), nil)

	publicID, err := first.Put(object, false)
	if err != nil {
		t.Fatal(err)
	}
	privateID, err := first.Put(object, true)
	if err != nil {
		t.Fatal(err)
	}
	if publicID == privateID {
		t.Fatal("public and private IDs unexpectedly match")
	}

	secondRoot := t.TempDir()
	second, err := Open(secondRoot, domain.EdgeBundle())
	if err != nil {
		t.Fatal(err)
	}
	samePrivateID, err := second.Put(object, true)
	if err != nil {
		t.Fatal(err)
	}
	if samePrivateID != privateID {
		t.Fatalf("same-domain private dedup failed: %s != %s", samePrivateID, privateID)
	}

	otherDomain, err := security.NewAuthorityDomain()
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(t.TempDir(), otherDomain)
	if err != nil {
		t.Fatal(err)
	}
	otherPrivateID, err := other.Put(object, true)
	if err != nil {
		t.Fatal(err)
	}
	if otherPrivateID == privateID {
		t.Fatal("cross-domain private handles leaked equality")
	}

	path, err := first.pathForID(privateID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("hello")) {
		t.Fatal("private object was stored as plaintext")
	}
	got, err := first.Get(privateID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Payload, object.Payload) {
		t.Fatalf("private payload mismatch: %q", got.Payload)
	}
}

func TestPutRejectsObjectTooLargeForReplicationEnvelope(t *testing.T) {
	domain, err := security.NewAuthorityDomain()
	if err != nil {
		t.Fatal(err)
	}
	current, err := Open(t.TempDir(), domain)
	if err != nil {
		t.Fatal(err)
	}
	payloadSize := (model.MaxGraphObjectSize * 3 / 4) + 1024
	object := model.NewGraphObject(
		model.KindSource,
		"application/octet-stream",
		bytes.Repeat([]byte{'x'}, payloadSize),
		nil,
	)
	if _, err := current.Put(object, false); err == nil {
		t.Fatal("store accepted an object too large for the replication transport")
	}
	stats, err := current.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.PublicObjects != 0 || stats.PrivateObjects != 0 {
		t.Fatal("oversized object persisted despite rejection")
	}
}

func TestVerifyClosureRejectsMissingObject(t *testing.T) {
	domain, err := security.NewAuthorityDomain()
	if err != nil {
		t.Fatal(err)
	}
	objectStore, err := Open(t.TempDir(), domain)
	if err != nil {
		t.Fatal(err)
	}
	root := model.NewGraphObject(
		model.KindManifest,
		"application/vnd.fabric.manifest+json",
		[]byte("{}"),
		[]string{"obj:sha256:" + string(bytes.Repeat([]byte{'0'}, 64))},
	)
	rootID, err := objectStore.Put(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := objectStore.VerifyClosure([]string{rootID}); err == nil {
		t.Fatal("missing closure unexpectedly verified")
	}
}

func TestObjectPersistsAcrossReopen(t *testing.T) {
	root := t.TempDir()
	domain, err := security.NewAuthorityDomain()
	if err != nil {
		t.Fatal(err)
	}
	first, err := Open(root, domain)
	if err != nil {
		t.Fatal(err)
	}
	id, err := first.Put(model.NewGraphObject(model.KindWorkspace, "text/plain", []byte("state"), nil), true)
	if err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(root, domain)
	if err != nil {
		t.Fatal(err)
	}
	object, err := reopened.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if string(object.Payload) != "state" {
		t.Fatalf("unexpected payload %q", object.Payload)
	}

	if _, err := os.Stat(filepath.Join(root, "objects")); err != nil {
		t.Fatal(err)
	}
}
