package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nebuk89/cdn_git/internal/canonical"
	"github.com/nebuk89/cdn_git/internal/model"
	"github.com/nebuk89/cdn_git/internal/security"
)

type encryptedEnvelope struct {
	Version    string `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type Store struct {
	root          string
	domainID      string
	identityKey   []byte
	encryptionKey []byte
}

func Open(root string, domain security.DomainConfig) (*Store, error) {
	identityKey, err := domain.IdentitySecret()
	if err != nil {
		return nil, err
	}
	encryptionKey, err := domain.EncryptionSecret()
	if err != nil {
		return nil, err
	}
	store := &Store{
		root:          root,
		domainID:      domain.ID,
		identityKey:   identityKey,
		encryptionKey: encryptionKey,
	}
	if err := os.MkdirAll(filepath.Join(root, "objects"), 0o700); err != nil {
		return nil, err
	}
	return store, nil
}

func (store *Store) Put(object model.GraphObject, private bool) (string, error) {
	if err := object.Validate(); err != nil {
		return "", err
	}
	plaintext, err := canonical.Marshal(object)
	if err != nil {
		return "", err
	}
	id := store.objectID(plaintext, private)
	path, err := store.pathForID(id)
	if err != nil {
		return "", err
	}
	if existing, err := os.ReadFile(path); err == nil {
		if err := store.verifyStored(id, existing); err != nil {
			return "", fmt.Errorf("immutable object collision at %s: %w", id, err)
		}
		return id, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	data := plaintext
	if private {
		data, err = store.encrypt(plaintext)
		if err != nil {
			return "", err
		}
	}
	if err := writeFileDurable(path, data, 0o600); err != nil {
		return "", err
	}
	return id, nil
}

func (store *Store) Get(id string) (model.GraphObject, error) {
	path, err := store.pathForID(id)
	if err != nil {
		return model.GraphObject{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.GraphObject{}, fmt.Errorf("object %s not found", id)
		}
		return model.GraphObject{}, err
	}
	plaintext := data
	if strings.HasPrefix(id, "priv:") {
		plaintext, err = store.decrypt(data)
		if err != nil {
			return model.GraphObject{}, fmt.Errorf("decrypt %s: %w", id, err)
		}
	}
	if store.objectID(plaintext, strings.HasPrefix(id, "priv:")) != id {
		return model.GraphObject{}, errors.New("object identity verification failed")
	}
	var object model.GraphObject
	if err := canonical.Decode(plaintext, &object); err != nil {
		return model.GraphObject{}, err
	}
	if err := object.Validate(); err != nil {
		return model.GraphObject{}, err
	}
	return object, nil
}

func (store *Store) Has(id string) bool {
	path, err := store.pathForID(id)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func (store *Store) VerifyClosure(roots []string) ([]string, error) {
	pending := append([]string(nil), roots...)
	visited := make(map[string]struct{})
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, ok := visited[id]; ok {
			continue
		}
		object, err := store.Get(id)
		if err != nil {
			return nil, fmt.Errorf("incomplete object closure at %s: %w", id, err)
		}
		visited[id] = struct{}{}
		pending = append(pending, object.Links...)
	}
	result := make([]string, 0, len(visited))
	for id := range visited {
		result = append(result, id)
	}
	sort.Strings(result)
	return result, nil
}

func (store *Store) IsPrivate(id string) bool {
	return strings.HasPrefix(id, "priv:")
}

func (store *Store) objectID(plaintext []byte, private bool) string {
	digest := sha256.Sum256(plaintext)
	if !private {
		return "obj:sha256:" + hex.EncodeToString(digest[:])
	}
	mac := hmac.New(sha256.New, store.identityKey)
	mac.Write(digest[:])
	return "priv:" + store.domainID + ":1:" + hex.EncodeToString(mac.Sum(nil))
}

func (store *Store) pathForID(id string) (string, error) {
	parts := strings.Split(id, ":")
	var category, domain, version, digest string
	switch {
	case len(parts) == 3 && parts[0] == "obj" && parts[1] == "sha256":
		category, digest = "public", parts[2]
	case len(parts) == 4 && parts[0] == "priv":
		category, domain, version, digest = "private", parts[1], parts[2], parts[3]
		if domain != store.domainID || version != "1" {
			return "", errors.New("private object belongs to another trust domain or key version")
		}
	default:
		return "", fmt.Errorf("invalid object ID %q", id)
	}
	if len(digest) != sha256.Size*2 {
		return "", errors.New("invalid object digest length")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", errors.New("invalid object digest")
	}
	if category == "public" {
		return filepath.Join(store.root, "objects", category, digest[:2], digest[2:]+".obj"), nil
	}
	return filepath.Join(store.root, "objects", category, domain, version, digest[:2], digest[2:]+".obj"), nil
}

func (store *Store) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(store.encryptionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, []byte(store.domainID))
	return canonical.Marshal(encryptedEnvelope{
		Version:    "aes-256-gcm-v0",
		Nonce:      base64.RawURLEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawURLEncoding.EncodeToString(ciphertext),
	})
}

func (store *Store) decrypt(data []byte) ([]byte, error) {
	var envelope encryptedEnvelope
	if err := canonical.Decode(data, &envelope); err != nil {
		return nil, err
	}
	if envelope.Version != "aes-256-gcm-v0" {
		return nil, errors.New("unsupported encryption envelope")
	}
	nonce, err := base64.RawURLEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(store.encryptionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid AES-GCM nonce length")
	}
	return gcm.Open(nil, nonce, ciphertext, []byte(store.domainID))
}

func (store *Store) verifyStored(id string, data []byte) error {
	plaintext := data
	var err error
	private := strings.HasPrefix(id, "priv:")
	if private {
		plaintext, err = store.decrypt(data)
		if err != nil {
			return err
		}
	}
	if store.objectID(plaintext, private) != id {
		return errors.New("stored bytes do not match object ID")
	}
	return nil
}

func writeFileDurable(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".tmp-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer directoryHandle.Close()
	return directoryHandle.Sync()
}
