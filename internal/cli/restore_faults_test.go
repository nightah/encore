package cli

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/testkit"
)

// beforeAnswer runs do when the confirmation prompt is read, then answers yes.
type beforeAnswer struct {
	do     func()
	answer io.Reader
}

func (r *beforeAnswer) Read(p []byte) (int, error) {
	if r.answer == nil {
		r.do()
		r.answer = strings.NewReader("y\n")
	}

	return r.answer.Read(p)
}

func TestRestoreReportsSavedSettingsItCannotRead(t *testing.T) {
	f := newFixture(t, "")

	faults := testkit.NewFaults(t, f.dbPath())
	faults.Inject(testkit.Fault{Op: testkit.Read, Table: "settings"})

	err := f.restorer(io.Discard).restore(t.Context(), "")
	require.ErrorContains(t, err, "reading saved storage settings: ")
	require.ErrorContains(t, err, testkit.ReadFaultMessage)

	faults.Clear()
	f.assertUntouched(t)
}

func TestRestoreLeavesTheDatabaseWhenItCannotBeMovedAside(t *testing.T) {
	f := newFixture(t, "")

	// An earlier restore within the same second left its copy behind.
	earlier := f.dbPath() + ".before-restore-" + restoreNow.Format(beforeRestoreLayout)
	require.NoError(t, os.WriteFile(earlier, []byte("earlier"), 0o600))

	err := f.restorer(io.Discard).restore(t.Context(), "")
	require.ErrorContains(t, err, earlier+" already exists")

	assert.Equal(t, "after", marker(t, f.dbPath()), "the current database is unchanged")

	b, err := os.ReadFile(earlier)
	require.NoError(t, err)
	assert.Equal(t, "earlier", string(b), "the earlier copy is kept")

	entries, err := os.ReadDir(f.cfg.DataDir)
	require.NoError(t, err)

	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".restore-"), "the downloaded copy was removed: %s", e.Name())
	}
}

func TestRestoreStatelessNeedsATemporaryDirectory(t *testing.T) {
	f := statelessFixture(t)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))

	err := f.restorer(io.Discard).list(t.Context())
	require.ErrorIs(t, err, fs.ErrNotExist)
	assert.Equal(t, "after", f.savedMarker(t))
}

func TestRestoreStatelessReportsAFailedUpload(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	f := statelessFixture(t)
	storage := f.cfg.Storage.Local.Path

	var dirs []string

	readOnly := func() {
		require.NoError(t, filepath.WalkDir(storage, func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				dirs = append(dirs, path)
				err = os.Chmod(path, 0o500)
			}

			return err
		}))
	}

	writable := func() {
		for _, dir := range dirs {
			_ = os.Chmod(dir, 0o700)
		}
	}

	t.Cleanup(writable)

	r := f.restorer(io.Discard)
	r.yes, r.interactive = false, true
	// The storage stops taking writes while the operator confirms.
	r.in = &beforeAnswer{do: readOnly}

	err := r.restore(t.Context(), "")
	require.ErrorContains(t, err, "keeping the current database as ")

	writable()
	assert.Equal(t, "after", f.savedMarker(t), "the saved state was not replaced")
}

func TestSwapDatabaseLeavesEverythingWhenTheDatabaseCannotBeMoved(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	dir := t.TempDir()
	db := filepath.Join(dir, "encore.db")
	restored := filepath.Join(t.TempDir(), "restored")

	require.NoError(t, os.WriteFile(db, []byte("current"), 0o600))
	require.NoError(t, os.WriteFile(restored, []byte("restored"), 0o600))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	old, err := swapDatabase(db, restored, restoreNow)
	require.ErrorContains(t, err, "moving "+db+" aside: ")
	assert.Empty(t, old)

	b, err := os.ReadFile(db)
	require.NoError(t, err)
	assert.Equal(t, "current", string(b))
	assert.FileExists(t, restored)
}
