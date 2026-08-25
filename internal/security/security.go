package security

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nebuk89/cdn_git/internal/canonical"
	"github.com/nebuk89/cdn_git/internal/model"
)

type Identity struct {
	Version    string `json:"version"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

func NewIdentity() (Identity, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, fmt.Errorf("generate Ed25519 identity: %w", err)
	}
	return Identity{
		Version:    model.ProtocolVersion,
		PublicKey:  base64.RawURLEncoding.EncodeToString(publicKey),
		PrivateKey: base64.RawURLEncoding.EncodeToString(privateKey),
	}, nil
}

func (identity Identity) Validate() error {
	if identity.Version != model.ProtocolVersion {
		return fmt.Errorf("unsupported identity version %q", identity.Version)
	}
	publicKey, err := identity.Public()
	if err != nil {
		return err
	}
	privateKey, err := identity.Private()
	if err != nil {
		return err
	}
	if !ed25519.PublicKey(privateKey.Public().(ed25519.PublicKey)).Equal(publicKey) {
		return errors.New("identity public and private keys do not match")
	}
	return nil
}

func (identity Identity) Public() (ed25519.PublicKey, error) {
	return decodePublicKey(identity.PublicKey)
}

func (identity Identity) Private() (ed25519.PrivateKey, error) {
	key, err := base64.RawURLEncoding.DecodeString(identity.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid Ed25519 private key length")
	}
	return ed25519.PrivateKey(key), nil
}

func (identity Identity) NodeID() (string, error) {
	return NodeIDForPublicKey(identity.PublicKey)
}

func (identity Identity) Sign(message string) (string, error) {
	privateKey, err := identity.Private()
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(message))), nil
}

type DomainConfig struct {
	Version             string `json:"version"`
	ID                  string `json:"id"`
	IdentityKey         string `json:"identity_key"`
	EncryptionKey       string `json:"encryption_key"`
	PeerToken           string `json:"peer_token"`
	AuthorityPublicKey  string `json:"authority_public_key"`
	AuthorityPrivateKey string `json:"authority_private_key"`
}

func NewAuthorityDomain() (DomainConfig, error) {
	identityKey, err := randomBytes(32)
	if err != nil {
		return DomainConfig{}, err
	}
	encryptionKey, err := randomBytes(32)
	if err != nil {
		return DomainConfig{}, err
	}
	peerToken, err := randomBytes(32)
	if err != nil {
		return DomainConfig{}, err
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return DomainConfig{}, fmt.Errorf("generate authority key: %w", err)
	}
	domainDigest := sha256.Sum256(publicKey)
	return DomainConfig{
		Version:             model.ProtocolVersion,
		ID:                  "td-" + hex.EncodeToString(domainDigest[:16]),
		IdentityKey:         base64.RawURLEncoding.EncodeToString(identityKey),
		EncryptionKey:       base64.RawURLEncoding.EncodeToString(encryptionKey),
		PeerToken:           base64.RawURLEncoding.EncodeToString(peerToken),
		AuthorityPublicKey:  base64.RawURLEncoding.EncodeToString(publicKey),
		AuthorityPrivateKey: base64.RawURLEncoding.EncodeToString(privateKey),
	}, nil
}

func (config DomainConfig) EdgeBundle() DomainConfig {
	config.AuthorityPrivateKey = ""
	return config
}

func (config DomainConfig) Validate(requireAuthority bool) error {
	if config.Version != model.ProtocolVersion || config.ID == "" {
		return errors.New("invalid domain version or ID")
	}
	for _, encoded := range []struct {
		name  string
		value string
	}{
		{"identity key", config.IdentityKey},
		{"encryption key", config.EncryptionKey},
		{"peer token", config.PeerToken},
	} {
		decoded, err := base64.RawURLEncoding.DecodeString(encoded.value)
		if err != nil || len(decoded) != 32 {
			return fmt.Errorf("invalid %s", encoded.name)
		}
	}
	if _, err := decodePublicKey(config.AuthorityPublicKey); err != nil {
		return fmt.Errorf("invalid authority public key: %w", err)
	}
	if requireAuthority {
		privateKey, err := config.AuthorityPrivate()
		if err != nil {
			return err
		}
		if !privateKey.Public().(ed25519.PublicKey).Equal(mustPublic(config.AuthorityPublicKey)) {
			return errors.New("authority public and private keys do not match")
		}
	}
	return nil
}

func (config DomainConfig) IdentitySecret() ([]byte, error) {
	return decodeSecret(config.IdentityKey, "identity key")
}

func (config DomainConfig) EncryptionSecret() ([]byte, error) {
	return decodeSecret(config.EncryptionKey, "encryption key")
}

func (config DomainConfig) AuthorityPrivate() (ed25519.PrivateKey, error) {
	key, err := base64.RawURLEncoding.DecodeString(config.AuthorityPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("decode authority private key: %w", err)
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("authority private key is unavailable")
	}
	return ed25519.PrivateKey(key), nil
}

func IssueCapability(config DomainConfig, subjectPublicKey, namespace string, operations []string, ttl time.Duration) (model.Capability, error) {
	privateKey, err := config.AuthorityPrivate()
	if err != nil {
		return model.Capability{}, err
	}
	if _, err := decodePublicKey(subjectPublicKey); err != nil {
		return model.Capability{}, fmt.Errorf("invalid subject public key: %w", err)
	}
	normalizedOperations := append([]string(nil), operations...)
	sort.Strings(normalizedOperations)
	now := time.Now().UTC()
	body := model.CapabilityBody{
		ProtocolVersion:  model.ProtocolVersion,
		TrustDomainID:    config.ID,
		Namespace:        namespace,
		SubjectPublicKey: subjectPublicKey,
		Operations:       normalizedOperations,
		IssuedAtUnix:     now.Unix(),
		ExpiresAtUnix:    now.Add(ttl).Unix(),
		MaxDepth:         0,
	}
	id, err := model.CanonicalID("cap:sha256:", body)
	if err != nil {
		return model.Capability{}, err
	}
	return model.Capability{
		Body:            body,
		ID:              id,
		IssuerPublicKey: config.AuthorityPublicKey,
		Signature:       base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(id))),
	}, nil
}

func VerifyCapability(config DomainConfig, capability model.Capability, subjectPublicKey, namespace, operation string, now time.Time) error {
	if err := VerifyCapabilityScope(config, capability, subjectPublicKey, namespace, operation); err != nil {
		return err
	}
	if now.Unix() < capability.Body.IssuedAtUnix || now.Unix() >= capability.Body.ExpiresAtUnix {
		return errors.New("capability is not currently valid")
	}
	return nil
}

func VerifyCapabilityScope(config DomainConfig, capability model.Capability, subjectPublicKey, namespace, operation string) error {
	if capability.Body.ProtocolVersion != model.ProtocolVersion {
		return errors.New("unsupported capability version")
	}
	if capability.Body.TrustDomainID != config.ID || capability.Body.Namespace != namespace {
		return errors.New("capability scope mismatch")
	}
	if capability.Body.SubjectPublicKey != subjectPublicKey {
		return errors.New("capability subject mismatch")
	}
	if capability.IssuerPublicKey != config.AuthorityPublicKey {
		return errors.New("untrusted capability issuer")
	}
	expectedID, err := model.CanonicalID("cap:sha256:", capability.Body)
	if err != nil {
		return err
	}
	if expectedID != capability.ID {
		return errors.New("capability ID mismatch")
	}
	publicKey, err := decodePublicKey(capability.IssuerPublicKey)
	if err != nil {
		return err
	}
	signature, err := decodeSignature(capability.Signature)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, []byte(capability.ID), signature) {
		return errors.New("invalid capability signature")
	}
	if !capability.Allows(operation) {
		return fmt.Errorf("capability does not allow %q", operation)
	}
	return nil
}

func VerifySignature(publicKeyEncoded, message, signatureEncoded string) error {
	publicKey, err := decodePublicKey(publicKeyEncoded)
	if err != nil {
		return err
	}
	signature, err := decodeSignature(signatureEncoded)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, []byte(message), signature) {
		return errors.New("invalid Ed25519 signature")
	}
	return nil
}

func NodeIDForPublicKey(publicKeyEncoded string) (string, error) {
	publicKey, err := decodePublicKey(publicKeyEncoded)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(publicKey)
	return "node:ed25519:" + hex.EncodeToString(digest[:]), nil
}

func SignWithPrivate(privateKey ed25519.PrivateKey, message string) string {
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(message)))
}

func Save(path string, value any, mode os.FileMode) error {
	data, err := canonical.Marshal(value)
	if err != nil {
		return err
	}
	return writeFileDurable(path, data, mode)
}

func Load(path string, destination any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return canonical.Decode(data, destination)
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
	return syncDirectory(directory)
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}

func randomBytes(length int) ([]byte, error) {
	data := make([]byte, length)
	if _, err := rand.Read(data); err != nil {
		return nil, fmt.Errorf("read cryptographic randomness: %w", err)
	}
	return data, nil
}

func decodeSecret(encoded, name string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != 32 {
		return nil, fmt.Errorf("invalid %s", name)
	}
	return decoded, nil
}

func decodePublicKey(encoded string) (ed25519.PublicKey, error) {
	key, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, errors.New("invalid Ed25519 public key length")
	}
	return ed25519.PublicKey(key), nil
}

func decodeSignature(encoded string) ([]byte, error) {
	signature, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return nil, errors.New("invalid Ed25519 signature length")
	}
	return signature, nil
}

func mustPublic(encoded string) ed25519.PublicKey {
	key, err := decodePublicKey(encoded)
	if err != nil {
		panic(err)
	}
	return key
}
