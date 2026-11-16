package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testMatrix = `
jobs:
  contract:
    strategy:
      matrix:
        include:
          - kodi: Piers
            image: matthuisman/kodi-headless:Piers-22.0rc2@sha256:aaaa
          - kodi: Omega
            image: matthuisman/kodi-headless:Omega-21.3@sha256:bbbb
          - kodi: Other
            image: example/other:1.0
  unrelated:
    runs-on: ubuntu-26.04
`

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeSchema writes a schema directory whose methods.json holds methods and whose version.txt holds version.
func writeSchema(t *testing.T, dir, version, methods string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "methods.json"), methods)
	writeFile(t, filepath.Join(dir, "notifications.json"), `{}`)
	writeFile(t, filepath.Join(dir, "types.json"), `{}`)
	writeFile(t, filepath.Join(dir, versionFile), "JSONRPC_VERSION "+version+"\n")
	writeFile(t, filepath.Join(dir, commitFile), "c0ffee\n")
}

func TestReadMatrix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kodi-contract.yml")
	writeFile(t, path, testMatrix)

	pins, err := readMatrix(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []pin{{name: "Omega", kodiTag: "21.3-Omega"}, {name: "Piers", kodiTag: "22.0rc2-Piers"}}
	if len(pins) != len(want) || pins[0] != want[0] || pins[1] != want[1] {
		t.Errorf("readMatrix = %+v, want %+v", pins, want)
	}
}

