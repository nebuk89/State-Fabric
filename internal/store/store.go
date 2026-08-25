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
	"time"

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

type ObjectInfo struct {
	ID       string    `json:"id"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Private  bool      `json:"private"`
}

type Stats struct {
	PublicObjects  int   `json:"public_objects"`
	PrivateObjects int   `json:"private_objects"`
	StoredBytes    int64 `json:"stored_bytes"`
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
	plaintext, id, err := store.prepare(object, private)
	if err != nil {
		return "", err
	}
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

func (store *Store) ExpectedID(object model.GraphObject, private bool) (string, error) {
	_, id, err := store.prepare(object, private)
	return id, err
}

func (store *Store) prepare(object model.GraphObject, private bool) ([]byte, string, error) {
	if err := object.Validate(); err != nil {
		return nil, "", err
	}
	plaintext, err := canonical.Marshal(object)
	if err != nil {
		return nil, "", err
	}
	if len(plaintext) > model.MaxGraphObjectSize {
		return nil, "", fmt.Errorf(
			"graph object canonical envelope is %d bytes; maximum is %d bytes",
			len(plaintext),
			model.MaxGraphObjectSize,
		)
	}
	id := store.objectID(plaintext, private)
	return plaintext, id, nil
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

func (store *Store) Objects() ([]ObjectInfo, error) {
	var result []ObjectInfo
	root := filepath.Join(store.root, "objects")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".obj") {
			return nil
		}
		id, err := store.idForPath(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		result = append(result, ObjectInfo{
			ID:       id,
			Size:     info.Size(),
			Modified: info.ModTime().UTC(),
			Private:  strings.HasPrefix(id, "priv:"),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].ID < result[right].ID
	})
	return result, nil
}

func (store *Store) Stats() (Stats, error) {
	objects, err := store.Objects()
	if err != nil {
		return Stats{}, err
	}
	var result Stats
	for _, object := range objects {
		if object.Private {
			result.PrivateObjects++
		} else {
			result.PublicObjects++
		}
		result.StoredBytes += object.Size
	}
	return result, nil
}

func (store *Store) Delete(id string) error {
	path, err := store.pathForID(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
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

func (store *Store) idForPath(path string) (string, error) {
	relative, err := filepath.Rel(filepath.Join(store.root, "objects"), path)
	if err != nil {
		return "", err
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	switch {
	case len(parts) == 3 && parts[0] == "public":
		digest := parts[1] + strings.TrimSuffix(parts[2], ".obj")
		return "obj:sha256:" + digest, nil
	case len(parts) == 5 && parts[0] == "private":
		digest := parts[3] + strings.TrimSuffix(parts[4], ".obj")
		return "priv:" + parts[1] + ":" + parts[2] + ":" + digest, nil
	default:
		return "", fmt.Errorf("unexpected object path %q", relative)
	}
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
