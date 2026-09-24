package batch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCSVAndYAML(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "sheet.csv")
	csvBody := "\xef\xbb\xbfDependency,Target Version,To Module,Workspace,Developer Owner\n" +
		"github.com/googleapis/gax-go,v2.0.2,github.com/googleapis/gax-go/v2,services/speech,Suresh Peddinti\n" +
		",,,,\n" +
		"github.com/paulmach/orb,v0.13.0,,,\n"
	if err := os.WriteFile(csvPath, []byte(csvBody), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := Load(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ToModule != "github.com/googleapis/gax-go/v2" || rows[0].Owner != "Suresh Peddinti" || rows[0].Workspace != "services/speech" {
		t.Fatalf("%+v", rows)
	}
	yamlPath := filepath.Join(dir, "sheet.yaml")
	yamlBody := "upgrades:\n  - dependency: github.com/stretchr/testify\n    to: v1.12.1\n"
	if err := os.WriteFile(yamlPath, []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err = Load(yamlPath)
	if err != nil || len(rows) != 1 || rows[0].To != "v1.12.1" {
		t.Fatalf("%v %+v", err, rows)
	}
}

func TestLoadRejectsEscapingWorkspace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(p, []byte("upgrades:\n  - dependency: a/b\n    to: v1.0.0\n    workspace: ../other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("expected an error")
	}
}
