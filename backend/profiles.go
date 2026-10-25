package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// The Compose profiles chosen in the tool, per project (the main one of a worktree), in the
// data folder: the assistant tools use them when a call names none.

var profilesMu sync.Mutex

func profilesFile() string { return filepath.Join(home, "profiles.json") }

func readProfiles() map[string][]string {
	all := map[string][]string{}
	if data, err := os.ReadFile(profilesFile()); err == nil {
		_ = json.Unmarshal(data, &all)
	}
	return all
}

func savedProfiles() []string {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	if p := readProfiles()[project]; p != nil {
		return p
	}
	return []string{}
}

func saveProfiles(p []string) error {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	all := readProfiles()
	if len(p) == 0 {
		delete(all, project)
	} else {
		all[project] = p
	}
	data, err := json.Marshal(all)
	if err != nil {
		return err
	}
	tmp := profilesFile() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, profilesFile())
}
