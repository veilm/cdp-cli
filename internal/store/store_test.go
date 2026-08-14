package store

import (
	"path/filepath"
	"testing"
)

func testStore(path string) *Store {
	return &Store{path: path, Sessions: make(map[string]Session)}
}

func TestSetMergesConcurrentStoreSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	first := testStore(path)
	second := testStore(path)

	if err := first.Set(Session{Name: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := second.Set(Session{Name: "second"}); err != nil {
		t.Fatal(err)
	}

	latest := testStore(path)
	if err := latest.reloadUnlocked(); err != nil {
		t.Fatal(err)
	}
	if _, ok := latest.Get("first"); !ok {
		t.Fatal("second writer erased the first session")
	}
	if _, ok := latest.Get("second"); !ok {
		t.Fatal("second session was not saved")
	}
}

func TestRemoveDoesNotEraseConcurrentSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	first := testStore(path)
	second := testStore(path)
	if err := first.Set(Session{Name: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := second.Set(Session{Name: "second"}); err != nil {
		t.Fatal(err)
	}

	removed, err := first.Remove("first")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected first session to be removed")
	}
	latest := testStore(path)
	if err := latest.reloadUnlocked(); err != nil {
		t.Fatal(err)
	}
	if _, ok := latest.Get("first"); ok {
		t.Fatal("removed session remains")
	}
	if _, ok := latest.Get("second"); !ok {
		t.Fatal("remove erased the concurrently-created session")
	}
}
