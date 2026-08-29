package files

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// ErrFileNotFound is returned when a file ID is not present in the store.
var ErrFileNotFound = errors.New("file content not found")

// ErrFileTooLarge is returned when a file exceeds the per-file byte limit.
var ErrFileTooLarge = errors.New("file content exceeds size limit")

// ContentStore keeps uploaded file bytes in memory so that request handlers can
// later read them back without depending on upstream file download APIs.
type ContentStore interface {
	Store(id string, filename, mimeType string, data []byte) error
	Read(id string) (filename, mimeType string, data []byte, err error)
}

// MemoryContentStore is an in-memory ContentStore with a TTL and a per-file
// byte cap. It is safe for concurrent use.
type MemoryContentStore struct {
	mu       sync.RWMutex
	entries  map[string]*memoryContentEntry
	maxBytes int
	ttl      time.Duration
}

type memoryContentEntry struct {
	filename  string
	mimeType  string
	data      []byte
	expiresAt time.Time
}

// NewMemoryContentStore creates a store that caps each stored file at maxBytes
// and drops entries ttl after they are written.
func NewMemoryContentStore(maxBytes int, ttl time.Duration) *MemoryContentStore {
	return &MemoryContentStore{
		entries:  make(map[string]*memoryContentEntry),
		maxBytes: maxBytes,
		ttl:      ttl,
	}
}

// Store saves file bytes keyed by id. Empty ids are rejected. Files larger than
// maxBytes are rejected with ErrFileTooLarge.
func (s *MemoryContentStore) Store(id string, filename, mimeType string, data []byte) error {
	if id = strings.TrimSpace(id); id == "" {
		return errors.New("file id is required")
	}
	if s.maxBytes > 0 && len(data) > s.maxBytes {
		return ErrFileTooLarge
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[id] = &memoryContentEntry{
		filename:  filename,
		mimeType:  mimeType,
		data:      data,
		expiresAt: time.Now().Add(s.ttl),
	}
	return nil
}

// Read returns the stored file content and metadata. Expired entries are
// treated as missing.
func (s *MemoryContentStore) Read(id string) (filename, mimeType string, data []byte, err error) {
	if id = strings.TrimSpace(id); id == "" {
		return "", "", nil, ErrFileNotFound
	}
	s.mu.RLock()
	entry, ok := s.entries[id]
	s.mu.RUnlock()
	if !ok || entry == nil {
		return "", "", nil, ErrFileNotFound
	}
	if time.Now().After(entry.expiresAt) {
		s.mu.Lock()
		// Re-check under write lock in case another goroutine refreshed it.
		if e, stillThere := s.entries[id]; stillThere && e == entry {
			delete(s.entries, id)
		}
		s.mu.Unlock()
		return "", "", nil, ErrFileNotFound
	}
	return entry.filename, entry.mimeType, entry.data, nil
}

// Delete removes a file from the store. It is safe to call for a missing id.
func (s *MemoryContentStore) Delete(id string) {
	if id = strings.TrimSpace(id); id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, id)
}

// CleanupExpired removes all entries past their TTL.
func (s *MemoryContentStore) CleanupExpired() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, id)
		}
	}
}
