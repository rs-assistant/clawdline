package healthsealed

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Bounds, registered in internal/domain/capacity.
const (
	// MaxPendingAttachments is how many attachments the machine keeps a
	// local record of at once. At the bound a new attachment is refused
	// (CodeStoreFull) until an expired one is pruned.
	MaxPendingAttachments = 16
	// MaxStoreBytes is every attachment record together.
	MaxStoreBytes = 4 << 20
)

// RevisionState is where one revision's exact body stands.
type RevisionState string

const (
	RevisionPending RevisionState = "pending"
	RevisionStored  RevisionState = "stored"
	RevisionRefused RevisionState = "refused"
)

// Revision is one immutable revision: its exact request body is written
// before it is first sent, so every retry, and every retry after a crash,
// sends the same bytes under the same nonce.
type Revision struct {
	Revision         int64           `json:"revision"`
	Nonce            string          `json:"nonce"`
	CiphertextSHA256 string          `json:"ciphertext_sha256"`
	AADSHA256        string          `json:"aad_sha256"`
	Body             json.RawMessage `json:"body"`
	State            RevisionState   `json:"state"`
	Refusal          string          `json:"refusal,omitempty"`
}

// Attachment is the local record of one sealed attachment.
type Attachment struct {
	ID              string     `json:"id"`
	Tenant          string     `json:"tenant"`
	MachineID       string     `json:"machine_id"`
	BootID          string     `json:"boot_id"`
	SubjectKind     string     `json:"subject_kind"`
	SubjectID       string     `json:"subject_id"`
	ContentKeyEpoch int64      `json:"content_key_epoch"`
	IdentityEpoch   int64      `json:"identity_epoch"`
	OpenedAtMS      int64      `json:"opened_at_ms"`
	ExpiresAtMS     int64      `json:"expires_at_ms"`
	Revisions       []Revision `json:"revisions"`
}

// Store keeps attachment records under one directory, one file each,
// written atomically. It never blocks on anything but its own files.
type Store struct {
	dir string
	mu  sync.Mutex
}

// OpenStore uses dir, creating it owner-only.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fail(CodeStoreUnwritable)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".json") }

// Load reads one attachment record.
func (s *Store) Load(id string) (Attachment, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(id)
}

func (s *Store) load(id string) (Attachment, bool, error) {
	raw, err := os.ReadFile(s.path(id))
	if errors.Is(err, fs.ErrNotExist) {
		return Attachment{}, false, nil
	}
	if err != nil {
		return Attachment{}, false, fail(CodeStoreUnreadable)
	}
	var a Attachment
	if err := json.Unmarshal(raw, &a); err != nil || a.ID != id {
		return Attachment{}, false, fail(CodeStoreUnreadable)
	}
	return a, true, nil
}

// IDs lists the attachments on disk.
func (s *Store) IDs() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids, _, err := s.scan()
	return ids, err
}

func (s *Store) scan() ([]string, map[string]int64, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, nil, fail(CodeStoreUnreadable)
	}
	var ids []string
	sizes := map[string]int64{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "hs_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, nil, fail(CodeStoreUnreadable)
		}
		id := strings.TrimSuffix(name, ".json")
		ids = append(ids, id)
		sizes[id] = info.Size()
	}
	return ids, sizes, nil
}

// Put writes a record atomically, or refuses it at once when the store is
// at its bounds.
func (s *Store) Put(a Attachment) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return fail(CodeStoreUnwritable)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, sizes, err := s.scan()
	if err != nil {
		return err
	}
	_, exists := sizes[a.ID]
	if !exists && len(sizes) >= MaxPendingAttachments {
		return fail(CodeStoreFull)
	}
	var total int64
	for id, n := range sizes {
		if id != a.ID {
			total += n
		}
	}
	if total+int64(len(raw)) > MaxStoreBytes {
		return fail(CodeStoreFull)
	}
	return writeAtomically(s.path(a.ID), raw)
}

// Usage counts the records on disk and their bytes, against
// MaxPendingAttachments and MaxStoreBytes.
func (s *Store) Usage() (int, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, sizes, err := s.scan()
	if err != nil {
		return 0, 0, err
	}
	var total int64
	for _, n := range sizes {
		total += n
	}
	return len(sizes), total, nil
}

// Prune removes records whose attachment has expired: Cloud no longer
// accepts or serves them.
func (s *Store) Prune(nowMS int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids, _, err := s.scan()
	if err != nil {
		return err
	}
	for _, id := range ids {
		a, ok, err := s.load(id)
		if err != nil || !ok {
			continue
		}
		if a.ExpiresAtMS <= nowMS {
			if err := os.Remove(s.path(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fail(CodeStoreUnwritable)
			}
		}
	}
	return nil
}

func writeAtomically(path string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fail(CodeStoreUnwritable)
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fail(CodeStoreUnwritable)
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fail(CodeStoreUnwritable)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fail(CodeStoreUnwritable)
	}
	if err := tmp.Close(); err != nil {
		return fail(CodeStoreUnwritable)
	}
	if err := os.Rename(name, path); err != nil {
		return fail(CodeStoreUnwritable)
	}
	ok = true
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
