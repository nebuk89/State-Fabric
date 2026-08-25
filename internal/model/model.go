package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/nebuk89/cdn_git/internal/canonical"
)

const ProtocolVersion = "0.1"

var namespacePattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

type GraphKind string

const (
	KindSource     GraphKind = "source"
	KindWorkspace  GraphKind = "workspace"
	KindProvenance GraphKind = "provenance"
	KindManifest   GraphKind = "manifest"
)

type GraphObject struct {
	ProtocolVersion string    `json:"protocol_version"`
	Kind            GraphKind `json:"kind"`
	Links           []string  `json:"links"`
	MediaType       string    `json:"media_type"`
	Payload         []byte    `json:"payload"`
}

func NewGraphObject(kind GraphKind, mediaType string, payload []byte, links []string) GraphObject {
	normalizedLinks := append([]string(nil), links...)
	sort.Strings(normalizedLinks)
	return GraphObject{
		ProtocolVersion: ProtocolVersion,
		Kind:            kind,
		Links:           normalizedLinks,
		MediaType:       mediaType,
		Payload:         append([]byte(nil), payload...),
	}
}

func (object GraphObject) Validate() error {
	if object.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("unsupported protocol version %q", object.ProtocolVersion)
	}
	switch object.Kind {
	case KindSource, KindWorkspace, KindProvenance, KindManifest:
	default:
		return fmt.Errorf("unsupported graph kind %q", object.Kind)
	}
	if object.MediaType == "" {
		return errors.New("media type is required")
	}
	for index, link := range object.Links {
		if link == "" {
			return errors.New("empty graph link")
		}
		if index > 0 && object.Links[index-1] >= link {
			return errors.New("graph links must be sorted and unique")
		}
	}
	return nil
}

type Roots struct {
	Source     string `json:"source"`
	Workspace  string `json:"workspace"`
	Provenance string `json:"provenance"`
}

func (roots Roots) Values() []string {
	return []string{roots.Source, roots.Workspace, roots.Provenance}
}

type ExpectedRef struct {
	Name         string `json:"name"`
	TransitionID string `json:"transition_id"`
}

type RefIntent struct {
	Name      string `json:"name"`
	Operation string `json:"operation"`
}

type CapabilityBody struct {
	ProtocolVersion  string   `json:"protocol_version"`
	TrustDomainID    string   `json:"trust_domain_id"`
	Namespace        string   `json:"namespace"`
	SubjectPublicKey string   `json:"subject_public_key"`
	Operations       []string `json:"operations"`
	IssuedAtUnix     int64    `json:"issued_at_unix"`
	ExpiresAtUnix    int64    `json:"expires_at_unix"`
	MaxDepth         int      `json:"max_depth"`
}

type Capability struct {
	Body            CapabilityBody `json:"body"`
	ID              string         `json:"id"`
	IssuerPublicKey string         `json:"issuer_public_key"`
	Signature       string         `json:"signature"`
}

func (capability Capability) Allows(operation string) bool {
	index := sort.SearchStrings(capability.Body.Operations, operation)
	return index < len(capability.Body.Operations) && capability.Body.Operations[index] == operation
}

type TransitionBody struct {
	ProtocolVersion        string        `json:"protocol_version"`
	Namespace              string        `json:"namespace"`
	TrustDomainID          string        `json:"trust_domain_id"`
	ParentTransitions      []string      `json:"parent_transitions"`
	ExpectedRefs           []ExpectedRef `json:"expected_refs"`
	Roots                  Roots         `json:"roots"`
	RequiredObjectManifest string        `json:"required_object_manifest"`
	RefIntents             []RefIntent   `json:"ref_intents"`
	ActorPublicKey         string        `json:"actor_public_key"`
	Capability             Capability    `json:"capability"`
	PolicyContext          string        `json:"policy_context"`
}

type Transition struct {
	Body           TransitionBody `json:"body"`
	ID             string         `json:"id"`
	ActorSignature string         `json:"actor_signature"`
}

func (transition Transition) ExpectedRef(name string) string {
	for _, expected := range transition.Body.ExpectedRefs {
		if expected.Name == name {
			return expected.TransitionID
		}
	}
	return ""
}

