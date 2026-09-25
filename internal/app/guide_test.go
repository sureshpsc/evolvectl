package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sureshpsc/evolvectl/internal/exitcode"
)

func TestInitOpensGuide(t *testing.T) {
	dir := t.TempDir()
	var opened string
	out := &strings.Builder{}
	a := &App{
		Out: out,
		Err: &strings.Builder{},
		Browser: func(path string) error {
			opened = path
			return nil
		},
	}
	code, err := a.Init(Option{Workspace: dir}, false, false, true)
	if err != nil || code != exitcode.Success {
		t.Fatal(code, err)
	}
	if !strings.HasSuffix(filepath.ToSlash(opened), ".evolvectl/guide/index.html") {
		t.Fatal(opened)
	}
	if _, err := os.Stat(filepath.Join(dir, ".evolvectl", "guide", "help.html")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Opened the guide") {
		t.Fatal(out.String())
	}
}

func TestInitBrowserFailureStillWrites(t *testing.T) {
	dir := t.TempDir()
	errBuf := &strings.Builder{}
	a := &App{
		Out: &strings.Builder{},
		Err: errBuf,
		Browser: func(string) error {
			return fmt.Errorf("no display")
		},
	}
	code, err := a.Init(Option{Workspace: dir}, false, false, true)
	if err != nil || code != exitcode.Success {
		t.Fatal(code, err)
	}
	if !strings.Contains(errBuf.String(), "no display") {
		t.Fatal(errBuf.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".evolvectl.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestOpenGuideHelpWithoutBrowser(t *testing.T) {
	dir := t.TempDir()
	opened := false
	out := &strings.Builder{}
	a := &App{
		Out: out,
		Browser: func(string) error {
			opened = true
			return nil
		},
	}
	code, err := a.OpenGuide(Option{Workspace: dir}, "help", false)
	if err != nil || code != exitcode.Success {
		t.Fatal(code, err)
	}
	if opened {
		t.Fatal("browser opened")
	}
	if !strings.Contains(out.String(), "help.html") {
		t.Fatal(out.String())
	}
	code, err = a.OpenGuide(Option{Workspace: dir}, "missing", false)
	if err == nil || code != exitcode.Invalid {
		t.Fatal(code, err)
	}
}
