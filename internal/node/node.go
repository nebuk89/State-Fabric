package node

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nebuk89/cdn_git/internal/canonical"
	"github.com/nebuk89/cdn_git/internal/filelock"
	"github.com/nebuk89/cdn_git/internal/journal"
	"github.com/nebuk89/cdn_git/internal/model"
	"github.com/nebuk89/cdn_git/internal/security"
	"github.com/nebuk89/cdn_git/internal/store"
)

type Config struct {
	Version         string `json:"version"`
	IsAuthority     bool   `json:"is_authority"`
	DurabilityClass string `json:"durability_class"`
}

type Node struct {
	mu               sync.Mutex
	root             string
	config           Config
	identity         security.Identity
	domain           security.DomainConfig
	store            *store.Store
	localJournal     *journal.Log
	authorityJournal *journal.Log
	refs             map[string]string
	divergent        map[string]string
	revoked          map[string]uint64
}

type TransitionRequest struct {
	Namespace         string
	Roots             model.Roots
	ParentTransitions []string
	RefName           string
	Expected          string
	Capability        model.Capability
	PolicyContext     string
}

type OperationalStats struct {
	NodeID           string      `json:"node_id"`
	Authority        bool        `json:"authority"`
	Objects          store.Stats `json:"objects"`
	Transitions      int         `json:"transitions"`
	Receipts         int         `json:"receipts"`
	AuthorityRecords int         `json:"authority_records"`
	SharedRefs       int         `json:"shared_refs"`
	DivergentRefs    int         `json:"divergent_refs"`
}

type AuditReport struct {
	Objects     int `json:"objects"`
	Transitions int `json:"transitions"`
	Receipts    int `json:"receipts"`
}

type GCReport struct {
	DryRun          bool     `json:"dry_run"`
	GraceSeconds    int64    `json:"grace_seconds"`
	Reachable       int      `json:"reachable_objects"`
	Candidates      int      `json:"candidate_objects"`
	CandidateBytes  int64    `json:"candidate_bytes"`
	Deleted         int      `json:"deleted_objects"`
	DeletedBytes    int64    `json:"deleted_bytes"`
	CandidateObject []string `json:"candidate_object_ids"`
}

