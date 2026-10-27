package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/nightah/encore/internal/api"
	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/statestore"
	"github.com/nightah/encore/internal/store"
)

func newRestoreCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore [key]",
		Short: "Restore the database from a backup",
		Long: `Restore the database from a backup in the configured storage. Stop Encore first; restore refuses to run while Encore is using the same data directory.

The backup is downloaded and checked before anything is replaced: it must be a sound SQLite database, its schema must not be newer than this Encore's, and the configured secrets key must decrypt its secrets. The current database is then moved aside to encore.db.before-restore-<time> and the backup takes its place.

In stateless mode the backup is uploaded as the saved state instead, keeping the current state as the previous copy.`,
		Example: `  encore restore --list
  encore restore --latest
  encore restore Encore/encore-20261026T030000.000Z.db.gz --yes`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			list, _ := cmd.Flags().GetBool("list")
			latest, _ := cmd.Flags().GetBool("latest")
			yes, _ := cmd.Flags().GetBool("yes")

			key := ""
			if len(args) == 1 {
				key = args[0]
			}

			switch {
			case list && (latest || key != ""):
				return errors.New("--list takes no backup key and can't be combined with --latest")
			case !list && latest == (key != ""):
				return errors.New("name a backup key, or pass --latest or --list")
			}

			cfg, err := loadConfig(cmd)
			if err != nil {
				return err
			}

			r := &restorer{
				cfg:         cfg,
				in:          cmd.InOrStdin(),
				out:         cmd.OutOrStdout(),
				interactive: isTerminal(cmd.InOrStdin()),
				yes:         yes,
				now:         time.Now,
			}

			if list {
				return r.list(cmd.Context())
			}

			return r.restore(cmd.Context(), key)
		},
	}

	cmd.Flags().Bool("list", false, "list the backups in the configured storage, newest first")
	cmd.Flags().Bool("latest", false, "restore the newest backup")
	cmd.Flags().BoolP("yes", "y", false, "restore without asking for confirmation")

	return cmd
}

// close releases the lock and clears the key.
func (s *session) close() {
	if s.lock != nil {
		_ = s.lock.Close()
	}

	clear(s.key)
}

