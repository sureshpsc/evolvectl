// Package discovery walks a workspace and builds a dependency inventory.
package discovery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/fsio"
	"github.com/sureshpsc/evolvectl/internal/idgen"
	"github.com/sureshpsc/evolvectl/internal/policy"
	"golang.org/x/mod/modfile"
)

// Options controls a scan.
type Options struct {
	Workers int
	Ignore  []string
	Changed bool
}

type cacheFile struct {
	Files map[string]cacheEntry `json:"files"`
}

type cacheEntry struct {
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
	Hash  string `json:"hash"`
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, "dist": true, "build": true, "target": true, ".idea": true,
	".evolvectl": true,
}

// Scan inventories workspace.
func Scan(ctx context.Context, root string, opt Options) (domain.Inventory, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return domain.Inventory{}, err
	}
	if opt.Workers < 1 {
		opt.Workers = 8
	}
	inv := domain.Inventory{
		ScannedAt: time.Now().UTC(),
		Root:      ".",
		RepoType:  "repository",
	}
	cache := loadCache(root)
	var (
		mu       sync.Mutex
		files    []string
		nextHash = map[string]cacheEntry{}
	)
	if err := walk(ctx, root, opt, &files, &inv); err != nil {
		return inv, err
	}
	sort.Strings(files)
	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < opt.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				if ctx.Err() != nil {
					return
				}
				info, err := os.Stat(path)
				if err != nil || !info.Mode().IsRegular() {
					continue
				}
				rel := fsio.RelSlash(root, path)
				key := rel
				if ent, ok := cache.Files[key]; ok && ent.Size == info.Size() && ent.Mtime == info.ModTime().UnixNano() {
					mu.Lock()
					inv.CacheHits++
					nextHash[key] = ent
					mu.Unlock()
					continue
				}
				sum, err := fsio.HashFile(path)
				if err != nil {
					continue
				}
				mu.Lock()
				inv.CacheMisses++
				nextHash[key] = cacheEntry{Size: info.Size(), Mtime: info.ModTime().UnixNano(), Hash: sum}
				mu.Unlock()
			}
		}()
	}
	for _, f := range files {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return inv, ctx.Err()
		case jobs <- f:
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return inv, ctx.Err()
	}
	saveCache(root, cacheFile{Files: nextHash})

	projects := map[string]*domain.Project{}
	for _, f := range files {
		rel := fsio.RelSlash(root, f)
		base := filepath.Base(f)
		body, _ := os.ReadFile(f)
		generated := policy.GeneratedContent(body) || policy.GeneratedPath(rel)
		if generated {
			inv.GeneratedPaths = append(inv.GeneratedPaths, rel)
		}
		lang := languageOf(base, rel)
		if lang != "" && !generated && !binaryName(base) {
			inv.SourceFiles = append(inv.SourceFiles, domain.SourceFile{
				Path: rel, Language: lang, Generated: generated, Hash: nextHash[rel].Hash,
			})
		}
		switch base {
		case "go.mod":
			p := ensureProject(projects, filepath.Dir(rel), "go", "gomod")
			p.Manifests = appendUnique(p.Manifests, rel)
			inv.Manifests = append(inv.Manifests, domain.Manifest{Path: rel, Ecosystem: "go", Kind: "go.mod", ProjectID: p.ID})
			deps, err := parseGoMod(rel, p.ID, body)
			if err != nil {
				inv.Warnings = append(inv.Warnings, rel+": "+err.Error())
			}
			inv.Dependencies = append(inv.Dependencies, deps...)
		case "go.work":
			inv.RepoType = "monorepo"
			uses, err := parseGoWork(rel, body)
			if err != nil {
				inv.Warnings = append(inv.Warnings, rel+": "+err.Error())
			}
			for _, u := range uses {
				inv.WorkspaceModules = appendUnique(inv.WorkspaceModules, u)
				if strings.HasPrefix(u, "../") || u == ".." {
					inv.Warnings = append(inv.Warnings, "go.work use "+u+" is outside the scan root")
				}
			}
			inv.Warnings = append(inv.Warnings, "go.work modules: "+strings.Join(uses, ", "))
		case "pyproject.toml":
			p := ensureProject(projects, filepath.Dir(rel), "python", "pyproject")
			p.Manifests = appendUnique(p.Manifests, rel)
			inv.Manifests = append(inv.Manifests, domain.Manifest{Path: rel, Ecosystem: "python", Kind: "pyproject", ProjectID: p.ID})
			inv.Dependencies = append(inv.Dependencies, parsePyProject(rel, p.ID, body)...)
		case "pom.xml":
			p := ensureProject(projects, filepath.Dir(rel), "java", "maven")
			p.Manifests = appendUnique(p.Manifests, rel)
			inv.Manifests = append(inv.Manifests, domain.Manifest{Path: rel, Ecosystem: "maven", Kind: "pom", ProjectID: p.ID})
			inv.Dependencies = append(inv.Dependencies, parsePom(rel, p.ID, body)...)
		case "package.json":
			p := ensureProject(projects, filepath.Dir(rel), "node", "node")
			p.Manifests = appendUnique(p.Manifests, rel)
			inv.Manifests = append(inv.Manifests, domain.Manifest{Path: rel, Ecosystem: "node", Kind: "package.json", ProjectID: p.ID})
			inv.Dependencies = append(inv.Dependencies, parsePackageJSON(rel, p.ID, body)...)
		case "copy.bara.sky":
			inv.CopybaraConfigs = append(inv.CopybaraConfigs, rel)
		case "WORKSPACE", "WORKSPACE.bazel", "MODULE.bazel":
			addBuild(&inv, "bazel")
		case "BUILD", "BUILD.bazel":
			addBuild(&inv, "bazel")
		default:
			if strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt") {
				p := ensureProject(projects, filepath.Dir(rel), "python", "requirements")
				p.Manifests = appendUnique(p.Manifests, rel)
				inv.Manifests = append(inv.Manifests, domain.Manifest{Path: rel, Ecosystem: "python", Kind: "requirements", ProjectID: p.ID})
				inv.Dependencies = append(inv.Dependencies, parseRequirements(rel, p.ID, body)...)
			}
		}
		if strings.HasSuffix(rel, ".evolvectl/adapters") || (strings.Contains(rel, ".evolvectl/adapters/") && strings.HasSuffix(rel, ".yaml")) {
			inv.CommandAdapters = append(inv.CommandAdapters, rel)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		inv.HasGit = true
		inv.WorkspaceAdapters = append(inv.WorkspaceAdapters, "git")
	}
	inv.WorkspaceAdapters = append(inv.WorkspaceAdapters, "filesystem")
	if len(inv.CopybaraConfigs) > 0 {
		inv.WorkspaceAdapters = append(inv.WorkspaceAdapters, "copybara")
	}
	for _, p := range projects {
		sort.Strings(p.Manifests)
		inv.Projects = append(inv.Projects, *p)
		switch p.Language {
		case "go":
			addBuild(&inv, "go modules")
		case "python":
			addBuild(&inv, p.Build)
		case "java":
			addBuild(&inv, "maven")
		case "node":
			addBuild(&inv, "node")
		}
	}
	sort.Slice(inv.Projects, func(i, j int) bool { return inv.Projects[i].Root < inv.Projects[j].Root })
	sort.Slice(inv.Dependencies, func(i, j int) bool {
		if inv.Dependencies[i].Ecosystem != inv.Dependencies[j].Ecosystem {
			return inv.Dependencies[i].Ecosystem < inv.Dependencies[j].Ecosystem
		}
		if inv.Dependencies[i].Name != inv.Dependencies[j].Name {
			return inv.Dependencies[i].Name < inv.Dependencies[j].Name
		}
		return inv.Dependencies[i].Manifest < inv.Dependencies[j].Manifest
	})
	sort.Slice(inv.SourceFiles, func(i, j int) bool { return inv.SourceFiles[i].Path < inv.SourceFiles[j].Path })
	sort.Strings(inv.BuildSystems)
	sort.Strings(inv.CopybaraConfigs)
	sort.Strings(inv.GeneratedPaths)
	langs := map[string]bool{}
	for _, p := range inv.Projects {
		langs[p.Language] = true
	}
	for l := range langs {
		inv.Languages = append(inv.Languages, l)
	}
	sort.Strings(inv.Languages)
	if len(inv.Projects) > 1 || len(inv.Languages) > 1 {
		inv.RepoType = "monorepo"
	}
	inv.FilesIndexed = len(files)
	for i := range inv.SourceFiles {
		inv.SourceFiles[i].ProjectID = projectFor(inv.Projects, inv.SourceFiles[i].Path)
	}
	return inv, nil
}

