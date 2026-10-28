package docsgen

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// copyConfigSource copies package config's non-test Go files into a temporary directory, applying edit to each file's contents.
func copyConfigSource(t *testing.T, edit func(name, src string) string) string {
	t.Helper()

	dir := t.TempDir()
	src := filepath.Join(root, "internal", "config")

	entries, err := os.ReadDir(src)
	require.NoError(t, err)

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, e.Name()), []byte(edit(e.Name(), string(b))), 0o600))
	}

	return dir
}

func TestConfigSectionsRequireADocCommentForEveryKey(t *testing.T) {
	dir := copyConfigSource(t, func(name, src string) string {
		if name != "types.go" {
			return src
		}

		src = strings.Replace(src, "// Server configures the single HTTP listener.\n", "", 1)

		return strings.Replace(src, "\t// BasePath is the URL path prefix", "\t// The URL path prefix", 1)
	})

	_, err := ConfigSections(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.Server (server) has no doc comment")

	_, err = ConfigReference(dir)
	require.Error(t, err, "the page isn't rendered with undocumented keys")
}

func TestConfigSectionsMissingSource(t *testing.T) {
	_, err := ConfigSections(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)

	_, err = ConfigSections(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Go files")

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package config\n\ntype {"), 0o600))

	_, err = ConfigSections(dir)
	require.Error(t, err, "a file that doesn't parse")
}

func TestGenerateFailsWithoutTheConfigSource(t *testing.T) {
	_, err := Generate(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration reference")
}

func TestConfigSectionsDescribeTheDataDirDefault(t *testing.T) {
	sections, err := ConfigSections(filepath.Join(root, "internal", "config"))
	require.NoError(t, err)

	var dataDir *ConfigKey

	for _, s := range sections {
		for i := range s.Keys {
			if s.Keys[i].Key == "data_dir" {
				dataDir = &s.Keys[i]
			}
		}
	}

	require.NotNil(t, dataDir)
	assert.Empty(t, dataDir.Default, "this machine's path isn't shown")
	assert.Contains(t, dataDir.DefaultDoc, "XDG_DATA_HOME")
	assert.Equal(t, "ENCORE_DATA_DIR", dataDir.Env)
}

func TestParseDocsIgnoresTestFilesAndReadsTrailingComments(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte(`package config

// Grouped types share a declaration.
type (
	// One is the first.
	One struct {
		A string // A is a trailing comment.
		// B is a leading
		// comment over two lines.
		B, C int
	}
	Two int
)

// Alone is a type on its own.
type Alone struct{}

func f() {}
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a_test.go"), []byte("package config\n\n// Hidden is a test type.\ntype Hidden struct{}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not Go"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub.go"), 0o700))

	docs, err := parseDocs(dir)
	require.NoError(t, err)

	assert.Equal(t, "One is the first.", docs.types["One"])
	assert.Empty(t, docs.types["Two"], "a grouped type doesn't take the group's comment")
	assert.Equal(t, "Alone is a type on its own.", docs.types["Alone"])
	assert.NotContains(t, docs.types, "Hidden")
	assert.Equal(t, map[string]string{"A": "A is a trailing comment.", "B": "B is a leading comment over two lines.", "C": "B is a leading comment over two lines."}, docs.fields["One"])
}

func TestRewrite(t *testing.T) {
	t.Parallel()

	w := &walker{}
	siblings := map[string]string{"GroupsClaim": "groups_claim", "SSEKMSKeyID": "sse_kms_key_id"}

	for _, tc := range []struct {
		name, doc, want string
	}{
		{"leading name becomes the key", "AdminGroup is the group.", "`auth.oidc.admin_group` is the group."},
		{"leading name before a comma", "AdminGroup, when set, is required.", "`auth.oidc.admin_group`, when set, is required."},
		{"siblings become keys", "Read from GroupsClaim; see also SSEKMSKeyID.", "Read from `auth.oidc.groups_claim`; see also `auth.oidc.sse_kms_key_id`."},
		{"unknown identifiers stay", "Unlike OtherThing, it works.", "Unlike OtherThing, it works."},
		{"Go asides are dropped", "The mode (AuthModeLocal) applies; see ParseMode", "The mode applies"},
		{"name elsewhere is untouched", "Use AdminGroup.", "Use AdminGroup."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, w.rewrite(tc.doc, "AdminGroup", "auth.oidc.admin_group", "auth.oidc.", siblings))
		})
	}
}

func TestTypeName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		v    any
		want string
	}{
		{"", "string"},
		{0, "integer"},
		{false, "bool"},
		{[]string{}, "list of strings"},
		{[]int{}, "list of integers"},
		{new(bool), "bool"},
		{int64(0), "int64"},
	} {
		assert.Equal(t, tc.want, typeName(reflect.TypeOf(tc.v)), "%T", tc.v)
	}
}

func TestFormatDefault(t *testing.T) {
	t.Parallel()

	yes := true
	n := 3

	for _, tc := range []struct {
		v    any
		want string
	}{
		{"", `""`},
		{`say "hi"`, `"say \"hi\""`},
		{14, "14"},
		{true, "true"},
		{[]string{}, "[]"},
		{[]string{"openid", "email"}, "[openid, email]"},
		{(*bool)(nil), "unset"},
		{&yes, "true"},
		{&n, "3"},
	} {
		assert.Equal(t, tc.want, formatDefault(reflect.ValueOf(tc.v)), "%#v", tc.v)
	}
}
