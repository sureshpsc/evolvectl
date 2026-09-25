package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sureshpsc/evolvectl/internal/guide"
)

func TestRootHelp(t *testing.T) {
	root := NewRoot()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "evolvectl scan") && !strings.Contains(buf.String(), "scan") {
		t.Fatal(buf.String())
	}
}

func TestVersionDoesNotNeedRepo(t *testing.T) {
	root := NewRoot()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "evolvectl") {
		t.Fatal(buf.String())
	}
}

func TestInitWritesGuideWithoutBrowser(t *testing.T) {
	dir := t.TempDir()
	root := NewRoot()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"init", "--workspace", dir, "--no-browser"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	helpPage := filepath.Join(dir, ".evolvectl", "guide", "help.html")
	b, err := os.ReadFile(helpPage)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "evolvectl upgrade") {
		t.Fatal("help page")
	}
	if !strings.Contains(buf.String(), "Guide") {
		t.Fatal(buf.String())
	}
}

func TestHelpCommandPrintsUpgrade(t *testing.T) {
	root := NewRoot()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"help", "upgrade"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "--dry-run") {
		t.Fatal(buf.String())
	}
}

func TestGuideListsEveryCommand(t *testing.T) {
	root := NewRoot()
	var got []string
	var walk func(c *cobra.Command, prefix string)
	walk = func(c *cobra.Command, prefix string) {
		for _, child := range c.Commands() {
			if child.Hidden {
				continue
			}
			path := child.Name()
			if prefix != "" {
				path = prefix + " " + child.Name()
			}
			got = append(got, path)
			walk(child, path)
		}
	}
	walk(root, "")
	want := append([]string{}, guide.CommandNames()...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("cobra:\n%s\n\nguide:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