func walk(ctx context.Context, root string, opt Options, files *[]string, inv *domain.Inventory) error {
	var mu sync.Mutex
	var dirs []string
	dirs = append(dirs, root)
	seen := map[string]bool{root: true}
	for len(dirs) > 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		batch := dirs
		dirs = nil
		type found struct {
			files  []string
			dirs   []string
			vendor []string
		}
		out := make([]found, len(batch))
		var wg sync.WaitGroup
		sem := make(chan struct{}, opt.Workers)
		for i, dir := range batch {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int, dir string) {
				defer wg.Done()
				defer func() { <-sem }()
				entries, err := os.ReadDir(dir)
				if err != nil {
					return
				}
				sort.Slice(entries, func(a, b int) bool { return entries[a].Name() < entries[b].Name() })
				for _, e := range entries {
					full := filepath.Join(dir, e.Name())
					rel := fsio.RelSlash(root, full)
					if e.IsDir() {
						if skipDirs[e.Name()] || ignored(rel, opt.Ignore) {
							if e.Name() == "vendor" {
								out[i].vendor = append(out[i].vendor, rel)
							}
							continue
						}
						out[i].dirs = append(out[i].dirs, full)
						continue
					}
					if ignored(rel, opt.Ignore) || binaryName(e.Name()) {
						continue
					}
					out[i].files = append(out[i].files, full)
				}
			}(i, dir)
		}
		wg.Wait()
		for _, f := range out {
			mu.Lock()
			*files = append(*files, f.files...)
			inv.VendorPaths = append(inv.VendorPaths, f.vendor...)
			mu.Unlock()
			for _, d := range f.dirs {
				if !seen[d] {
					seen[d] = true
					dirs = append(dirs, d)
				}
			}
		}
		sort.Strings(dirs)
	}
	return nil
}