func TestReadMatrixErrors(t *testing.T) {
	for name, src := range map[string]string{
		"invalid YAML":   "jobs: [",
		"no Kodi images": `jobs: {a: {strategy: {matrix: {include: [{kodi: X, image: "example/other:1"}]}}}}`,
		"pinned twice": `jobs: {a: {strategy: {matrix: {include: [
			{kodi: Piers, image: "matthuisman/kodi-headless:Piers-22.0@sha256:a"},
			{kodi: Piers, image: "matthuisman/kodi-headless:Piers-22.1@sha256:b"}]}}}}`,
		"no release name":     `jobs: {a: {strategy: {matrix: {include: [{image: "matthuisman/kodi-headless:Piers-22.0@sha256:a"}]}}}}`,
		"tag without version": `jobs: {a: {strategy: {matrix: {include: [{kodi: Piers, image: "matthuisman/kodi-headless:Piers@sha256:a"}]}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "m.yml")
			writeFile(t, path, src)
			if _, err := readMatrix(path); err == nil {
				t.Error("expected an error")
			}
		})
	}
	if _, err := readMatrix(filepath.Join(t.TempDir(), "missing.yml")); err == nil {
		t.Error("missing file: expected an error")
	}
}

// fakeXbmc points kodigen at a local git repository tagged 21.3-Omega (lightweight) and 22.0rc2-Piers (annotated), and at a server holding each tagged commit's schema files. It returns the commits by tag.
func fakeXbmc(t *testing.T) map[string]string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found")
	}
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(gitPath, append([]string{"-c", "user.name=kodigen", "-c", "user.email=kodigen@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "omega")
	git("tag", "21.3-Omega")
	omega := git("rev-parse", "HEAD")
	git("commit", "-q", "--allow-empty", "-m", "piers")
	git("tag", "-a", "-m", "Piers rc2", "22.0rc2-Piers")
	piers := git("rev-parse", "HEAD")

	files := map[string]string{
		omega + "/methods.json": `{"A.Old": {"type": "method", "params": [], "returns": "string"}}`,
		omega + "/version.txt":  "JSONRPC_VERSION 13.5.0\n",
		piers + "/methods.json": `{"A.Old": {"type": "method", "params": [], "returns": "string"}, "A.New": {"type": "method", "params": [], "returns": "string"}}`,
		piers + "/version.txt":  "JSONRPC_VERSION 13.2000.0\n",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		commit, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		name, ok := strings.CutPrefix(rest, "xbmc/interfaces/json-rpc/schema/")
		if !ok {
			http.NotFound(w, r)
			return
		}
		if body, ok := files[commit+"/"+name]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		if name == "notifications.json" || name == "types.json" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	oldRepo, oldRaw := xbmcRepo, xbmcRaw
	xbmcRepo, xbmcRaw = repo, srv.URL
	t.Cleanup(func() { xbmcRepo, xbmcRaw = oldRepo, oldRaw })
	return map[string]string{"21.3-Omega": omega, "22.0rc2-Piers": piers}
}

func TestResolveTag(t *testing.T) {
	commits := fakeXbmc(t)
	for tag, want := range commits {
		got, err := resolveTag(t.Context(), tag)
		if err != nil || got != want {
			t.Errorf("resolveTag(%s) = %q, %v; want %q (an annotated tag resolves to its commit)", tag, got, err, want)
		}
	}
	if _, err := resolveTag(t.Context(), "23.0-Nope"); err == nil {
		t.Error("missing tag: expected an error")
	}
	xbmcRepo = filepath.Join(t.TempDir(), "not-a-repo")
	if _, err := resolveTag(t.Context(), "21.3-Omega"); err == nil {
		t.Error("missing repository: expected an error")
	}
}

func TestFetchSchema(t *testing.T) {
	commits := fakeXbmc(t)
	dir := filepath.Join(t.TempDir(), "22.0rc2-Piers")
	if err := fetchSchema(t.Context(), "22.0rc2-Piers", dir); err != nil {
		t.Fatal(err)
	}
	spec, err := loadSpec(dir)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Version != (APIVersion{13, 2000, 0}) || spec.Tag != "22.0rc2-Piers" || len(spec.Methods) != 2 {
		t.Errorf("fetched %s at %s with %d methods", spec.Tag, spec.Version, len(spec.Methods))
	}
	if b, _ := os.ReadFile(filepath.Join(dir, commitFile)); strings.TrimSpace(string(b)) != commits["22.0rc2-Piers"] {
		t.Errorf("commit.txt = %q, want %s", b, commits["22.0rc2-Piers"])
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), ".fetch-*")); len(leftovers) > 0 {
		t.Errorf("temporary directories left behind: %v", leftovers)
	}
}

func TestFetchSchemaErrors(t *testing.T) {
	fakeXbmc(t)
	parent := t.TempDir()
	if err := fetchSchema(t.Context(), "23.0-Nope", filepath.Join(parent, "23.0-Nope")); err == nil {
		t.Error("missing tag: expected an error")
	}

	// A server without the files answers 404.
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	xbmcRaw = srv.URL
	if err := fetchSchema(t.Context(), "21.3-Omega", filepath.Join(parent, "21.3-Omega")); err == nil {
		t.Error("missing files: expected an error")
	}

	// A server sending something that is not a schema.
	junk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("junk")) }))
	t.Cleanup(junk.Close)
	xbmcRaw = junk.URL
	if err := fetchSchema(t.Context(), "21.3-Omega", filepath.Join(parent, "21.3-Omega")); err == nil {
		t.Error("invalid schema: expected an error")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := fetchSchema(ctx, "21.3-Omega", filepath.Join(parent, "21.3-Omega")); err == nil {
		t.Error("cancelled: expected an error")
	}
	if _, err := os.Stat(filepath.Join(parent, "21.3-Omega")); !os.IsNotExist(err) {
		t.Error("a failed fetch must not leave the schema directory behind")
	}
}

func TestLoadChainAndPrune(t *testing.T) {
	dir := t.TempDir()
	same := `{"A.Old": {"type": "method", "params": [], "returns": "string"}}`
	writeSchema(t, filepath.Join(dir, "22.0rc2-Piers"), "13.2000.0", same)
	writeSchema(t, filepath.Join(dir, "22.0rc1-Piers"), "13.1000.0", same)
	writeSchema(t, filepath.Join(dir, "21.4-Omega"), "13.5.0", `{}`)
	writeSchema(t, filepath.Join(dir, "21.3-Omega"), "13.5.0", `{}`)
	writeSchema(t, filepath.Join(dir, ".fetch-123"), "13.9999.0", `{}`)
	writeFile(t, filepath.Join(dir, "SOURCES.md"), "")

	chain, err := loadChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, s := range chain {
		tags = append(tags, s.Tag)
	}
	if got := strings.Join(tags, " "); got != "21.3-Omega 21.4-Omega 22.0rc1-Piers 22.0rc2-Piers" {
		t.Fatalf("chain = %s, want oldest version first, ties by name, hidden directories skipped", got)
	}

	// 21.4-Omega repeats 21.3-Omega and no image pins it; 22.0rc2-Piers repeats 22.0rc1-Piers but is pinned; 22.0rc1-Piers adds A.Old.
	stale, err := prunable(chain, []pin{{name: "Piers", kodiTag: "22.0rc2-Piers"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0].Tag != "21.4-Omega" {
		t.Errorf("prunable = %v, want only 21.4-Omega", stale)
	}

	if err := checkPinned(chain, []pin{{name: "Q", kodiTag: "23.0-Q"}}); err == nil || !strings.Contains(err.Error(), "23.0-Q") {
		t.Errorf("checkPinned = %v, want the missing 23.0-Q named", err)
	}
	if _, err := renderReleases(chain, []pin{{name: "Q", kodiTag: "23.0-Q"}}, "kodi"); err == nil {
		t.Error("renderReleases: expected an error for a missing schema")
	}
}

func TestLoadChainErrors(t *testing.T) {
	if _, err := loadChain(t.TempDir()); err == nil {
		t.Error("empty: expected an error")
	}
	if _, err := loadChain(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing: expected an error")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "1.0-A", "methods.json"), `{}`)
	if _, err := loadChain(dir); err == nil {
		t.Error("incomplete schema: expected an error")
	}
	// sameAPI and renderSources need the files they compare and record.
	writeSchema(t, filepath.Join(dir, "2.0-B"), "13.1.0", `{}`)
	if _, err := sameAPI(filepath.Join(dir, "1.0-A"), filepath.Join(dir, "2.0-B")); err == nil {
		t.Error("sameAPI with a missing file: expected an error")
	}
	if _, err := sameAPI(filepath.Join(dir, "2.0-B"), filepath.Join(dir, "1.0-A")); err == nil {
		t.Error("sameAPI with a missing file: expected an error")
	}
	if _, err := renderSources([]*Spec{{Tag: "1.0-A", Path: filepath.Join(dir, "1.0-A")}}, nil, "m.yml"); err == nil {
		t.Error("renderSources without commit.txt: expected an error")
	}
	writeFile(t, filepath.Join(dir, "1.0-A", commitFile), "c0ffee\n")
	if _, err := renderSources([]*Spec{{Tag: "1.0-A", Path: filepath.Join(dir, "1.0-A")}}, nil, "m.yml"); err == nil {
		t.Error("renderSources without every schema file: expected an error")
	}
}

// TestRun downloads the pinned release with no directory yet, removes a schema that adds nothing, and writes every generated file.
func TestRun(t *testing.T) {
	commits := fakeXbmc(t)
	root := t.TempDir()
	cfg := config{
		schemas:     filepath.Join(root, "tools", "kodigen", "schema"),
		matrix:      filepath.Join(root, ".github", "workflows", "kodi-contract.yml"),
		out:         filepath.Join(root, "kodirpc"),
		releases:    filepath.Join(root, "kodi", "contract_releases_gen_test.go"),
		pkg:         "kodirpc",
		releasesPkg: "kodi",
	}
	writeFile(t, cfg.matrix, testMatrix)
	omega := `{"A.Old": {"type": "method", "params": [], "returns": "string"}}`
	writeSchema(t, filepath.Join(cfg.schemas, "21.3-Omega"), "13.5.0", omega)
	writeSchema(t, filepath.Join(cfg.schemas, "21.2-Omega"), "13.5.0", omega)
	if err := os.MkdirAll(filepath.Dir(cfg.releases), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := run(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(cfg.schemas, "21.3-Omega")); err != nil {
		t.Errorf("the pinned Omega schema was removed: %v", err)
	}
	// Equal versions sort by name, so 21.2-Omega comes first and is never compared with an earlier schema.
	if _, err := os.Stat(filepath.Join(cfg.schemas, "21.2-Omega")); err != nil {
		t.Errorf("the first schema of a version is kept: %v", err)
	}
	sources, err := os.ReadFile(filepath.Join(cfg.schemas, "SOURCES.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"| `22.0rc2-Piers/` | `" + commits["22.0rc2-Piers"] + "` | 13.2000.0 | Piers |",
		"| `21.3-Omega/` | `c0ffee` | 13.5.0 | Omega |",
		"`.github/workflows/kodi-contract.yml`",
	} {
		if !strings.Contains(string(sources), want) {
			t.Errorf("SOURCES.md lacks %q:\n%s", want, sources)
		}
	}
	releases, err := os.ReadFile(cfg.releases)
	if err != nil {
		t.Fatal(err)
	}
	src := strings.Join(strings.Fields(string(releases)), " ")
	assertContains(t, src, "//go:build contract", "package kodi", `"Omega": "13.5.0", "Piers": "13.2000.0",`)
	methods, err := os.ReadFile(filepath.Join(cfg.out, "methods.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	src = strings.Join(strings.Fields(string(methods)), " ")
	assertContains(t, src, "var SchemaVersion = APIVersion{13, 2000, 0}", `MethodANew: { Name: "A.New", Since: APIVersion{13, 2000, 0}, },`)

	// A second run downloads nothing: the server is gone and the result is the same.
	xbmcRaw, xbmcRepo = "http://127.0.0.1:1", filepath.Join(root, "gone")
	if err := run(t.Context(), cfg); err != nil {
		t.Errorf("second run: %v", err)
	}
}

func TestRunPrunes(t *testing.T) {
	fakeXbmc(t)
	root := t.TempDir()
	cfg := config{
		schemas:  filepath.Join(root, "schema"),
		matrix:   filepath.Join(root, "kodi-contract.yml"),
		out:      filepath.Join(root, "kodirpc"),
		releases: filepath.Join(root, "releases_gen_test.go"),
		pkg:      "kodirpc", releasesPkg: "kodi",
	}
	writeFile(t, cfg.matrix, testMatrix)
	writeSchema(t, filepath.Join(cfg.schemas, "21.3-Omega"), "13.5.0", `{}`)
	// The same API as the pinned 21.3-Omega under a later name and no pin: it adds nothing.
	writeSchema(t, filepath.Join(cfg.schemas, "21.4-Omega"), "13.5.0", `{}`)
	if err := run(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.schemas, "21.4-Omega")); !os.IsNotExist(err) {
		t.Errorf("21.4-Omega should have been removed: %v", err)
	}
}

func TestRunErrors(t *testing.T) {
	fakeXbmc(t)
	root := t.TempDir()
	cfg := config{schemas: filepath.Join(root, "schema"), matrix: filepath.Join(root, "m.yml"), out: filepath.Join(root, "out"), releases: filepath.Join(root, "r_test.go"), pkg: "kodirpc", releasesPkg: "kodi"}
	if err := run(t.Context(), cfg); err == nil {
		t.Error("missing matrix: expected an error")
	}
	writeFile(t, cfg.matrix, `jobs: {a: {strategy: {matrix: {include: [{kodi: Q, image: "matthuisman/kodi-headless:Q-23.0@sha256:a"}]}}}}`)
	if err := run(t.Context(), cfg); err == nil {
		t.Error("unknown tag: expected an error")
	}
}

func TestRunOutputErrors(t *testing.T) {
	fakeXbmc(t)
	setup := func(t *testing.T) config {
		t.Helper()
		root := t.TempDir()
		cfg := config{schemas: filepath.Join(root, "schema"), matrix: filepath.Join(root, "m.yml"), out: filepath.Join(root, "out"), releases: filepath.Join(root, "r_test.go"), pkg: "kodirpc", releasesPkg: "kodi"}
		writeFile(t, cfg.matrix, `jobs: {a: {strategy: {matrix: {include: [{kodi: Omega, image: "matthuisman/kodi-headless:Omega-21.3@sha256:a"}]}}}}`)
		writeSchema(t, filepath.Join(cfg.schemas, "21.3-Omega"), "13.5.0", `{}`)
		return cfg
	}
	for name, breakIt := range map[string]func(t *testing.T, cfg *config){
		"incomplete schema": func(t *testing.T, cfg *config) {
			writeFile(t, filepath.Join(cfg.schemas, "21.2-Omega", "methods.json"), `{}`)
		},
		"no commit.txt":       func(_ *testing.T, cfg *config) { _ = os.Remove(filepath.Join(cfg.schemas, "21.3-Omega", commitFile)) },
		"out is a file":       func(t *testing.T, cfg *config) { writeFile(t, cfg.out, "") },
		"releases unwritable": func(_ *testing.T, cfg *config) { cfg.releases = filepath.Join(cfg.releases, "missing", "r_test.go") },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := setup(t)
			breakIt(t, &cfg)
			if err := run(t.Context(), cfg); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestDownloadBadURL(t *testing.T) {
	if _, err := download(t.Context(), http.DefaultClient, "http://bad\x7f/"); err == nil {
		t.Error("expected an error for an invalid URL")
	}
}
