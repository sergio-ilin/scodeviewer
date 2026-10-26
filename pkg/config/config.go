package config

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"scodeviewer/pkg/models"
)

type Manager struct {
	mu         sync.RWMutex
	configPath string
	config     models.Config
}

func NewManager() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".scodeviewer")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	configPath := filepath.Join(dir, "config.json")

	m := &Manager{
		configPath: configPath,
		config: models.Config{
			Projects:        make([]models.Project, 0),
			ThemePreference: "system",
		},
	}
	_ = m.load()
	return m, nil
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return err
	}
	var cfg models.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	m.config = cfg
	return nil
}

func (m *Manager) save() error {
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.configPath, data, 0644)
}

func generateID(path string) string {
	h := sha1.New()
	h.Write([]byte(filepath.Clean(path)))
	return hex.EncodeToString(h.Sum(nil))[:10]
}

func (m *Manager) GetConfig() models.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

func (m *Manager) AddProject(path string, isGit bool) models.Project {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanPath := filepath.Clean(path)
	name := filepath.Base(cleanPath)
	id := generateID(cleanPath)

	for i, p := range m.config.Projects {
		if p.Path == cleanPath || p.ID == id {
			m.config.Projects[i].LastOpened = time.Now()
			m.config.Projects[i].IsGit = isGit
			m.config.ActivePath = cleanPath
			_ = m.save()
			return m.config.Projects[i]
		}
	}

	newProj := models.Project{
		ID:         id,
		Name:       name,
		Path:       cleanPath,
		LastOpened: time.Now(),
		AddedAt:    time.Now(),
		IsGit:      isGit,
	}

	m.config.Projects = append([]models.Project{newProj}, m.config.Projects...)
	m.config.ActivePath = cleanPath
	_ = m.save()
	return newProj
}

func (m *Manager) RemoveProject(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	found := false
	filtered := make([]models.Project, 0, len(m.config.Projects))
	for _, p := range m.config.Projects {
		if p.ID == id || p.Path == id {
			found = true
			continue
		}
		filtered = append(filtered, p)
	}

	if found {
		m.config.Projects = filtered
		if len(filtered) > 0 {
			m.config.ActivePath = filtered[0].Path
		} else {
			m.config.ActivePath = ""
		}
		_ = m.save()
	}
	return found
}

func (m *Manager) RenameProject(idOrPath string, newName string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanName := strings.TrimSpace(newName)
	if cleanName == "" {
		return false
	}

	for i, p := range m.config.Projects {
		if p.ID == idOrPath || p.Path == idOrPath {
			m.config.Projects[i].Name = cleanName
			_ = m.save()
			return true
		}
	}
	return false
}

func (m *Manager) SetActive(pathOrID string) (models.Project, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, p := range m.config.Projects {
		if p.ID == pathOrID || p.Path == pathOrID {
			m.config.Projects[i].LastOpened = time.Now()
			m.config.ActivePath = p.Path
			_ = m.save()
			return m.config.Projects[i], true
		}
	}
	return models.Project{}, false
}

func (m *Manager) SetTheme(theme string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.ThemePreference = theme
	_ = m.save()
}