func (transition Transition) ValidateShape() error {
	body := transition.Body
	if body.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("unsupported protocol version %q", body.ProtocolVersion)
	}
	if body.Namespace == "" || body.TrustDomainID == "" {
		return errors.New("namespace and trust domain are required")
	}
	if !namespacePattern.MatchString(body.Namespace) {
		return errors.New("namespace contains unsupported characters")
	}
	if body.Roots.Source == "" || body.Roots.Workspace == "" || body.Roots.Provenance == "" {
		return errors.New("all three graph roots are required")
	}
	if body.RequiredObjectManifest == "" {
		return errors.New("required object manifest is required")
	}
	if len(body.RefIntents) != 1 || body.RefIntents[0].Operation != "set" {
		return errors.New("v0 requires exactly one set ref intent")
	}
	if body.RefIntents[0].Name == "" {
		return errors.New("ref name is required")
	}
	if len(body.ExpectedRefs) != 1 || body.ExpectedRefs[0].Name != body.RefIntents[0].Name {
		return errors.New("v0 requires one expected ref matching the ref intent")
	}
	if body.ActorPublicKey == "" {
		return errors.New("actor public key is required")
	}
	if err := validateSortedUnique(body.ParentTransitions, "parent transitions"); err != nil {
		return err
	}
	if err := validateExpectedRefs(body.ExpectedRefs); err != nil {
		return err
	}
	if err := validateRefIntents(body.RefIntents); err != nil {
		return err
	}
	if expected := body.ExpectedRefs[0].TransitionID; expected != "" {
		index := sort.SearchStrings(body.ParentTransitions, expected)
		if index >= len(body.ParentTransitions) || body.ParentTransitions[index] != expected {
			return errors.New("expected ref transition must be a causal parent")
		}
	}
	return nil
}

type EdgeReceiptBody struct {
	ProtocolVersion     string `json:"protocol_version"`
	TransitionID        string `json:"transition_id"`
	AcceptedBy          string `json:"accepted_by"`
	AcceptedAtUnix      int64  `json:"accepted_at_unix"`
	DurabilityClass     string `json:"durability_class"`
	ObjectsManifest     string `json:"objects_manifest"`
	AuthorityCheckpoint string `json:"authority_checkpoint"`
}

type EdgeReceipt struct {
	Body          EdgeReceiptBody `json:"body"`
	ID            string          `json:"id"`
	NodePublicKey string          `json:"node_public_key"`
	NodeSignature string          `json:"node_signature"`
}

type JournalRecordBody struct {
	ProtocolVersion string `json:"protocol_version"`
	Sequence        uint64 `json:"sequence"`
	PreviousRecord  string `json:"previous_record"`
	Type            string `json:"type"`
	Namespace       string `json:"namespace"`
	RefName         string `json:"ref_name"`
	TransitionID    string `json:"transition_id"`
	ReceiptID       string `json:"receipt_id"`
	Expected        string `json:"expected"`
	Result          string `json:"result"`
	RecordedAtUnix  int64  `json:"recorded_at_unix"`
}

type JournalRecord struct {
	Body            JournalRecordBody `json:"body"`
	ID              string            `json:"id"`
	SignerPublicKey string            `json:"signer_public_key"`
	Signature       string            `json:"signature"`
}

type FinalizeResult struct {
	Status        string `json:"status"`
	RefName       string `json:"ref_name"`
	TransitionID  string `json:"transition_id"`
	Current       string `json:"current"`
	DivergentRef  string `json:"divergent_ref"`
	JournalRecord string `json:"journal_record"`
}

func CanonicalID(prefix string, value any) (string, error) {
	data, err := canonical.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return prefix + hex.EncodeToString(digest[:]), nil
}

func validateSortedUnique(values []string, name string) error {
	for index, value := range values {
		if value == "" {
			return fmt.Errorf("%s contains an empty value", name)
		}
		if index > 0 && values[index-1] >= value {
			return fmt.Errorf("%s must be sorted and unique", name)
		}
	}
	return nil
}

func validateExpectedRefs(values []ExpectedRef) error {
	for index, value := range values {
		if err := validateRefName(value.Name); err != nil {
			return err
		}
		if index > 0 && values[index-1].Name >= value.Name {
			return errors.New("expected refs must be sorted and unique")
		}
	}
	return nil
}

func validateRefIntents(values []RefIntent) error {
	for index, value := range values {
		if err := validateRefName(value.Name); err != nil {
			return err
		}
		if value.Operation == "" {
			return errors.New("ref intent operation is required")
		}
		if index > 0 && strings.Compare(values[index-1].Name, value.Name) >= 0 {
			return errors.New("ref intents must be sorted and unique")
		}
	}
	return nil
}

func validateRefName(name string) error {
	if !strings.HasPrefix(name, "refs/") || strings.ContainsAny(name, ":\x00 \t\r\n") {
		return fmt.Errorf("invalid ref name %q", name)
	}
	return nil
}