func ignored(rel string, patterns []string) bool {
	slash := filepath.ToSlash(rel)
	for _, p := range patterns {
		p = filepath.ToSlash(p)
		p = strings.TrimPrefix(p, "./")
		if p == "" {
			continue
		}
		if dir, ok := strings.CutSuffix(p, "/**"); ok && !strings.Contains(dir, "*") {
			if slash == dir || strings.HasPrefix(slash, dir+"/") {
				return true
			}
			continue
		}
		if strings.Contains(p, "**/") {
			body := strings.Trim(p, "*")
			body = strings.Trim(body, "/")
			if body == "" {
				continue
			}
			if slash == body || strings.Contains(slash, "/"+body+"/") || strings.HasPrefix(slash, body+"/") || strings.HasSuffix(slash, "/"+body) {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(p, filepath.Base(slash)); ok {
			return true
		}
		if slash == strings.TrimSuffix(p, "/") {
			return true
		}
	}
	return false
}

func languageOf(base, _ string) string {
	switch strings.ToLower(filepath.Ext(base)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".js", ".ts", ".tsx", ".jsx":
		return "node"
	default:
		return ""
	}
}

func binaryName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".zip", ".gz", ".exe", ".dll", ".so", ".dylib", ".pyc", ".wasm", ".bin", ".pdf":
		return true
	default:
		return false
	}
}

func ensureProject(m map[string]*domain.Project, dir, lang, build string) *domain.Project {
	if dir == "" {
		dir = "."
	}
	dir = filepath.ToSlash(dir)
	key := dir + "\x00" + lang
	if p, ok := m[key]; ok {
		if build != "" && p.Build == "" {
			p.Build = build
		}
		return p
	}
	p := &domain.Project{
		ID:         idgen.ProjectID(dir + ":" + lang),
		Root:       dir,
		Language:   lang,
		Build:      build,
		TestScopes: []string{dir},
	}
	m[key] = p
	return p
}

func projectFor(projects []domain.Project, file string) string {
	best := ""
	bestID := ""
	for _, p := range projects {
		root := p.Root
		if root == "." {
			if best == "" {
				best = "."
				bestID = p.ID
			}
			continue
		}
		if file == root || strings.HasPrefix(file, root+"/") {
			if len(root) > len(best) {
				best = root
				bestID = p.ID
			}
		}
	}
	return bestID
}

func appendUnique(in []string, v string) []string {
	for _, s := range in {
		if s == v {
			return in
		}
	}
	return append(in, v)
}

func addBuild(inv *domain.Inventory, name string) {
	for _, b := range inv.BuildSystems {
		if b == name {
			return
		}
	}
	inv.BuildSystems = append(inv.BuildSystems, name)
}

