package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Session describes a tracked DevTools target.
type Session struct {
	Name           string    `json:"name"`
	Host           string    `json:"host"`
	Port           int       `json:"port"`
	URL            string    `json:"url"`
	TargetID       string    `json:"targetId"`
	WebSocketURL   string    `json:"webSocketUrl"`
	Title          string    `json:"title"`
	Type           string    `json:"type"`
	LastConnected  time.Time `json:"lastConnected"`
	LastTargetInfo string    `json:"lastTargetInfo"`
}

// Store keeps sessions on disk.
type Store struct {
	path     string
	mu       sync.Mutex
	Sessions map[string]Session `json:"sessions"`
}

// Load initializes a store from the default location.
func Load() (*Store, error) {
	path, err := defaultPath()
	if err != nil {
		return nil, err
	}
	s := &Store{path: path, Sessions: make(map[string]Session)}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse sessions: %w", err)
	}
	if s.Sessions == nil {
		s.Sessions = make(map[string]Session)
	}
	return s, nil
}

// Save persists the store to disk.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Set stores / overwrites a named session.
func (s *Store) Set(session Session) error {
	s.Sessions[session.Name] = session
	return s.Save()
}

// Remove deletes the named session, returning false if it didn't exist.
func (s *Store) Remove(name string) (bool, error) {
	if _, ok := s.Sessions[name]; !ok {
		return false, nil
	}
	delete(s.Sessions, name)
	return true, s.Save()
}

// Get fetches a stored session.
func (s *Store) Get(name string) (Session, bool) {
	session, ok := s.Sessions[name]
	return session, ok
}

// List returns a copy of the session map.
func (s *Store) List() map[string]Session {
	out := make(map[string]Session, len(s.Sessions))
	for k, v := range s.Sessions {
		out[k] = v
	}
	return out
}

func defaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cdp-cli", "sessions.json"), nil
}
