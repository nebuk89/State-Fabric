package journal

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/nebuk89/cdn_git/internal/canonical"
	"github.com/nebuk89/cdn_git/internal/model"
	"github.com/nebuk89/cdn_git/internal/security"
)

type SignFunc func(message string) (string, error)

type Log struct {
	mu           sync.Mutex
	path         string
	signerPublic string
	sign         SignFunc
	records      []model.JournalRecord
}

type persistedHead struct {
	Sequence uint64 `json:"sequence"`
	RecordID string `json:"record_id"`
}

func Open(path, signerPublic string, sign SignFunc) (*Log, error) {
	log := &Log{
		path:         path,
		signerPublic: signerPublic,
		sign:         sign,
	}
	records, err := readRecords(path)
	if err != nil {
		return nil, err
	}
	if err := verifyRecords(records, signerPublic); err != nil {
		return nil, fmt.Errorf("verify journal %s: %w", path, err)
	}
	if err := verifyPersistedHead(path, records); err != nil {
		return nil, fmt.Errorf("verify journal head %s: %w", path, err)
	}
	if err := persistHead(path, records); err != nil {
		return nil, err
	}
	log.records = records
	return log, nil
}

func (log *Log) Append(body model.JournalRecordBody) (model.JournalRecord, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.sign == nil {
		return model.JournalRecord{}, errors.New("journal is read-only")
	}

	body.ProtocolVersion = model.ProtocolVersion
	body.Sequence = uint64(len(log.records) + 1)
	if len(log.records) > 0 {
		body.PreviousRecord = log.records[len(log.records)-1].ID
	} else {
		body.PreviousRecord = ""
	}
	id, err := model.CanonicalID("journal:sha256:", body)
	if err != nil {
		return model.JournalRecord{}, err
	}
	signature, err := log.sign(id)
	if err != nil {
		return model.JournalRecord{}, err
	}
	record := model.JournalRecord{
		Body:            body,
		ID:              id,
		SignerPublicKey: log.signerPublic,
		Signature:       signature,
	}
	if err := appendRecord(log.path, record); err != nil {
		return model.JournalRecord{}, err
	}
	log.records = append(log.records, record)
	if err := persistHead(log.path, log.records); err != nil {
		return model.JournalRecord{}, err
	}
	return record, nil
}

func (log *Log) Records() []model.JournalRecord {
	log.mu.Lock()
	defer log.mu.Unlock()
	return append([]model.JournalRecord(nil), log.records...)
}

func (log *Log) Reload() error {
	log.mu.Lock()
	defer log.mu.Unlock()
	records, err := readRecords(log.path)
	if err != nil {
		return err
	}
	if err := verifyRecords(records, log.signerPublic); err != nil {
		return err
	}
	if err := verifyExtension(log.records, records); err != nil {
		return err
	}
	if err := verifyPersistedHead(log.path, records); err != nil {
		return err
	}
	if err := persistHead(log.path, records); err != nil {
		return err
	}
	log.records = records
	return nil
}

func (log *Log) Import(records []model.JournalRecord) error {
	log.mu.Lock()
	defer log.mu.Unlock()
	if err := validateImport(log.records, records, log.signerPublic); err != nil {
		return err
	}
	if len(records) == len(log.records) {
		return nil
	}
	if err := replaceRecords(log.path, records); err != nil {
		return err
	}
	if err := persistHead(log.path, records); err != nil {
		return err
	}
	log.records = append([]model.JournalRecord(nil), records...)
	return nil
}

func (log *Log) ValidateImport(records []model.JournalRecord) error {
	log.mu.Lock()
	defer log.mu.Unlock()
	return validateImport(log.records, records, log.signerPublic)
}

func validateImport(current, incoming []model.JournalRecord, signerPublic string) error {
	if err := verifyRecords(incoming, signerPublic); err != nil {
		return err
	}
	if len(incoming) < len(current) {
		return errors.New("imported journal is behind local journal")
	}
	for index := range current {
		if subtle.ConstantTimeCompare([]byte(current[index].ID), []byte(incoming[index].ID)) != 1 {
			return errors.New("journal history fork detected")
		}
	}
	return nil
}

func verifyExtension(expected, records []model.JournalRecord) error {
	if len(records) < len(expected) {
		return errors.New("journal truncation detected")
	}
	for index := range expected {
		if subtle.ConstantTimeCompare([]byte(expected[index].ID), []byte(records[index].ID)) != 1 {
			return errors.New("journal history fork detected")
		}
	}
	return nil
}

func verifyPersistedHead(path string, records []model.JournalRecord) error {
	var head persistedHead
	if err := security.Load(headPath(path), &head); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if head.Sequence == 0 || head.RecordID == "" {
		return errors.New("invalid persisted journal head")
	}
	if uint64(len(records)) < head.Sequence {
		return errors.New("journal truncation detected")
	}
	if records[head.Sequence-1].ID != head.RecordID {
		return errors.New("journal history fork detected")
	}
	return nil
}

func persistHead(path string, records []model.JournalRecord) error {
	if len(records) == 0 {
		return nil
	}
	latest := records[len(records)-1]
	return security.Save(headPath(path), persistedHead{
		Sequence: latest.Body.Sequence,
		RecordID: latest.ID,
	}, 0o600)
}

func headPath(path string) string {
	return path + ".head"
}

func verifyRecords(records []model.JournalRecord, signerPublic string) error {
	previous := ""
	for index, record := range records {
		if record.Body.ProtocolVersion != model.ProtocolVersion {
			return fmt.Errorf("record %d has unsupported version", index)
		}
		if record.Body.Sequence != uint64(index+1) {
			return fmt.Errorf("record %d has invalid sequence", index)
		}
		if record.Body.PreviousRecord != previous {
			return fmt.Errorf("record %d breaks hash chain", index)
		}
		if record.SignerPublicKey != signerPublic {
			return fmt.Errorf("record %d has an untrusted signer", index)
		}
		expectedID, err := model.CanonicalID("journal:sha256:", record.Body)
		if err != nil {
			return err
		}
		if record.ID != expectedID {
			return fmt.Errorf("record %d ID mismatch", index)
		}
		if err := security.VerifySignature(record.SignerPublicKey, record.ID, record.Signature); err != nil {
			return fmt.Errorf("record %d signature: %w", index, err)
		}
		previous = record.ID
	}
	return nil
}

func readRecords(path string) ([]model.JournalRecord, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var records []model.JournalRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var record model.JournalRecord
		if err := canonical.Decode(line, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func appendRecord(path string, record model.JournalRecord) error {
	data, err := canonical.Marshal(record)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
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
	return syncDirectory(directory)
}

func replaceRecords(path string, records []model.JournalRecord) error {
	var data bytes.Buffer
	for _, record := range records {
		encoded, err := canonical.Marshal(record)
		if err != nil {
			return err
		}
		data.Write(encoded)
		data.WriteByte('\n')
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".journal-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data.Bytes()); err != nil {
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