func parseGoWork(rel string, body []byte) ([]string, error) {
	f, err := modfile.ParseWork(rel, body, nil)
	if err != nil {
		return nil, err
	}
	base := filepath.Dir(filepath.FromSlash(rel))
	var out []string
	for _, u := range f.Use {
		p := filepath.ToSlash(filepath.Clean(filepath.Join(base, u.Path)))
		if p == "." {
			p = "."
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func parseGoMod(rel, projectID string, body []byte) ([]domain.Dependency, error) {
	f, err := modfile.Parse(rel, body, nil)
	if err != nil {
		return nil, err
	}
	var out []domain.Dependency
	for _, r := range f.Require {
		if r.Indirect {
			continue
		}
		out = append(out, dep("go", r.Mod.Path, r.Mod.Version, rel, projectID, true))
	}
	for _, r := range f.Require {
		if !r.Indirect {
			continue
		}
		out = append(out, dep("go", r.Mod.Path, r.Mod.Version, rel, projectID, false))
	}
	return out, nil
}

func parseRequirements(rel, projectID string, body []byte) []domain.Dependency {
	var out []domain.Dependency
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, ver, ok := splitReq(line)
		if !ok {
			continue
		}
		out = append(out, dep("python", name, ver, rel, projectID, true))
	}
	return out
}

func parsePyProject(rel, projectID string, body []byte) []domain.Dependency {
	text := string(body)
	var out []domain.Dependency
	lines := strings.Split(text, "\n")
	section := ""
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
			section = strings.Trim(trim, "[]")
			continue
		}
		if section == "project" && strings.Contains(trim, "==") && strings.Contains(trim, "\"") {
			name, ver, ok := splitQuotedReq(trim)
			if ok {
				out = append(out, dep("python", name, ver, rel, projectID, true))
			}
		}
		if section == "tool.poetry.dependencies" && strings.Contains(trim, "=") && !strings.HasPrefix(trim, "python ") && !strings.HasPrefix(trim, "python=") {
			parts := strings.SplitN(trim, "=", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(parts[0])
				ver := strings.Trim(strings.TrimSpace(parts[1]), "\"' ")
				ver = strings.TrimLeft(ver, "^~>=<!")
				if name != "" && name != "python" {
					out = append(out, dep("python", name, ver, rel, projectID, true))
				}
			}
		}
	}
	return out
}

func parsePackageJSON(rel, projectID string, body []byte) []domain.Dependency {
	var doc struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil
	}
	var out []domain.Dependency
	for name, ver := range doc.Dependencies {
		out = append(out, dep("node", name, strings.TrimLeft(ver, "^~"), rel, projectID, true))
	}
	for name, ver := range doc.DevDependencies {
		d := dep("node", name, strings.TrimLeft(ver, "^~"), rel, projectID, true)
		d.Scope = "dev"
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func parsePom(rel, projectID string, body []byte) []domain.Dependency {
	text := string(body)
	var out []domain.Dependency
	chunks := strings.Split(text, "<dependency>")
	for _, c := range chunks[1:] {
		end := strings.Index(c, "</dependency>")
		if end >= 0 {
			c = c[:end]
		}
		group := xmlTag(c, "groupId")
		artifact := xmlTag(c, "artifactId")
		ver := xmlTag(c, "version")
		if artifact == "" {
			continue
		}
		name := artifact
		if group != "" {
			name = group + ":" + artifact
		}
		out = append(out, dep("maven", name, ver, rel, projectID, true))
	}
	return out
}

func xmlTag(s, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	rest := s[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

func splitReq(line string) (string, string, bool) {
	line = strings.Split(line, "#")[0]
	line = strings.TrimSpace(line)
	for _, op := range []string{"==", ">=", "<=", "~=", "!="} {
		if i := strings.Index(line, op); i > 0 {
			return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+len(op):]), true
		}
	}
	if line != "" && !strings.Contains(line, " ") {
		return line, "", true
	}
	return "", "", false
}

func splitQuotedReq(line string) (string, string, bool) {
	i := strings.Index(line, "\"")
	if i < 0 {
		return "", "", false
	}
	rest := line[i+1:]
	j := strings.Index(rest, "\"")
	if j < 0 {
		return "", "", false
	}
	return splitReq(rest[:j])
}

func dep(ecosystem, name, version, manifest, projectID string, direct bool) domain.Dependency {
	return domain.Dependency{
		ID:             idgen.DepID(ecosystem, name, manifest),
		Ecosystem:      ecosystem,
		Name:           name,
		PackageURL:     purl(ecosystem, name, version),
		CurrentVersion: version,
		Direct:         direct,
		Manifest:       manifest,
		ProjectID:      projectID,
		Status:         domain.StatusRegistryUnavailable,
		StatusReason:   "registry was not queried; offline inventory records the declared version only",
		Source:         "manifest",
	}
}

func purl(ecosystem, name, version string) string {
	typ := ecosystem
	switch ecosystem {
	case "go":
		typ = "golang"
	case "node":
		typ = "npm"
	case "maven":
		typ = "maven"
	}
	if version == "" {
		return "pkg:" + typ + "/" + name
	}
	return "pkg:" + typ + "/" + name + "@" + version
}

func cachePath(root string) string {
	return filepath.Join(root, ".evolvectl", "cache", "files.json")
}

func loadCache(root string) cacheFile {
	b, err := os.ReadFile(cachePath(root))
	if err != nil {
		return cacheFile{Files: map[string]cacheEntry{}}
	}
	var c cacheFile
	if json.Unmarshal(b, &c) != nil || c.Files == nil {
		return cacheFile{Files: map[string]cacheEntry{}}
	}
	return c
}

func saveCache(root string, c cacheFile) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	_ = fsio.WriteAtomic(cachePath(root), b, 0o644)
}
