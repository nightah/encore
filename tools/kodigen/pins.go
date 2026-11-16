package main

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
)

// schemaFiles are the files of one schema, as Kodi keeps them in xbmc/interfaces/json-rpc/schema.
var schemaFiles = []string{"methods.json", "notifications.json", "types.json", versionFile}

// xbmcRepo and xbmcRaw locate Kodi's sources; tests point them at a local repository and server.
var (
	xbmcRepo = "https://github.com/xbmc/xbmc.git"
	xbmcRaw  = "https://raw.githubusercontent.com/xbmc/xbmc"
)

// readMatrix returns the Kodi images pinned by the include entries of every job matrix in the workflow at path, sorted by release name.
func readMatrix(path string) ([]pin, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: path comes from the -matrix flag of a dev tool.
	if err != nil {
		return nil, err
	}
	var wf struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []map[string]any `yaml:"include"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &wf); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	var pins []pin
	for _, job := range wf.Jobs {
		for _, entry := range job.Strategy.Matrix.Include {
			image, _ := entry["image"].(string)
			if !strings.HasPrefix(image, kodiImage+":") {
				continue
			}
			name, _ := entry["kodi"].(string)
			p, err := parsePin(name, image)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			pins = append(pins, p)
		}
	}
	if len(pins) == 0 {
		return nil, fmt.Errorf("%s: no matrix entry pins a %s image", path, kodiImage)
	}
	slices.SortFunc(pins, func(a, b pin) int { return strings.Compare(a.name, b.name) })
	for i := 1; i < len(pins); i++ {
		if pins[i].name == pins[i-1].name {
			return nil, fmt.Errorf("%s: release %s is pinned twice", path, pins[i].name)
		}
	}
	return pins, nil
}

// parsePin maps a matrix entry's image, as "matthuisman/kodi-headless:Piers-22.0rc2@sha256:...", to the Kodi tag it was built from, "22.0rc2-Piers".
func parsePin(name, image string) (pin, error) {
	tag, _, _ := strings.Cut(strings.TrimPrefix(image, kodiImage+":"), "@")
	codename, version, ok := strings.Cut(tag, "-")
	if name == "" || !ok || codename == "" || version == "" {
		return pin{}, fmt.Errorf("matrix entry %q: want kodi: <Release> and image: %s:<Release>-<version>, got image %q", name, kodiImage, image)
	}
	return pin{name: name, kodiTag: version + "-" + codename}, nil
}

// fetchSchema downloads the schema files of Kodi tag into dir, recording the commit the tag points at in commit.txt.
func fetchSchema(ctx context.Context, tag, dir string) error {
	commit, err := resolveTag(ctx, tag)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".fetch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	client := &http.Client{Timeout: time.Minute}
	for _, name := range schemaFiles {
		url := fmt.Sprintf("%s/%s/xbmc/interfaces/json-rpc/schema/%s", xbmcRaw, commit, name)
		b, err := download(ctx, client, url)
		if err != nil {
			return fmt.Errorf("%s: %w", tag, err)
		}
		//nolint:gosec // G306: pinned schema files are checked in and must be world-readable.
		if err := os.WriteFile(filepath.Join(tmp, name), b, 0o644); err != nil {
			return err
		}
	}
	//nolint:gosec // G306: as above.
	if err := os.WriteFile(filepath.Join(tmp, commitFile), []byte(commit+"\n"), 0o644); err != nil {
		return err
	}
	if _, err := loadSpec(tmp); err != nil {
		return fmt.Errorf("%s: downloaded schema: %w", tag, err)
	}
	//nolint:gosec // G302: the directory is checked in.
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dir)
}

// resolveTag returns the commit Kodi tag points at, peeling an annotated tag.
func resolveTag(ctx context.Context, tag string) (string, error) {
	ref := "refs/tags/" + tag
	//nolint:gosec // G204: the tag comes from the contract matrix, and git receives it as a single argument.
	out, err := exec.CommandContext(ctx, "git", "ls-remote", "--", xbmcRepo, ref, ref+"^{}").Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote %s %s: %w", xbmcRepo, ref, err)
	}
	commit := ""
	for line := range strings.Lines(string(out)) {
		sha, name, _ := strings.Cut(strings.TrimSpace(line), "\t")
		if name == ref+"^{}" || (name == ref && commit == "") {
			commit = sha
		}
	}
	if commit == "" {
		return "", fmt.Errorf("%s has no tag %s", xbmcRepo, tag)
	}
	return commit, nil
}

func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// loadChain loads every schema directory under dir, oldest API version first; directories with the same version keep their name order.
func loadChain(dir string) ([]*Spec, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var chain []*Spec
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		spec, err := loadSpec(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		chain = append(chain, spec)
	}
	if len(chain) == 0 {
		return nil, fmt.Errorf("%s holds no schema directories", dir)
	}
	slices.SortStableFunc(chain, func(a, b *Spec) int {
		return cmp.Or(compareVersions(a.Version, b.Version), strings.Compare(a.Tag, b.Tag))
	})
	return chain, nil
}

// prunable returns the schemas no pin names whose methods, types and notifications are byte-identical to the schema before them, which therefore add nothing to the availability data.
func prunable(chain []*Spec, pins []pin) ([]*Spec, error) {
	var out []*Spec
	for i := 1; i < len(chain); i++ {
		s := chain[i]
		if slices.ContainsFunc(pins, func(p pin) bool { return p.kodiTag == s.Tag }) {
			continue
		}
		same, err := sameAPI(chain[i-1].Path, s.Path)
		if err != nil {
			return nil, err
		}
		if same {
			out = append(out, s)
		}
	}
	return out, nil
}

func sameAPI(a, b string) (bool, error) {
	for _, name := range schemaFiles {
		if name == versionFile {
			continue
		}
		x, err := os.ReadFile(filepath.Join(a, name)) //nolint:gosec // G304: paths come from the -schemas flag of a dev tool.
		if err != nil {
			return false, err
		}
		y, err := os.ReadFile(filepath.Join(b, name)) //nolint:gosec // G304: as above.
		if err != nil {
			return false, err
		}
		if !bytes.Equal(x, y) {
			return false, nil
		}
	}
	return true, nil
}

// checkPinned reports a pin whose schema directory is missing from chain.
func checkPinned(chain []*Spec, pins []pin) error {
	var missing []string
	for _, p := range pins {
		if !slices.ContainsFunc(chain, func(s *Spec) bool { return s.Tag == p.kodiTag }) {
			missing = append(missing, p.kodiTag)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("no schema directory for %s", strings.Join(missing, ", "))
	}
	return nil
}

// renderSources renders SOURCES.md, which records where each pinned schema came from.
func renderSources(chain []*Spec, pins []pin, matrix string) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- Code generated by kodigen. DO NOT EDIT. -->\n\n# Pinned Kodi JSON-RPC schemas\n\n")
	fmt.Fprintf(&b, "Downloaded read-only from `xbmc/xbmc`, path `xbmc/interfaces/json-rpc/schema/`, via `%s/<commit>/xbmc/interfaces/json-rpc/schema/<file>`, into a directory named after the Kodi tag. Files are stored byte-for-byte; do not edit them.\n\n", xbmcRaw)
	fmt.Fprintf(&b, "`go generate ./internal/backend/kodi/kodirpc` downloads the schema of every Kodi release the contract matrix in `%s` pins, removes a schema no image pins whose methods, types and notifications are identical to the one before it, and rewrites this file. The oldest schema is the availability baseline and the newest the one the package is generated from.\n\n", matrix)
	b.WriteString("| Dir | Commit | API version | Pinned for |\n|-----|--------|-------------|------------|\n")
	for _, s := range chain {
		commit, err := os.ReadFile(filepath.Join(s.Path, commitFile))
		if err != nil {
			return nil, err
		}
		var names []string
		for _, p := range pins {
			if p.kodiTag == s.Tag {
				names = append(names, p.name)
			}
		}
		fmt.Fprintf(&b, "| `%s/` | `%s` | %s | %s |\n", s.Tag, strings.TrimSpace(string(commit)), s.Version, strings.Join(names, ", "))
	}
	b.WriteString("\n## SHA-256\n\n```\n")
	for _, s := range chain {
		for _, name := range slices.Sorted(slices.Values(schemaFiles)) {
			data, err := os.ReadFile(filepath.Join(s.Path, name)) //nolint:gosec // G304: as above.
			if err != nil {
				return nil, err
			}
			sum := sha256.Sum256(data)
			fmt.Fprintf(&b, "%s  %s/%s\n", hex.EncodeToString(sum[:]), s.Tag, name)
		}
	}
	b.WriteString("```\n")
	return []byte(b.String()), nil
}

// renderReleases renders the contract tests' map from each pinned release name to the JSON-RPC version its image speaks.
func renderReleases(chain []*Spec, pins []pin, pkg string) ([]byte, error) {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("//go:build contract\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString("// releaseAPIVersions maps the Kodi release names CI passes in ENCORE_CONTRACT_KODI_VERSION to the JSON-RPC API version of the image the contract matrix pins for each.\n")
	b.WriteString("var releaseAPIVersions = map[string]string{\n")
	for _, p := range pins {
		i := slices.IndexFunc(chain, func(s *Spec) bool { return s.Tag == p.kodiTag })
		if i < 0 {
			return nil, errors.New("no schema directory for " + p.kodiTag)
		}
		fmt.Fprintf(&b, "\t%q: %q,\n", p.name, chain[i].Version.String())
	}
	b.WriteString("}\n")
	return format.Source([]byte(b.String()))
}
