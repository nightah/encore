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

		return strings.Replace(src, "\t// Address is the listen address, e.g. \":8080\".\n", "", 1)
	})

	_, err := ConfigSections(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.Server (server) has no doc comment")
	assert.Contains(t, err.Error(), "config.Server.Address (server.address) has no doc comment")

	_, err = ConfigReference(dir)
	require.Error(t, err, "the page isn't rendered with undocumented keys")
}

func TestConfigSectionsMissingSource(t *testing.T) {
	testCases := []struct {
		name string
		dir  func(t *testing.T) string
		err  string
	}{
		{name: "ShouldRejectMissingDirectory", dir: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") }},
		{name: "ShouldRejectDirectoryWithoutGoFiles", dir: func(t *testing.T) string { return t.TempDir() }, err: "no Go files"},
		{
			name: "ShouldRejectFileThatDoesNotParse",
			dir: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package config\n\ntype {"), 0o600))

				return dir
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ConfigSections(tc.dir(t))
			require.Error(t, err)

			if tc.err != "" {
				assert.Contains(t, err.Error(), tc.err)
			}
		})
	}
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

func TestWalkSkipsFieldsWithoutAKey(t *testing.T) {
	t.Parallel()

	type section struct {
		Address string `koanf:"address"`
		cache   string
		Derived string
	}

	w := &walker{docs: goDocs{fields: map[string]map[string]string{"section": {"Address": "Address is where to listen."}}}}

	var sec ConfigSection

	w.walk(&sec, reflect.TypeFor[section](), reflect.ValueOf(section{Address: ":8080", cache: "x", Derived: "y"}), "server", false)

	require.Empty(t, w.errs)
	require.Len(t, sec.Keys, 1, "fields without a koanf tag aren't configuration")
	assert.Equal(t, "server.address", sec.Keys[0].Key)
	assert.Equal(t, `":8080"`, sec.Keys[0].Default)
}

func TestRewrite(t *testing.T) {
	t.Parallel()

	w := &walker{}
	siblings := map[string]string{"GroupsClaim": "groups_claim", "SSEKMSKeyID": "sse_kms_key_id"}

	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldTurnLeadingNameIntoTheKey", have: "AdminGroup is the group.", expected: "`auth.oidc.admin_group` is the group."},
		{name: "ShouldTurnLeadingNameBeforeACommaIntoTheKey", have: "AdminGroup, when set, is required.", expected: "`auth.oidc.admin_group`, when set, is required."},
		{name: "ShouldTurnSiblingsIntoKeys", have: "Read from GroupsClaim; see also SSEKMSKeyID.", expected: "Read from `auth.oidc.groups_claim`; see also `auth.oidc.sse_kms_key_id`."},
		{name: "ShouldKeepUnknownIdentifiers", have: "Unlike OtherThing, it works.", expected: "Unlike OtherThing, it works."},
		{name: "ShouldDropGoAsides", have: "The mode (AuthModeLocal) applies; see ParseMode", expected: "The mode applies"},
		{name: "ShouldNotTouchNameElsewhere", have: "Use AdminGroup.", expected: "Use AdminGroup."},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, w.rewrite(tc.have, "AdminGroup", "auth.oidc.admin_group", "auth.oidc.", siblings))
		})
	}
}

func TestTypeName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		have     any
		expected string
	}{
		{name: "ShouldNameString", have: "", expected: "string"},
		{name: "ShouldNameIntAsInteger", have: 0, expected: "integer"},
		{name: "ShouldNameBool", have: false, expected: "bool"},
		{name: "ShouldNameListOfStrings", have: []string{}, expected: "list of strings"},
		{name: "ShouldNameListOfIntegers", have: []int{}, expected: "list of integers"},
		{name: "ShouldNamePointerByItsElement", have: new(bool), expected: "bool"},
		{name: "ShouldNameOtherKindsByGoName", have: int64(0), expected: "int64"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, typeName(reflect.TypeOf(tc.have)))
		})
	}
}

func TestFormatDefault(t *testing.T) {
	t.Parallel()

	yes := true
	n := 3

	testCases := []struct {
		name     string
		have     any
		expected string
	}{
		{name: "ShouldQuoteEmptyString", have: "", expected: `""`},
		{name: "ShouldQuoteAndEscapeString", have: `say "hi"`, expected: `"say \"hi\""`},
		{name: "ShouldFormatInteger", have: 14, expected: "14"},
		{name: "ShouldFormatBool", have: true, expected: "true"},
		{name: "ShouldFormatEmptyList", have: []string{}, expected: "[]"},
		{name: "ShouldFormatList", have: []string{"openid", "email"}, expected: "[openid, email]"},
		{name: "ShouldFormatNilPointerAsUnset", have: (*bool)(nil), expected: "unset"},
		{name: "ShouldFormatBoolPointer", have: &yes, expected: "true"},
		{name: "ShouldFormatIntPointer", have: &n, expected: "3"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, formatDefault(reflect.ValueOf(tc.have)))
		})
	}
}
