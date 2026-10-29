//go:generate go test -run TestDocsDrift -update .

package docsgen

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/cli"
	"github.com/nightah/encore/internal/skillmodel"
)

//go:embed templates
var templates embed.FS

// Generate renders every generated page. root is the repository root, from which package config's source is read for its doc comments. The result maps paths relative to root to file contents.
func Generate(root string) (map[string][]byte, error) {
	out := map[string][]byte{}

	cfg, err := ConfigReference(filepath.Join(root, "internal", "config"))
	if err != nil {
		return nil, fmt.Errorf("configuration reference: %w", err)
	}

	out[ContentDir+"/reference/configuration.md"] = cfg

	cmd, err := CLIReference(cli.NewRootCommand(io.Discard, io.Discard))
	if err != nil {
		return nil, fmt.Errorf("CLI reference: %w", err)
	}

	out[ContentDir+"/reference/cli.md"] = cmd

	spec, err := fs.ReadFile(os.DirFS(root), "internal/api/openapi.json")
	if err != nil {
		return nil, err
	}

	endpoints, err := APIEndpoints(spec)
	if err != nil {
		return nil, fmt.Errorf("API endpoints: %w", err)
	}

	out[ContentDir+"/reference/api/endpoints.md"] = endpoints

	src, err := skillmodel.Load(i18n.FS)
	if err != nil {
		return nil, err
	}

	pages, err := VoiceCommands(src, i18n.FS)
	if err != nil {
		return nil, fmt.Errorf("voice commands: %w", err)
	}

	for lang, page := range pages {
		out[ContentDir+"/voice-commands/"+lang+".md"] = page
	}

	return out, nil
}

func render(name string, data any) ([]byte, error) {
	t, err := template.New(name).Funcs(template.FuncMap{"base": func() string { return SiteBase }, "code": code, "cell": cell, "prose": prose, "join": strings.Join}).ParseFS(templates, "templates/"+name)
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// code widens the fence when s contains backticks.
func code(s string) string {
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}

	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return fence + " " + s + " " + fence
	}

	return fence + s + fence
}

// cell makes s safe inside a Markdown table cell.
func cell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}

// prose makes s safe as Markdown text: Starlight renders .md with HTML enabled, so a stray < would start a tag.
func prose(s string) string {
	return strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace(s)
}