func Initialize(root string, authority bool, domainBundle *security.DomainConfig) (*Node, error) {
	if _, err := os.Stat(filepath.Join(root, "config.json")); err == nil {
		return nil, fmt.Errorf("node already initialized at %s", root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	identity, err := security.NewIdentity()
	if err != nil {
		return nil, err
	}
	var domain security.DomainConfig
	if authority {
		if domainBundle != nil {
			return nil, errors.New("authority node cannot be initialized from an edge bundle")
		}
		domain, err = security.NewAuthorityDomain()
		if err != nil {
			return nil, err
		}
	} else {
		if domainBundle == nil {
			return nil, errors.New("edge node requires a domain bundle")
		}
		domain = domainBundle.EdgeBundle()
	}
	config := Config{
		Version:         model.ProtocolVersion,
		IsAuthority:     authority,
		DurabilityClass: "host-disk",
	}
	if err := security.Save(filepath.Join(root, "identity.json"), identity, 0o600); err != nil {
		return nil, err
	}
	if err := security.Save(filepath.Join(root, "domain.json"), domain, 0o600); err != nil {
		return nil, err
	}
	if err := security.Save(filepath.Join(root, "config.json"), config, 0o600); err != nil {
		return nil, err
	}
	return Open(root)
}

func Open(root string) (*Node, error) {
	var config Config
	if err := security.Load(filepath.Join(root, "config.json"), &config); err != nil {
		return nil, fmt.Errorf("load node config: %w", err)
	}
	if config.Version != model.ProtocolVersion || config.DurabilityClass == "" {
		return nil, errors.New("invalid node config")
	}
	var identity security.Identity
	if err := security.Load(filepath.Join(root, "identity.json"), &identity); err != nil {
		return nil, fmt.Errorf("load node identity: %w", err)
	}
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	var domain security.DomainConfig
	if err := security.Load(filepath.Join(root, "domain.json"), &domain); err != nil {
		return nil, fmt.Errorf("load trust domain: %w", err)
	}
	if err := domain.Validate(config.IsAuthority); err != nil {
		return nil, err
	}
	objectStore, err := store.Open(root, domain)
	if err != nil {
		return nil, err
	}
	var localJournal *journal.Log
	if err := filelock.With(filepath.Join(root, "journal", "local.lock"), func() error {
		var openErr error
		localJournal, openErr = journal.Open(filepath.Join(root, "journal", "local.log"), identity.PublicKey, identity.Sign)
		return openErr
	}); err != nil {
		return nil, err
	}
	var authoritySign journal.SignFunc
	if config.IsAuthority {
		privateKey, err := domain.AuthorityPrivate()
		if err != nil {
			return nil, err
		}
		authoritySign = func(message string) (string, error) {
			return security.SignWithPrivate(privateKey, message), nil
		}
	}
	var authorityJournal *journal.Log
	if err := filelock.With(filepath.Join(root, "journal", "authority.lock"), func() error {
		var openErr error
		authorityJournal, openErr = journal.Open(
			filepath.Join(root, "journal", "authority.log"),
			domain.AuthorityPublicKey,
			authoritySign,
		)
		return openErr
	}); err != nil {
		return nil, err
	}
	result := &Node{
		root:             root,
		config:           config,
		identity:         identity,
		domain:           domain,
		store:            objectStore,
		localJournal:     localJournal,
		authorityJournal: authorityJournal,
		refs:             make(map[string]string),
		divergent:        make(map[string]string),
		revoked:          make(map[string]uint64),
	}
	if err := filelock.With(result.localLockPath(), func() error {
		if err := result.localJournal.Reload(); err != nil {
			return err
		}
		return result.recoverStagedReceipts()
	}); err != nil {
		return nil, err
	}
	result.rebuildAuthorityState()
	return result, nil
}

func (node *Node) Root() string {
	return node.root
}

func (node *Node) NodeID() string {
	id, err := node.identity.NodeID()
	if err != nil {
		panic(err)
	}
	return id
}

func (node *Node) PublicKey() string {
	return node.identity.PublicKey
}

func (node *Node) Domain() security.DomainConfig {
	return node.domain
}

func (node *Node) IsAuthority() bool {
	return node.config.IsAuthority
}

func (node *Node) PutObject(object model.GraphObject, private bool) (string, error) {
	return node.store.Put(object, private)
}

func (node *Node) ExpectedObjectID(object model.GraphObject, private bool) (string, error) {
	return node.store.ExpectedID(object, private)
}

func (node *Node) GetObject(id string) (model.GraphObject, error) {
	return node.store.Get(id)
}

func (node *Node) IssueCapability(subjectPublicKey, namespace string, operations []string, ttl time.Duration) (model.Capability, error) {
	if !node.config.IsAuthority {
		return model.Capability{}, errors.New("only an authority node can issue capabilities")
	}
	return security.IssueCapability(node.domain, subjectPublicKey, namespace, operations, ttl)
}

func (node *Node) CreateTransition(request TransitionRequest) (model.Transition, model.EdgeReceipt, error) {
	manifestPayload, err := canonical.Marshal(request.Roots)
	if err != nil {
		return model.Transition{}, model.EdgeReceipt{}, err
	}
	rootLinks := request.Roots.Values()
	sort.Strings(rootLinks)
	manifest := model.NewGraphObject(
		model.KindManifest,
		"application/vnd.fabric.required-objects+json",
		manifestPayload,
		rootLinks,
	)
	privateManifest := false
	for _, root := range rootLinks {
		privateManifest = privateManifest || node.store.IsPrivate(root)
	}
	manifestID, err := node.store.Put(manifest, privateManifest)
	if err != nil {
		return model.Transition{}, model.EdgeReceipt{}, err
	}

	parents := append([]string(nil), request.ParentTransitions...)
	sort.Strings(parents)
	expectedRefs := []model.ExpectedRef{{Name: request.RefName, TransitionID: request.Expected}}
	body := model.TransitionBody{
		ProtocolVersion:        model.ProtocolVersion,
		Namespace:              request.Namespace,
		TrustDomainID:          node.domain.ID,
		ParentTransitions:      parents,
		ExpectedRefs:           expectedRefs,
		Roots:                  request.Roots,
		RequiredObjectManifest: manifestID,
		RefIntents:             []model.RefIntent{{Name: request.RefName, Operation: "set"}},
		ActorPublicKey:         node.identity.PublicKey,
		Capability:             request.Capability,
		PolicyContext:          request.PolicyContext,
	}
	id, err := model.CanonicalID("txn:sha256:", body)
	if err != nil {
		return model.Transition{}, model.EdgeReceipt{}, err
	}
	signature, err := node.identity.Sign(id)
	if err != nil {
		return model.Transition{}, model.EdgeReceipt{}, err
	}
	transition := model.Transition{Body: body, ID: id, ActorSignature: signature}
	receipt, err := node.AcceptTransition(transition)
	return transition, receipt, err
}

func (node *Node) AcceptTransition(transition model.Transition) (model.EdgeReceipt, error) {
	return node.acceptTransition(transition, model.Capability{})
}

func (node *Node) AcceptTransitionFor(
	transition model.Transition,
	acceptanceCapability model.Capability,
) (model.EdgeReceipt, error) {
	return node.acceptTransition(transition, acceptanceCapability)
}

func (node *Node) acceptTransition(
	transition model.Transition,
	acceptanceCapability model.Capability,
) (model.EdgeReceipt, error) {
	node.mu.Lock()
	defer node.mu.Unlock()
	var receipt model.EdgeReceipt
	err := filelock.With(node.authorityLockPath(), func() error {
		if err := node.reloadAuthorityState(); err != nil {
			return err
		}
		return filelock.With(node.localLockPath(), func() error {
			if err := node.localJournal.Reload(); err != nil {
				return err
			}
			acceptedAt := time.Now().UTC()
			if err := node.verifyTransitionForAcceptance(transition, acceptedAt); err != nil {
				return err
			}
			if err := node.verifyAcceptanceCapability(transition, acceptanceCapability, acceptedAt); err != nil {
				return err
			}
			if err := node.saveTransition(transition); err != nil {
				return err
			}
			body := model.EdgeReceiptBody{
				ProtocolVersion:        model.ProtocolVersion,
				TransitionID:           transition.ID,
				AcceptedBy:             node.NodeID(),
				AcceptedAtUnix:         acceptedAt.Unix(),
				DurabilityClass:        node.config.DurabilityClass,
				ObjectsManifest:        transition.Body.RequiredObjectManifest,
				AuthorityCheckpoint:    node.authorityCheckpoint(),
				AcceptanceCapabilityID: acceptanceCapability.ID,
			}
			id, err := model.CanonicalID("receipt:sha256:", body)
			if err != nil {
				return err
			}
			signature, err := node.identity.Sign(id)
			if err != nil {
				return err
			}
			receipt = model.EdgeReceipt{
				Body:                 body,
				ID:                   id,
				NodePublicKey:        node.identity.PublicKey,
				NodeSignature:        signature,
				AcceptanceCapability: acceptanceCapability,
			}
			if err := node.stageReceipt(receipt); err != nil {
				return err
			}
			refName := transition.Body.RefIntents[0].Name
			if _, err = node.localJournal.Append(model.JournalRecordBody{
				Type:           "accepted",
				Namespace:      transition.Body.Namespace,
				RefName:        refName,
				TransitionID:   transition.ID,
				ReceiptID:      receipt.ID,
				Expected:       transition.ExpectedRef(refName),
				Result:         node.config.DurabilityClass,
				RecordedAtUnix: time.Now().UTC().Unix(),
			}); err != nil {
				return err
			}
			return node.publishStagedReceipt(receipt.ID)
		})
	})
	return receipt, err
}

func (node *Node) IngestTransition(transition model.Transition, receipt model.EdgeReceipt) error {
	node.mu.Lock()
	defer node.mu.Unlock()
	return filelock.With(node.authorityLockPath(), func() error {
		if err := node.reloadAuthorityState(); err != nil {
			return err
		}
		if err := node.verifyTransitionIntegrity(transition); err != nil {
			return err
		}
		if err := node.verifyReceipt(receipt, transition); err != nil {
			return err
		}
		if err := node.verifyAuthorizationAtReceipt(transition, receipt); err != nil {
			return err
		}
		if err := node.saveTransition(transition); err != nil {
			return err
		}
		return node.saveReceipt(receipt)
	})
}

func (node *Node) Finalize(transitionID string) (model.FinalizeResult, error) {
	node.mu.Lock()
	defer node.mu.Unlock()
	if !node.config.IsAuthority {
		return model.FinalizeResult{}, errors.New("only an authority node can finalize refs")
	}
	var result model.FinalizeResult
	err := filelock.With(node.authorityLockPath(), func() error {
		if err := node.reloadAuthorityState(); err != nil {
			return err
		}
		transition, err := node.loadTransition(transitionID)
		if err != nil {
			return err
		}
		if err := node.verifyTransitionIntegrity(transition); err != nil {
			return err
		}
		receipt, err := node.findValidReceipt(transition)
		if err != nil {
			return err
		}
		if err := node.verifyAuthorizationAtReceipt(transition, receipt); err != nil {
			return err
		}
		intent := transition.Body.RefIntents[0]
		if existing, found := node.existingFinalizeResult(transition.Body.Namespace, intent.Name, transition.ID); found {
			result = existing
			return nil
		}
		key := refKey(transition.Body.Namespace, intent.Name)
		current := node.refs[key]
		expected := transition.ExpectedRef(intent.Name)
		result = model.FinalizeResult{
			RefName:      intent.Name,
			TransitionID: transition.ID,
			Current:      current,
		}
		recordBody := model.JournalRecordBody{
			Namespace:      transition.Body.Namespace,
			RefName:        intent.Name,
			TransitionID:   transition.ID,
			ReceiptID:      receipt.ID,
			Expected:       expected,
			RecordedAtUnix: time.Now().UTC().Unix(),
		}
		if current == expected {
			result.Status = "finalized"
			result.Current = transition.ID
			recordBody.Type = "finalized"
			recordBody.Result = transition.ID
		} else {
			result.Status = "divergent"
			result.DivergentRef = divergentRef(intent.Name, transition.ID)
			recordBody.Type = "divergent"
			recordBody.Result = result.DivergentRef
		}
		record, err := node.authorityJournal.Append(recordBody)
		if err != nil {
			return err
		}
		result.JournalRecord = record.ID
		node.applyAuthorityRecord(record)
		return nil
	})
	return result, err
}

func (node *Node) RevokeCapability(capabilityID string) error {
	node.mu.Lock()
	defer node.mu.Unlock()
	if !node.config.IsAuthority {
		return errors.New("only an authority node can revoke capabilities")
	}
	return filelock.With(node.authorityLockPath(), func() error {
		if err := node.reloadAuthorityState(); err != nil {
			return err
		}
		if _, alreadyRevoked := node.revoked[capabilityID]; alreadyRevoked {
			return nil
		}
		record, err := node.authorityJournal.Append(model.JournalRecordBody{
			Type:           "capability_revoked",
			Result:         capabilityID,
			RecordedAtUnix: time.Now().UTC().Unix(),
		})
		if err != nil {
			return err
		}
		node.applyAuthorityRecord(record)
		return nil
	})
}

func (node *Node) AuthorityRecords() ([]model.JournalRecord, error) {
	node.mu.Lock()
	defer node.mu.Unlock()
	var records []model.JournalRecord
	err := filelock.With(node.authorityLockPath(), func() error {
		if err := node.reloadAuthorityState(); err != nil {
			return err
		}
		records = node.authorityJournal.Records()
		return nil
	})
	return records, err
}

func (node *Node) ImportAuthorityRecords(records []model.JournalRecord) error {
	node.mu.Lock()
	defer node.mu.Unlock()
	return filelock.With(node.authorityLockPath(), func() error {
		if err := node.authorityJournal.Reload(); err != nil {
			return err
		}
		if err := node.authorityJournal.Import(records); err != nil {
			return err
		}
		node.rebuildAuthorityState()
		return nil
	})
}

func (node *Node) Refs() (map[string]string, map[string]string, error) {
	node.mu.Lock()
	defer node.mu.Unlock()
	var refs map[string]string
	var divergent map[string]string
	err := filelock.With(node.authorityLockPath(), func() error {
		if err := node.reloadAuthorityState(); err != nil {
			return err
		}
		refs = make(map[string]string, len(node.refs))
		for key, value := range node.refs {
			refs[key] = value
		}
		divergent = make(map[string]string, len(node.divergent))
		for key, value := range node.divergent {
			divergent[key] = value
		}
		return nil
	})
	return refs, divergent, err
}

func (node *Node) LoadTransition(id string) (model.Transition, error) {
	node.mu.Lock()
	defer node.mu.Unlock()
	return node.loadTransition(id)
}

func (node *Node) LoadReceipt(id string) (model.EdgeReceipt, error) {
	node.mu.Lock()
	defer node.mu.Unlock()
	var receipt model.EdgeReceipt
	if err := security.Load(node.receiptPath(id), &receipt); err != nil {
		return model.EdgeReceipt{}, err
	}
	return receipt, nil
}

func (node *Node) ReceiptForTransition(transitionID string) (model.EdgeReceipt, error) {
	node.mu.Lock()
	defer node.mu.Unlock()
	var receipt model.EdgeReceipt
	err := filelock.With(node.authorityLockPath(), func() error {
		if err := node.reloadAuthorityState(); err != nil {
			return err
		}
		transition, err := node.loadTransition(transitionID)
		if err != nil {
			return err
		}
		receipt, err = node.findValidReceipt(transition)
		return err
	})
	return receipt, err
}

func (node *Node) VerifyObjectClosure(root string) ([]string, error) {
	return node.store.VerifyClosure([]string{root})
}

func (node *Node) OperationalStats() (OperationalStats, error) {
	objectStats, err := node.store.Stats()
	if err != nil {
		return OperationalStats{}, err
	}
	transitions, err := countJSONFiles(filepath.Join(node.root, "transitions"))
	if err != nil {
		return OperationalStats{}, err
	}
	receipts, err := countJSONFiles(filepath.Join(node.root, "receipts"))
	if err != nil {
		return OperationalStats{}, err
	}
	records, err := node.AuthorityRecords()
	if err != nil {
		return OperationalStats{}, err
	}
	refs, divergent, err := node.Refs()
	if err != nil {
		return OperationalStats{}, err
	}
	return OperationalStats{
		NodeID:           node.NodeID(),
		Authority:        node.IsAuthority(),
		Objects:          objectStats,
		Transitions:      transitions,
		Receipts:         receipts,
		AuthorityRecords: len(records),
		SharedRefs:       len(refs),
		DivergentRefs:    len(divergent),
	}, nil
}

func (node *Node) Audit() (AuditReport, error) {
	node.mu.Lock()
	defer node.mu.Unlock()
	var report AuditReport
	err := filelock.With(node.authorityLockPath(), func() error {
		if err := node.reloadAuthorityState(); err != nil {
			return err
		}
		objects, err := node.store.Objects()
		if err != nil {
			return err
		}
		for _, object := range objects {
			if _, err := node.store.Get(object.ID); err != nil {
				return fmt.Errorf("verify object %s: %w", object.ID, err)
			}
		}
		report.Objects = len(objects)
		entries, err := os.ReadDir(filepath.Join(node.root, "transitions"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			var transition model.Transition
			if err := security.Load(filepath.Join(node.root, "transitions", entry.Name()), &transition); err != nil {
				return err
			}
			if err := node.verifyTransitionIntegrity(transition); err != nil {
				return fmt.Errorf("verify transition %s: %w", transition.ID, err)
			}
			report.Transitions++
		}
		receiptEntries, err := os.ReadDir(filepath.Join(node.root, "receipts"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, entry := range receiptEntries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			var receipt model.EdgeReceipt
			if err := security.Load(filepath.Join(node.root, "receipts", entry.Name()), &receipt); err != nil {
				return err
			}
			transition, err := node.loadTransition(receipt.Body.TransitionID)
			if err != nil {
				return err
			}
			if err := node.verifyReceipt(receipt, transition); err != nil {
				return fmt.Errorf("verify receipt %s: %w", receipt.ID, err)
			}
			if err := node.verifyAuthorizationAtReceipt(transition, receipt); err != nil {
				return fmt.Errorf("verify receipt authorization %s: %w", receipt.ID, err)
			}
			report.Receipts++
		}
		if err := filelock.With(node.localLockPath(), func() error {
			if err := node.localJournal.Reload(); err != nil {
				return err
			}
			for _, record := range node.localJournal.Records() {
				if record.Body.Type != "accepted" {
					continue
				}
				transition, err := node.loadTransition(record.Body.TransitionID)
				if err != nil {
					return fmt.Errorf("accepted journal record %s lost transition: %w", record.ID, err)
				}
				if transition.ID != record.Body.TransitionID {
					return fmt.Errorf("accepted journal record %s transition ID mismatch", record.ID)
				}
				var receipt model.EdgeReceipt
				if err := security.Load(node.receiptPath(record.Body.ReceiptID), &receipt); err != nil {
					return fmt.Errorf("accepted journal record %s lost receipt: %w", record.ID, err)
				}
				if receipt.ID != record.Body.ReceiptID {
					return fmt.Errorf("accepted journal record %s receipt ID mismatch", record.ID)
				}
				if err := node.verifyReceipt(receipt, transition); err != nil {
					return fmt.Errorf("accepted journal receipt %s: %w", receipt.ID, err)
				}
				if err := node.verifyAuthorizationAtReceipt(transition, receipt); err != nil {
					return fmt.Errorf("accepted journal authorization %s: %w", receipt.ID, err)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if node.config.IsAuthority {
			for _, record := range node.authorityJournal.Records() {
				if record.Body.Type != "finalized" && record.Body.Type != "divergent" {
					continue
				}
				transition, err := node.loadTransition(record.Body.TransitionID)
				if err != nil {
					return fmt.Errorf("authority record %s lost transition: %w", record.ID, err)
				}
				if transition.ID != record.Body.TransitionID {
					return fmt.Errorf("authority record %s transition ID mismatch", record.ID)
				}
				if err := node.verifyTransitionIntegrity(transition); err != nil {
					return fmt.Errorf("authority transition %s: %w", transition.ID, err)
				}
				var receipt model.EdgeReceipt
				if err := security.Load(node.receiptPath(record.Body.ReceiptID), &receipt); err != nil {
					return fmt.Errorf("authority record %s lost receipt: %w", record.ID, err)
				}
				if receipt.ID != record.Body.ReceiptID {
					return fmt.Errorf("authority record %s receipt ID mismatch", record.ID)
				}
				if err := node.verifyReceipt(receipt, transition); err != nil {
					return fmt.Errorf("authority record %s receipt: %w", record.ID, err)
				}
				if err := node.verifyAuthorizationAtReceipt(transition, receipt); err != nil {
					return fmt.Errorf("authority receipt authorization %s: %w", receipt.ID, err)
				}
			}
		}
		return nil
	})
	return report, err
}

func (node *Node) GarbageCollect(grace time.Duration, dryRun bool) (GCReport, error) {
	if grace < 0 {
		return GCReport{}, errors.New("garbage-collection grace period cannot be negative")
	}
	node.mu.Lock()
	defer node.mu.Unlock()
	report := GCReport{
		DryRun:       dryRun,
		GraceSeconds: int64(grace.Seconds()),
	}
	err := filelock.With(filepath.Join(node.root, "objects", "gc.lock"), func() error {
		reachable := make(map[string]struct{})
		entries, err := os.ReadDir(filepath.Join(node.root, "transitions"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			var transition model.Transition
			if err := security.Load(filepath.Join(node.root, "transitions", entry.Name()), &transition); err != nil {
				return err
			}
			closure, err := node.store.VerifyClosure([]string{transition.Body.RequiredObjectManifest})
			if err != nil {
				return err
			}
			for _, id := range closure {
				reachable[id] = struct{}{}
			}
		}
		report.Reachable = len(reachable)
		objects, err := node.store.Objects()
		if err != nil {
			return err
		}
		cutoff := time.Now().UTC().Add(-grace)
		for _, object := range objects {
			if _, ok := reachable[object.ID]; ok || object.Modified.After(cutoff) {
				continue
			}
			report.Candidates++
			report.CandidateBytes += object.Size
			if len(report.CandidateObject) < 100 {
				report.CandidateObject = append(report.CandidateObject, object.ID)
			}
			if dryRun {
				continue
			}
			if err := node.store.Delete(object.ID); err != nil {
				return err
			}
			report.Deleted++
			report.DeletedBytes += object.Size
		}
		return nil
	})
	return report, err
}

func (node *Node) verifyTransitionForAcceptance(transition model.Transition, acceptedAt time.Time) error {
	if err := node.verifyTransitionIntegrity(transition); err != nil {
		return err
	}
	if _, revoked := node.revoked[transition.Body.Capability.ID]; revoked {
		return errors.New("capability has been revoked")
	}
	return security.VerifyCapability(
		node.domain,
		transition.Body.Capability,
		transition.Body.ActorPublicKey,
		transition.Body.Namespace,
		"transition.accept",
		acceptedAt,
	)
}

func (node *Node) verifyTransitionIntegrity(transition model.Transition) error {
	if err := transition.ValidateShape(); err != nil {
		return err
	}
	if transition.Body.TrustDomainID != node.domain.ID {
		return errors.New("transition belongs to another trust domain")
	}
	expectedID, err := model.CanonicalID("txn:sha256:", transition.Body)
	if err != nil {
		return err
	}
	if expectedID != transition.ID {
		return errors.New("transition ID mismatch")
	}
	if err := security.VerifySignature(transition.Body.ActorPublicKey, transition.ID, transition.ActorSignature); err != nil {
		return fmt.Errorf("actor signature: %w", err)
	}
	if err := security.VerifyCapabilityScope(
		node.domain,
		transition.Body.Capability,
		transition.Body.ActorPublicKey,
		transition.Body.Namespace,
		"transition.accept",
	); err != nil {
		return err
	}
	if err := node.verifyParents(transition); err != nil {
		return err
	}
	if err := node.verifyRootKind(transition.Body.Roots.Source, model.KindSource); err != nil {
		return fmt.Errorf("source root: %w", err)
	}
	if err := node.verifyRootKind(transition.Body.Roots.Workspace, model.KindWorkspace); err != nil {
		return fmt.Errorf("workspace root: %w", err)
	}
	if err := node.verifyRootKind(transition.Body.Roots.Provenance, model.KindProvenance); err != nil {
		return fmt.Errorf("provenance root: %w", err)
	}
	manifest, err := node.store.Get(transition.Body.RequiredObjectManifest)
	if err != nil {
		return fmt.Errorf("required object manifest: %w", err)
	}
	if manifest.Kind != model.KindManifest {
		return errors.New("required object manifest has the wrong graph kind")
	}
	expectedLinks := transition.Body.Roots.Values()
	sort.Strings(expectedLinks)
	if strings.Join(manifest.Links, "\x00") != strings.Join(expectedLinks, "\x00") {
		return errors.New("required object manifest does not bind all graph roots")
	}
	if _, err := node.store.VerifyClosure([]string{transition.Body.RequiredObjectManifest}); err != nil {
		return err
	}
	return nil
}

func (node *Node) verifyReceipt(receipt model.EdgeReceipt, transition model.Transition) error {
	if receipt.Body.ProtocolVersion != model.ProtocolVersion {
		return errors.New("unsupported receipt version")
	}
	if receipt.Body.TransitionID != transition.ID ||
		receipt.Body.ObjectsManifest != transition.Body.RequiredObjectManifest ||
		receipt.Body.DurabilityClass != "host-disk" {
		return errors.New("receipt does not bind the transition")
	}
	if receipt.NodePublicKey == transition.Body.ActorPublicKey {
		if receipt.Body.AcceptanceCapabilityID != "" || receipt.AcceptanceCapability.ID != "" {
			return errors.New("self-accepted receipt must not carry an edge capability")
		}
	} else {
		if receipt.Body.AcceptanceCapabilityID == "" ||
			receipt.Body.AcceptanceCapabilityID != receipt.AcceptanceCapability.ID {
			return errors.New("independent receipt does not bind an acceptance capability")
		}
		acceptedAt := time.Unix(receipt.Body.AcceptedAtUnix, 0).UTC()
		if err := security.VerifyCapability(
			node.domain,
			receipt.AcceptanceCapability,
			receipt.NodePublicKey,
			transition.Body.Namespace,
			"receipt.issue",
			acceptedAt,
		); err != nil {
			return fmt.Errorf("acceptance capability: %w", err)
		}
	}
	expectedNodeID, err := security.NodeIDForPublicKey(receipt.NodePublicKey)
	if err != nil {
		return err
	}
	if expectedNodeID != receipt.Body.AcceptedBy {
		return errors.New("receipt node ID mismatch")
	}
	if _, err := node.authorityCheckpointSequence(receipt.Body.AuthorityCheckpoint); err != nil {
		return err
	}
	expectedID, err := model.CanonicalID("receipt:sha256:", receipt.Body)
	if err != nil {
		return err
	}
	if expectedID != receipt.ID {
		return errors.New("receipt ID mismatch")
	}
	return security.VerifySignature(receipt.NodePublicKey, receipt.ID, receipt.NodeSignature)
}

func (node *Node) verifyAuthorizationAtReceipt(transition model.Transition, receipt model.EdgeReceipt) error {
	acceptedAt := time.Unix(receipt.Body.AcceptedAtUnix, 0).UTC()
	if err := security.VerifyCapability(
		node.domain,
		transition.Body.Capability,
		transition.Body.ActorPublicKey,
		transition.Body.Namespace,
		"transition.accept",
		acceptedAt,
	); err != nil {
		return err
	}
	if revokedSequence, revoked := node.revoked[transition.Body.Capability.ID]; revoked {
		checkpointSequence, err := node.authorityCheckpointSequence(receipt.Body.AuthorityCheckpoint)
		if err != nil {
			return err
		}
		if checkpointSequence >= revokedSequence {
			return errors.New("accepting edge had observed capability revocation")
		}
	}
	if receipt.AcceptanceCapability.ID != "" {
		if revokedSequence, revoked := node.revoked[receipt.AcceptanceCapability.ID]; revoked {
			checkpointSequence, err := node.authorityCheckpointSequence(receipt.Body.AuthorityCheckpoint)
			if err != nil {
				return err
			}
			if checkpointSequence >= revokedSequence {
				return errors.New("accepting edge had observed acceptance-capability revocation")
			}
		}
	}
	return nil
}

func (node *Node) verifyAcceptanceCapability(
	transition model.Transition,
	capability model.Capability,
	acceptedAt time.Time,
) error {
	if node.identity.PublicKey == transition.Body.ActorPublicKey {
		if capability.ID != "" {
			return errors.New("self-acceptance does not use a separate edge capability")
		}
		return nil
	}
	if capability.ID == "" {
		return errors.New("independent edge acceptance requires a receipt.issue capability")
	}
	if _, revoked := node.revoked[capability.ID]; revoked {
		return errors.New("acceptance capability has been revoked")
	}
	return security.VerifyCapability(
		node.domain,
		capability,
		node.identity.PublicKey,
		transition.Body.Namespace,
		"receipt.issue",
		acceptedAt,
	)
}

func (node *Node) verifyParents(transition model.Transition) error {
	for _, parentID := range transition.Body.ParentTransitions {
		parent, err := node.loadTransition(parentID)
		if err != nil {
			return fmt.Errorf("missing parent transition %s: %w", parentID, err)
		}
		expectedID, err := model.CanonicalID("txn:sha256:", parent.Body)
		if err != nil {
			return err
		}
		if expectedID != parent.ID || parent.ID != parentID {
			return fmt.Errorf("parent transition %s failed identity verification", parentID)
		}
		if parent.Body.Namespace != transition.Body.Namespace || parent.Body.TrustDomainID != transition.Body.TrustDomainID {
			return fmt.Errorf("parent transition %s crosses namespace or trust domain", parentID)
		}
		if err := security.VerifySignature(parent.Body.ActorPublicKey, parent.ID, parent.ActorSignature); err != nil {
			return fmt.Errorf("parent transition %s signature: %w", parentID, err)
		}
	}
	return nil
}

func (node *Node) verifyRootKind(id string, want model.GraphKind) error {
	object, err := node.store.Get(id)
	if err != nil {
		return err
	}
	if object.Kind != want {
		return fmt.Errorf("got graph kind %q, want %q", object.Kind, want)
	}
	return nil
}

func (node *Node) findValidReceipt(transition model.Transition) (model.EdgeReceipt, error) {
	entries, err := os.ReadDir(filepath.Join(node.root, "receipts"))
	if err != nil {
		return model.EdgeReceipt{}, errors.New("transition has no durability receipt")
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var receipt model.EdgeReceipt
		if err := security.Load(filepath.Join(node.root, "receipts", entry.Name()), &receipt); err != nil {
			continue
		}
		if receipt.Body.TransitionID == transition.ID && node.verifyReceipt(receipt, transition) == nil {
			return receipt, nil
		}
	}
	return model.EdgeReceipt{}, errors.New("transition has no valid durability receipt")
}

func (node *Node) saveTransition(transition model.Transition) error {
	path := node.transitionPath(transition.ID)
	if _, err := os.Stat(path); err == nil {
		existing, loadErr := node.loadTransition(transition.ID)
		if loadErr != nil {
			return loadErr
		}
		existingBytes, _ := canonical.Marshal(existing)
		incomingBytes, _ := canonical.Marshal(transition)
		if string(existingBytes) != string(incomingBytes) {
			return errors.New("immutable transition collision")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return security.Save(path, transition, 0o600)
}

func (node *Node) saveReceipt(receipt model.EdgeReceipt) error {
	path := node.receiptPath(receipt.ID)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return security.Save(path, receipt, 0o600)
}

func (node *Node) stageReceipt(receipt model.EdgeReceipt) error {
	return security.Save(node.stagedReceiptPath(receipt.ID), receipt, 0o600)
}

func (node *Node) publishStagedReceipt(receiptID string) error {
	stagedPath := node.stagedReceiptPath(receiptID)
	finalPath := node.receiptPath(receiptID)
	if _, err := os.Stat(finalPath); err == nil {
		_ = os.Remove(stagedPath)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
		return err
	}
	if err := os.Rename(stagedPath, finalPath); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(finalPath)); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(stagedPath))
}

func (node *Node) recoverStagedReceipts() error {
	stagedDirectory := filepath.Join(node.root, "receipts", "staged")
	entries, err := os.ReadDir(stagedDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	accepted := make(map[string]struct{})
	for _, record := range node.localJournal.Records() {
		if record.Body.Type == "accepted" {
			accepted[record.Body.ReceiptID] = struct{}{}
		}
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(stagedDirectory, entry.Name())
		var receipt model.EdgeReceipt
		if err := security.Load(path, &receipt); err != nil {
			return err
		}
		if _, ok := accepted[receipt.ID]; ok {
			if err := node.publishStagedReceipt(receipt.ID); err != nil {
				return err
			}
		} else if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func (node *Node) loadTransition(id string) (model.Transition, error) {
	var transition model.Transition
	if err := security.Load(node.transitionPath(id), &transition); err != nil {
		return model.Transition{}, err
	}
	return transition, nil
}

func (node *Node) transitionPath(id string) string {
	return filepath.Join(node.root, "transitions", safeID(id)+".json")
}

func (node *Node) receiptPath(id string) string {
	return filepath.Join(node.root, "receipts", safeID(id)+".json")
}

func (node *Node) stagedReceiptPath(id string) string {
	return filepath.Join(node.root, "receipts", "staged", safeID(id)+".json")
}

func (node *Node) rebuildAuthorityState() {
	node.refs = make(map[string]string)
	node.divergent = make(map[string]string)
	node.revoked = make(map[string]uint64)
	for _, record := range node.authorityJournal.Records() {
		node.applyAuthorityRecord(record)
	}
}

func (node *Node) applyAuthorityRecord(record model.JournalRecord) {
	switch record.Body.Type {
	case "finalized":
		node.refs[refKey(record.Body.Namespace, record.Body.RefName)] = record.Body.TransitionID
	case "divergent":
		node.divergent[record.Body.Result] = record.Body.TransitionID
	case "capability_revoked":
		if sequence, exists := node.revoked[record.Body.Result]; !exists || record.Body.Sequence < sequence {
			node.revoked[record.Body.Result] = record.Body.Sequence
		}
	}
}

func (node *Node) reloadAuthorityState() error {
	if err := node.authorityJournal.Reload(); err != nil {
		return err
	}
	node.rebuildAuthorityState()
	return nil
}

func (node *Node) existingFinalizeResult(namespace, refName, transitionID string) (model.FinalizeResult, bool) {
	for _, record := range node.authorityJournal.Records() {
		if record.Body.Namespace != namespace || record.Body.RefName != refName || record.Body.TransitionID != transitionID {
			continue
		}
		switch record.Body.Type {
		case "finalized":
			return model.FinalizeResult{
				Status:        "finalized",
				RefName:       refName,
				TransitionID:  transitionID,
				Current:       transitionID,
				JournalRecord: record.ID,
			}, true
		case "divergent":
			return model.FinalizeResult{
				Status:        "divergent",
				RefName:       refName,
				TransitionID:  transitionID,
				Current:       node.refs[refKey(namespace, refName)],
				DivergentRef:  record.Body.Result,
				JournalRecord: record.ID,
			}, true
		}
	}
	return model.FinalizeResult{}, false
}

func (node *Node) authorityCheckpoint() string {
	records := node.authorityJournal.Records()
	if len(records) == 0 {
		return ""
	}
	return records[len(records)-1].ID
}

func (node *Node) authorityCheckpointSequence(checkpoint string) (uint64, error) {
	if checkpoint == "" {
		return 0, nil
	}
	for _, record := range node.authorityJournal.Records() {
		if record.ID == checkpoint {
			return record.Body.Sequence, nil
		}
	}
	return 0, errors.New("receipt references an unknown authority checkpoint")
}

func (node *Node) localLockPath() string {
	return filepath.Join(node.root, "journal", "local.lock")
}

func countJSONFiles(directory string) (int, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			count++
		}
	}
	return count, nil
}

func (node *Node) authorityLockPath() string {
	return filepath.Join(node.root, "journal", "authority.lock")
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}

func refKey(namespace, refName string) string {
	return namespace + ":" + refName
}

func divergentRef(refName, transitionID string) string {
	short := strings.TrimPrefix(transitionID, "txn:sha256:")
	if len(short) > 16 {
		short = short[:16]
	}
	return "refs/divergent/" + strings.TrimPrefix(refName, "refs/") + "/" + short
}

func safeID(id string) string {
	return strings.NewReplacer(":", "_", "/", "_").Replace(id)
}
