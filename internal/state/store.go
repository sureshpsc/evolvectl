// Package state persists run reports and append-only events under .evolvectl.
package state

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/fsio"
)

// Store is a workspace-local run store.
type Store struct {
	Root string
}

// Open returns the store rooted at the workspace.
func Open(workspace string) *Store {
	return &Store{Root: workspace}
}

func (s *Store) base() string {
	return filepath.Join(s.Root, ".evolvectl", "runs")
}

// SaveReport writes the snapshot atomically.
func (s *Store) SaveReport(r *domain.RunReport) error {
	if r == nil || r.ID == "" {
		return os.ErrInvalid
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return fsio.WriteAtomic(filepath.Join(s.base(), r.ID, "report.json"), b, 0o644)
}

// LoadReport reads one snapshot.
func (s *Store) LoadReport(id string) (*domain.RunReport, error) {
	if strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return nil, os.ErrInvalid
	}
	b, err := os.ReadFile(filepath.Join(s.base(), id, "report.json"))
	if err != nil {
		return nil, err
	}
	var r domain.RunReport
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// AppendEvent adds one JSONL event.
func (s *Store) AppendEvent(e domain.RunEvent) error {
	if strings.Contains(e.RunID, "..") || strings.ContainsAny(e.RunID, `/\`) {
		return os.ErrInvalid
	}
	dir := filepath.Join(s.base(), e.RunID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// Events reads the event log.
func (s *Store) Events(id string) ([]domain.RunEvent, error) {
	if strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return nil, os.ErrInvalid
	}
	f, err := os.Open(filepath.Join(s.base(), id, "events.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []domain.RunEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 2<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e domain.RunEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// ListReports returns reports sorted by creation time descending.
func (s *Store) ListReports() ([]*domain.RunReport, error) {
	entries, err := os.ReadDir(s.base())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*domain.RunReport
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := s.LoadReport(e.Name())
		if err != nil {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// SavePatch writes the unified patch artifact.
func (s *Store) SavePatch(id string, data []byte) (string, error) {
	rel := filepath.ToSlash(filepath.Join(".evolvectl", "patches", id+".patch"))
	abs := filepath.Join(s.Root, filepath.FromSlash(rel))
	if err := fsio.WriteAtomic(abs, data, 0o644); err != nil {
		return "", err
	}
	return rel, nil
}
