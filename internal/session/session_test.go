package session

import "testing"

func TestPrecedence(t *testing.T) {
	got, src := Resolve("copybara", "git", "filesystem")
	if got != "copybara" || src != "flag" {
		t.Fatalf("%s %s", got, src)
	}
	got, src = Resolve("", "git", "filesystem")
	if got != "git" || src != "session" {
		t.Fatalf("%s %s", got, src)
	}
	got, src = Resolve("", "", "auto")
	if got != "auto" || src != "auto" {
		t.Fatalf("%s %s", got, src)
	}
}

func TestExportRejectsInjection(t *testing.T) {
	if _, err := Export("git; rm -rf /", "bash"); err == nil {
		t.Fatal("accepted unsafe name")
	}
	got, err := Export("copybara", "bash")
	if err != nil {
		t.Fatal(err)
	}
	if got != "export EVOLVECTL_WORKSPACE_PROVIDER='copybara'\n" {
		t.Fatalf("%q", got)
	}
	got, err = Export("git", "powershell")
	if err != nil || got != "$env:EVOLVECTL_WORKSPACE_PROVIDER='git'\n" {
		t.Fatalf("%q %v", got, err)
	}
}
