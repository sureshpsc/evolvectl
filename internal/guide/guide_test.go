package guide

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteGuide(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir); err != nil {
		t.Fatal(err)
	}
	for _, p := range Pages() {
		b, err := os.ReadFile(filepath.Join(dir, p.File))
		if err != nil {
			t.Fatal(err)
		}
		html := string(b)
		if !strings.Contains(html, "<title>") || !strings.Contains(html, "help.html") {
			t.Fatalf("%s missing nav or title", p.File)
		}
		if strings.Contains(html, "https://") || strings.Contains(html, "http://") || strings.Contains(html, "cdn.") {
			t.Fatalf("%s references a remote asset", p.File)
		}
	}
	help, err := os.ReadFile(filepath.Join(dir, "help.html"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(help)
	for _, name := range []string{"upgrade", "copybara explain", "session export", "init"} {
		if !strings.Contains(text, `data-command="`+name) {
			t.Fatalf("help missing %s", name)
		}
	}
	if !strings.Contains(text, "google.golang.org/grpc") {
		t.Fatal("help missing upgrade example")
	}
	start, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(start), "copybara.html") || !strings.Contains(string(start), "languages.html") {
		t.Fatal("start page missing links")
	}
	copybara, err := os.ReadFile(filepath.Join(dir, "copybara.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(copybara), "copy.bara.sky") || !strings.Contains(string(copybara), "EVOLVECTL_WORKSPACE_PROVIDER") {
		t.Fatal("copybara page")
	}
	lang, err := os.ReadFile(filepath.Join(dir, "languages.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"pom.xml", "pyproject.toml", "package.json", "MODULE.bazel"} {
		if !strings.Contains(string(lang), needle) {
			t.Fatalf("languages missing %s", needle)
		}
	}
}

func TestEvenBackticks(t *testing.T) {
	var check func(string)
	check = func(s string) {
		if strings.Count(s, "`")%2 != 0 {
			t.Fatalf("odd backticks: %s", s)
		}
	}
	for _, p := range Pages() {
		check(p.Lead)
		for _, s := range p.Sections {
			check(s.Callout)
			for _, para := range s.Paragraphs {
				check(para)
			}
			for _, b := range s.Bullets {
				check(b)
			}
			if s.Table != nil {
				for _, row := range s.Table.Rows {
					for _, cell := range row {
						check(cell)
					}
				}
			}
		}
		for _, c := range p.Commands {
			check(c.Summary)
			for _, para := range c.Paragraphs {
				check(para)
			}
			for _, f := range c.Flags {
				check(f)
			}
			for _, ex := range c.Examples {
				check(ex.Note)
			}
		}
	}
}

func TestFileForTopic(t *testing.T) {
	file, ok := FileForTopic("")
	if !ok || file != "index.html" {
		t.Fatal(file, ok)
	}
	file, ok = FileForTopic("languages")
	if !ok || file != "languages.html" {
		t.Fatal(file, ok)
	}
	if _, ok := FileForTopic("nope"); ok {
		t.Fatal("accepted unknown topic")
	}
}
