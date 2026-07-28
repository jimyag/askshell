package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/jimyag/askshell/internal/assistant"
)

var validID = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type Store struct {
	path string
}

func Open(id string) (*Store, error) {
	if id == "" {
		return nil, nil
	}
	if !validID.MatchString(id) {
		return nil, fmt.Errorf("invalid ASKSHELL_SESSION_ID")
	}

	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("find home directory: %w", err)
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(stateHome, "askshell", "sessions")
	//nolint:gosec // XDG_STATE_HOME intentionally controls the user's state directory.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create session directory: %w", err)
	}
	return &Store{path: filepath.Join(dir, id+".json")}, nil
}

func (s *Store) Load() (assistant.Conversation, error) {
	if s == nil {
		return assistant.Conversation{}, nil
	}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return assistant.Conversation{}, nil
	}
	if err != nil {
		return assistant.Conversation{}, fmt.Errorf("read session: %w", err)
	}

	var conversation assistant.Conversation
	if err := json.Unmarshal(data, &conversation); err == nil {
		return conversation, nil
	}

	var legacy []assistant.Turn
	if err := json.Unmarshal(data, &legacy); err != nil {
		return assistant.Conversation{}, fmt.Errorf("decode session: %w", err)
	}
	return assistant.Conversation{Turns: legacy}, nil
}

func (s *Store) Save(conversation assistant.Conversation) error {
	if s == nil {
		return nil
	}
	data, err := json.Marshal(conversation)
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".session-")
	if err != nil {
		return fmt.Errorf("create session file: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("protect session file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write session: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close session: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}
