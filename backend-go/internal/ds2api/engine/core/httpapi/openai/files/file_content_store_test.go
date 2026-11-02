package files

import (
	"testing"
	"time"
)

func TestMemoryContentStore_StoreAndRead(t *testing.T) {
	store := NewMemoryContentStore(1024, time.Hour)
	if err := store.Store("file-1", "hello.txt", "text/plain", []byte("hello world")); err != nil {
		t.Fatalf("store failed: %v", err)
	}
	filename, mimeType, data, err := store.Read("file-1")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if filename != "hello.txt" {
		t.Errorf("filename mismatch: got %q", filename)
	}
	if mimeType != "text/plain" {
		t.Errorf("mime type mismatch: got %q", mimeType)
	}
	if string(data) != "hello world" {
		t.Errorf("data mismatch: got %q", string(data))
	}
}

func TestMemoryContentStore_ReadMissing(t *testing.T) {
	store := NewMemoryContentStore(1024, time.Hour)
	_, _, _, err := store.Read("missing")
	if err != ErrFileNotFound {
		t.Fatalf("expected ErrFileNotFound, got %v", err)
	}
}

func TestMemoryContentStore_ReadEmptyID(t *testing.T) {
	store := NewMemoryContentStore(1024, time.Hour)
	_, _, _, err := store.Read("   ")
	if err != ErrFileNotFound {
		t.Fatalf("expected ErrFileNotFound for empty id, got %v", err)
	}
}

func TestMemoryContentStore_TooLarge(t *testing.T) {
	store := NewMemoryContentStore(5, time.Hour)
	if err := store.Store("file-1", "big.txt", "text/plain", []byte("hello world")); err != ErrFileTooLarge {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestMemoryContentStore_Expired(t *testing.T) {
	store := NewMemoryContentStore(1024, time.Nanosecond)
	if err := store.Store("file-1", "hello.txt", "text/plain", []byte("hello world")); err != nil {
		t.Fatalf("store failed: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	_, _, _, err := store.Read("file-1")
	if err != ErrFileNotFound {
		t.Fatalf("expected ErrFileNotFound after expiry, got %v", err)
	}
}

func TestMemoryContentStore_CleanupExpired(t *testing.T) {
	store := NewMemoryContentStore(1024, time.Nanosecond)
	if err := store.Store("a", "a.txt", "text/plain", []byte("a")); err != nil {
		t.Fatalf("store failed: %v", err)
	}
	if err := store.Store("b", "b.txt", "text/plain", []byte("b")); err != nil {
		t.Fatalf("store failed: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	store.CleanupExpired()
	if _, _, _, err := store.Read("a"); err != ErrFileNotFound {
		t.Errorf("expected a expired")
	}
	if _, _, _, err := store.Read("b"); err != ErrFileNotFound {
		t.Errorf("expected b expired")
	}
}

func TestMemoryContentStore_Delete(t *testing.T) {
	store := NewMemoryContentStore(1024, time.Hour)
	if err := store.Store("file-1", "hello.txt", "text/plain", []byte("hello world")); err != nil {
		t.Fatalf("store failed: %v", err)
	}
	store.Delete("file-1")
	if _, _, _, err := store.Read("file-1"); err != ErrFileNotFound {
		t.Fatalf("expected ErrFileNotFound after delete, got %v", err)
	}
}