// open takes the data directory lock (or, in stateless mode, checks no Encore is running against the state store), then works out the effective storage settings the way the server does.
func (r *restorer) open(ctx context.Context) (_ *session, err error) {
	s := &session{}

	defer func() {
		if err != nil {
			s.close()
		}
	}()

	s.key, err = secrets.ReadKey(secrets.Options{Key: r.cfg.Secrets.Key, KeyFile: r.cfg.Secrets.KeyFile, DataDir: r.cfg.DataDir})
	if errors.Is(err, secrets.ErrNoKey) {
		s.key, err = nil, nil
	}

	if err != nil {
		return nil, err
	}

	dbPath := store.DefaultPath(r.cfg.DataDir)

	if r.cfg.Stateless {
		if s.state, err = blobstore.Open(r.cfg.Storage, r.cfg.DataDir); err != nil {
			return nil, fmt.Errorf("opening state storage: %w", err)
		}

		if err := r.checkStateOwner(ctx, s.state); err != nil {
			return nil, err
		}

		// There is no local database: read the saved settings from a copy of the saved state.
		dir, err := os.MkdirTemp("", "encore-restore-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)

		dbPath = store.DefaultPath(dir)

		if _, err := statestore.Restore(ctx, s.state, dbPath); err != nil {
			return nil, err
		}
	} else if s.lock, err = lockDataDir(r.cfg.DataDir); err != nil {
		if errors.Is(err, errLocked) {
			return nil, fmt.Errorf("%w; stop it before restoring", err)
		}

		return nil, err
	}

	eff, err := effectiveConfig(ctx, r.cfg, dbPath, s.key)
	if err != nil {
		return nil, err
	}

	if s.backups, err = blobstore.Open(eff.Storage, eff.DataDir); err != nil {
		return nil, fmt.Errorf("opening storage: %w", err)
	}

	s.prefix = eff.Storage.Backups.Prefix

	return s, nil
}

// effectiveConfig merges the overrides saved in the database at dbPath into cfg, as the web interface's settings do. Without a database it returns cfg.
func effectiveConfig(ctx context.Context, cfg *config.Config, dbPath string, key []byte) (*config.Config, error) {
	if _, err := os.Stat(dbPath); errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}

	db, err := store.Open(ctx, store.Options{Path: dbPath, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		return nil, fmt.Errorf("opening the current database for its storage settings: %w", err)
	}
	defer db.Close()

	sec, err := secrets.OpenWithKey(db.SecretRows(), bytes.Clone(key))
	if err != nil {
		return nil, err
	}
	defer sec.Close()

	eff, err := api.NewPrefs(cfg, db, sec).Effective(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading saved storage settings: %w", err)
	}

	return eff, nil
}

// checkStateOwner refuses when the owner marker shows a stateless Encore saved a heartbeat recently enough that it may still be running.
func (r *restorer) checkStateOwner(ctx context.Context, state blobstore.Store) error {
	owner, seen, active, err := statestore.ActiveOwner(ctx, state, r.now())
	if err != nil {
		return err
	}

	if active {
		return fmt.Errorf("the Encore on %q was running against this storage at %s and may still be; stop it before restoring, or wait a few minutes if it already stopped uncleanly", owner, seen.Local().Format(time.DateTime))
	}

	return nil
}

func (r *restorer) list(ctx context.Context) error {
	s, err := r.open(ctx)
	if err != nil {
		return err
	}
	defer s.close()

	backups, err := blobstore.ListBackups(ctx, s.backups, s.prefix)
	if err != nil {
		return fmt.Errorf("listing backups: %w", err)
	}

	if len(backups) == 0 {
		_, err := fmt.Fprintf(r.out, "No backups under %q in %s storage.\n", s.prefix, r.backend())

		return err
	}

	tw := tabwriter.NewWriter(r.out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "KEY\tTIME\tSIZE")

	for _, b := range backups {
		t, _ := blobstore.BackupTime(b.Key)
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", b.Key, t.Local().Format(time.DateTime), formatSize(b.Size))
	}

	return tw.Flush()
}

func (r *restorer) backend() string {
	if r.cfg.Storage.Backend == "" {
		return config.BackendLocal
	}

	return r.cfg.Storage.Backend
}

// An empty key restores the newest backup.
func (r *restorer) restore(ctx context.Context, key string) error {
	if !r.yes && !r.interactive {
		return errors.New("refusing to restore without confirmation: input is not a terminal; pass --yes to restore anyway")
	}

	s, err := r.open(ctx)
	if err != nil {
		return err
	}
	defer s.close()

	backup, err := findBackup(ctx, s, key)
	if err != nil {
		return err
	}

	// Stage next to the database so the final rename is atomic; in stateless mode the data directory may be read-only, and the file is only uploaded.
	stage := r.cfg.DataDir
	if r.cfg.Stateless {
		if stage, err = os.MkdirTemp("", "encore-restore-"); err != nil {
			return err
		}
		defer os.RemoveAll(stage)
	}

	_, _ = fmt.Fprintf(r.out, "Downloading %s (%s)...\n", backup.Key, formatSize(backup.Size))

	tmp, err := blobstore.FetchBackup(ctx, s.backups, backup.Key, stage)
	if err != nil {
		return err
	}
	defer removeDB(tmp)

	if err := verifyBackup(ctx, tmp, s.key); err != nil {
		return fmt.Errorf("backup %s was not restored, and the current database is unchanged: %w", backup.Key, err)
	}

	taken, _ := blobstore.BackupTime(backup.Key)
	_, _ = fmt.Fprintf(r.out, "Verified %s, taken %s.\n", backup.Key, taken.Local().Format(time.DateTime))

	target := store.DefaultPath(r.cfg.DataDir)
	if r.cfg.Stateless {
		target = fmt.Sprintf("the saved state (%s) in %s storage", statestore.KeyDatabase, r.backend())
	}

	if !r.yes {
		ok, err := r.confirm(fmt.Sprintf("Replace %s with this backup? [y/N] ", target))
		if err != nil {
			return err
		}

		if !ok {
			return errors.New("restore cancelled; nothing was changed")
		}
	}

	if r.cfg.Stateless {
		return r.upload(ctx, s, tmp, backup.Key)
	}

	old, err := swapDatabase(target, tmp, r.now())
	if err != nil {
		return err
	}

	if old != "" {
		_, _ = fmt.Fprintf(r.out, "Moved the previous database to %s.\n", old)
	}

	_, _ = fmt.Fprintf(r.out, "Restored %s to %s. Start Encore to use it.\n", backup.Key, target)

	return nil
}

func (r *restorer) upload(ctx context.Context, s *session, path, key string) error {
	// Check again in case an Encore started while we were downloading.
	if err := r.checkStateOwner(ctx, s.state); err != nil {
		return err
	}

	if err := statestore.Upload(ctx, s.state, path); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(r.out, "Uploaded %s as %s; the previous saved state is kept as %s. Start Encore to use it.\n", key, statestore.KeyDatabase, statestore.KeyPrevious)

	return nil
}

func (r *restorer) confirm(prompt string) (bool, error) {
	if _, err := fmt.Fprint(r.out, prompt); err != nil {
		return false, err
	}

	line, err := bufio.NewReader(r.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	}

	return false, nil
}

// findBackup returns the newest backup when key is empty, otherwise the backup whose key or file name is key.
func findBackup(ctx context.Context, s *session, key string) (blobstore.Object, error) {
	backups, err := blobstore.ListBackups(ctx, s.backups, s.prefix)
	if err != nil {
		return blobstore.Object{}, fmt.Errorf("listing backups: %w", err)
	}

	if key == "" {
		if len(backups) == 0 {
			return blobstore.Object{}, fmt.Errorf("there are no backups under %q", s.prefix)
		}

		return backups[0], nil
	}

	for _, b := range backups {
		if b.Key == key || b.Key[strings.LastIndexByte(b.Key, '/')+1:] == key {
			return b, nil
		}
	}

	return blobstore.Object{}, fmt.Errorf("no backup %q under %q; encore restore --list shows them", key, s.prefix)
}

// verifyBackup checks the database file at path can safely replace the current one: it is a sound Encore database, no newer than this build's schema, and key decrypts its secrets.
func verifyBackup(ctx context.Context, path string, key []byte) error {
	info, err := store.InspectFile(ctx, path)
	if err != nil {
		return err
	}

	latest, err := store.SchemaVersion()
	if err != nil {
		return err
	}

	switch {
	case info.SchemaVersion == 0:
		return errors.New("it is not an Encore database (it has no schema version)")
	case info.SchemaVersion > latest:
		return fmt.Errorf("its schema version %d is newer than this Encore's %d; restore it with the Encore version that made it or a newer one", info.SchemaVersion, latest)
	}

	if len(info.Secrets) == 0 {
		return nil
	}

	if key == nil {
		return fmt.Errorf("it holds %d encrypted secrets but no secrets key is configured; set secrets.key, or put the %s file it was made with in the data directory", len(info.Secrets), secrets.KeyFileName)
	}

	sec, err := secrets.OpenWithKey(nil, bytes.Clone(key))
	if err != nil {
		return err
	}
	defer sec.Close()

	if err := sec.CanOpen(info.Secrets); err != nil {
		return fmt.Errorf("the configured secrets key cannot decrypt its secrets (%w); configure the key it was made with", err)
	}

	return nil
}

// swapDatabase moves the database at dbPath and its -wal and -shm files aside to dbPath.before-restore-<now>, then renames restored into place. It returns where the old database went, or "" if there was none. If the rename fails, the old files are put back.
func swapDatabase(dbPath, restored string, now time.Time) (string, error) {
	old := dbPath + ".before-restore-" + now.UTC().Format(beforeRestoreLayout)

	// The -wal file goes first: if this is interrupted, a database left without its WAL loses recent writes but is never paired with another database's WAL.
	suffixes := []string{"-wal", "-shm", ""}

	for _, sfx := range suffixes {
		if _, err := os.Lstat(old + sfx); err == nil {
			return "", fmt.Errorf("%s already exists; try again in a second", old+sfx)
		}
	}

	var moved []string

	undo := func(err error) (string, error) {
		for _, sfx := range moved {
			if uerr := os.Rename(old+sfx, dbPath+sfx); uerr != nil {
				err = errors.Join(err, fmt.Errorf("putting %s back: %w", dbPath+sfx, uerr))
			}
		}

		return "", err
	}

	for _, sfx := range suffixes {
		err := os.Rename(dbPath+sfx, old+sfx)

		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return undo(fmt.Errorf("moving %s aside: %w", dbPath+sfx, err))
		}

		moved = append(moved, sfx)
	}

	if err := os.Rename(restored, dbPath); err != nil {
		return undo(fmt.Errorf("moving the restored database into place: %w", err))
	}

	syncDir(filepath.Dir(dbPath))

	if len(moved) == 0 {
		return "", nil
	}

	return old, nil
}

// syncDir flushes a directory's entries so renames survive a crash. It is best effort: not every platform can sync a directory.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil { //nolint:gosec // the data directory
		_ = d.Sync()
		_ = d.Close()
	}
}

// removeDB removes a database file and any -wal or -shm file beside it.
func removeDB(path string) {
	for _, sfx := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(path + sfx)
	}
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}

	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

func formatSize(n int64) string {
	const unit = 1024

	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
