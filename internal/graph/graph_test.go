package graph

import (
	"reflect"
	"testing"

	"github.com/evolvectl/evolvectl/internal/domain"
)

func TestBuildLinksImportsToDependencies(t *testing.T) {
	inv := domain.Inventory{
		Dependencies: []domain.Dependency{
			{ID: "dep:a:grpc", Name: "google.golang.org/grpc", Ecosystem: "go", ProjectID: "a"},
			{ID: "dep:b:grpc", Name: "google.golang.org/grpc", Ecosystem: "go", ProjectID: "b"},
			{ID: "dep:py:pyyaml", Name: "typing-extensions", Ecosystem: "python", ProjectID: "py"},
			{ID: "dep:a:grpcx", Name: "google.golang.org/grpcx", Ecosystem: "go", ProjectID: "a"},
		},
		SourceFiles: []domain.SourceFile{
			{Path: "a/client.go", Language: "go", ProjectID: "a", Imports: []string{"google.golang.org/grpc/codes"}},
			{Path: "c/other.go", Language: "go", ProjectID: "c", Imports: []string{"google.golang.org/grpc"}},
			{Path: "py/m.py", Language: "python", ProjectID: "py", Imports: []string{"typing_extensions.foo"}},
		},
	}
	g := Build(t.TempDir(), inv)
	got := map[string][]string{}
	for _, e := range g.Edges {
		if e.Kind == "imports" {
			got[e.From] = append(got[e.From], e.To)
		}
	}
	want := map[string][]string{
		"file:a/client.go": {"dep:a:grpc"},
		"file:c/other.go":  {"dep:a:grpc", "dep:b:grpc"},
		"file:py/m.py":     {"dep:py:pyyaml"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("imports edges\n got %v\nwant %v", got, want)
	}
	if files := FilesImporting(g, "google.golang.org/grpc"); !reflect.DeepEqual(files, []string{"a/client.go", "c/other.go"}) {
		t.Fatalf("FilesImporting = %v", files)
	}
}
