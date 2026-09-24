package cli

import (
	"bytes"
	"strings"
	"testing"
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
