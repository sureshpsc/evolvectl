// Package batch reads a list of upgrades from YAML or CSV.
package batch

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Row is one upgrade request.
type Row struct {
	Dependency string `yaml:"dependency"`
	To         string `yaml:"to"`
	ToModule   string `yaml:"to_module"`
	Workspace  string `yaml:"workspace"`
	Owner      string `yaml:"owner"`
}

type file struct {
	Upgrades []Row `yaml:"upgrades"`
}

// Load reads path. .csv is CSV with a header row; anything else is YAML with an upgrades list.
func Load(path string) ([]Row, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []Row
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		rows, err = parseCSV(b)
	} else {
		rows, err = parseYAML(b)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i, r := range rows {
		if r.Dependency == "" || r.To == "" {
			return nil, fmt.Errorf("%s: row %d needs dependency and to", path, i+1)
		}
		ws := filepath.ToSlash(filepath.Clean(filepath.FromSlash(r.Workspace)))
		if r.Workspace != "" && (filepath.IsAbs(r.Workspace) || ws == ".." || strings.HasPrefix(ws, "../")) {
			return nil, fmt.Errorf("%s: row %d workspace must stay inside the batch workspace: %s", path, i+1, r.Workspace)
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s: no upgrades listed", path)
	}
	return rows, nil
}

func parseYAML(b []byte) ([]Row, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var f file
	if err := dec.Decode(&f); err != nil && err != io.EOF {
		return nil, err
	}
	return f.Upgrades, nil
}

var columns = map[string]string{
	"dependency": "dependency", "name": "dependency", "module": "dependency", "package": "dependency",
	"to": "to", "target": "to", "version": "to", "target_version": "to",
	"to_module": "to_module", "new_module": "to_module",
	"workspace": "workspace", "path": "workspace", "dir": "workspace",
	"owner": "owner", "developer_owner": "owner",
}

func parseCSV(b []byte) ([]Row, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	idx := map[string]int{}
	for i, h := range records[0] {
		key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(h), " ", "_"))
		if c, ok := columns[key]; ok {
			idx[c] = i
		}
	}
	if _, ok := idx["dependency"]; !ok {
		return nil, fmt.Errorf("CSV header needs a dependency column")
	}
	if _, ok := idx["to"]; !ok {
		return nil, fmt.Errorf("CSV header needs a to column")
	}
	get := func(rec []string, col string) string {
		i, ok := idx[col]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	var rows []Row
	for _, rec := range records[1:] {
		row := Row{
			Dependency: get(rec, "dependency"), To: get(rec, "to"), ToModule: get(rec, "to_module"),
			Workspace: get(rec, "workspace"), Owner: get(rec, "owner"),
		}
		if row.Dependency == "" && row.To == "" {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}
