package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/nightah/encore/internal/store"
)

// Open loads or generates the key and returns a ready Store.
func Open(ctx context.Context, repo Repo, opts Options) (*Store, error) {
	s := &Store{repo: repo, log: opts.Logger, keys: map[string]*keyEntry{}}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}

	key, err := suppliedKey(opts)

	switch {
	case err != nil:
		return nil, err
	case key == nil:
		if opts.DataDir == "" {
			return nil, errors.New("secrets: no key configured and no data directory for a generated key")
		}

		s.managedFile = filepath.Join(opts.DataDir, KeyFileName)

		if key, err = s.loadManagedKey(ctx); err != nil {
			return nil, err
		}
	}

	s.primary = KeyID(key)

	if err := s.addKey(key); err != nil {
		return nil, err
	}

	if err := s.recoverRotation(ctx); err != nil {
		return nil, err
	}

	if err := s.checkKeys(ctx); err != nil {
		return nil, err
	}

	return s, nil
}

// suppliedKey returns nil if neither opts.Key nor opts.KeyFile is set.
func suppliedKey(opts Options) ([]byte, error) {
	switch {
	case opts.Key != "":
		key, err := ParseKey(opts.Key)
		if err != nil {
			return nil, fmt.Errorf("secrets.key: %w", err)
		}

		return key, nil
	case opts.KeyFile != "":
		key, err := readKeyFile(opts.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("secrets.key_file: %w", err)
		}

		return key, nil
	}

	return nil, nil
}

// ReadKey returns the key Open would use without generating one or changing any file: opts.Key, opts.KeyFile, or the generated key file in opts.DataDir. It returns ErrNoKey when there is none. The caller owns the key and should clear it.
func ReadKey(opts Options) ([]byte, error) {
	key, err := suppliedKey(opts)
	if err != nil || key != nil {
		return key, err
	}

	if opts.DataDir == "" {
		return nil, ErrNoKey
	}

	path := filepath.Join(opts.DataDir, KeyFileName)

	exists, err := fileExists(path)

	switch {
	case err != nil:
		return nil, fmt.Errorf("checking key file: %w", err)
	case !exists:
		return nil, ErrNoKey
	}

	return readKeyFile(path)
}

// OpenWithKey returns a Store that uses key alone: no generated key file is managed and no interrupted rotation is recovered, so it changes nothing on disk. It suits offline tools such as restore. Store takes ownership of key; a nil key gives a Store that can open nothing.
func OpenWithKey(repo Repo, key []byte) (*Store, error) {
	s := &Store{repo: repo, log: slog.New(slog.DiscardHandler), keys: map[string]*keyEntry{}}

	if key == nil {
		return s, nil
	}

	s.primary = KeyID(key)

	if err := s.addKey(key); err != nil {
		return nil, err
	}

	return s, nil
}

// CanOpen reports whether every row decrypts under the loaded keys, returning an error naming the first that does not. Plaintexts are discarded.
func (s *Store) CanOpen(rows []store.SecretRow) error {
	for i := range rows {
		pt, err := s.open(&rows[i])
		if err != nil {
			return fmt.Errorf("secret %q: %w", rows[i].ID, err)
		}

		clear(pt)
	}

	return nil
}

func (s *Store) loadManagedKey(ctx context.Context) ([]byte, error) {
	exists, err := fileExists(s.managedFile)
	if err != nil {
		return nil, fmt.Errorf("checking key file: %w", err)
	}

	if exists {
		if info, err := os.Stat(s.managedFile); err == nil && info.Mode().Perm()&0o077 != 0 {
			s.log.Warn("secrets key file is readable by other users; chmod it to 0600", "path", s.managedFile, "mode", info.Mode().Perm().String())
		}

		s.log.Warn("secrets are encrypted with a key stored next to the database; set secrets.key or ENCORE_SECRETS_KEY_FILE to keep it elsewhere", "path", s.managedFile)

		return readKeyFile(s.managedFile)
	}

	// A missing key with existing rows means the key file was lost; generating a new one would silently orphan every secret.
	ids, err := s.repo.KeyIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("checking for existing secrets: %w", err)
	}

	if len(ids) > 0 {
		return nil, fmt.Errorf("secrets: %d key(s) are in use but %s is missing; restore it or set secrets.key", len(ids), s.managedFile)
	}

	key, err := GenerateKey()
	if err != nil {
		return nil, err
	}

	if err := writeKeyFile(s.managedFile, key); err != nil {
		clear(key)

		return nil, err
	}

	s.log.Warn("generated a secrets key next to the database; anyone who can read the data directory can decrypt stored secrets. Set secrets.key or ENCORE_SECRETS_KEY_FILE to keep it elsewhere", "path", s.managedFile)

	return key, nil
}

// recoverRotation finishes or discards a rotation of the managed key file that was interrupted between committing the database and renaming the new key into place.
func (s *Store) recoverRotation(ctx context.Context) error {
	if s.managedFile == "" {
		return nil
	}

	pending := s.managedFile + ".new"

	exists, err := fileExists(pending)
	if err != nil || !exists {
		return err
	}

	next, err := readKeyFile(pending)
	if err != nil {
		return err
	}

	nextID := KeyID(next)

	ids, err := s.repo.KeyIDs(ctx)
	if err != nil {
		clear(next)

		return fmt.Errorf("checking key usage: %w", err)
	}

	if !slices.Contains(ids, nextID) {
		clear(next)
		s.log.Warn("discarding the key of an interrupted secrets rotation that never committed", "path", pending)

		return os.Remove(pending)
	}

	if err := s.addKey(next); err != nil {
		return err
	}

	s.primary = nextID
	s.log.Warn("completing an interrupted secrets key rotation", "key_id", nextID)

	if err := os.Rename(pending, s.managedFile); err != nil {
		return fmt.Errorf("completing key rotation: %w", err)
	}

	return nil
}

func (s *Store) checkKeys(ctx context.Context) error {
	ids, err := s.repo.KeyIDs(ctx)
	if err != nil {
		return fmt.Errorf("checking key usage: %w", err)
	}

	for _, id := range ids {
		if s.keys[id] == nil {
			s.log.Error("some secrets were encrypted with a key that is not configured and cannot be read", "key_id", id, "current_key_id", s.primary)
		}
	}

	return nil
}

// addKey takes ownership of k.
func (s *Store) addKey(k []byte) error {
	if len(k) != KeySize {
		return fmt.Errorf("key must be %d bytes", KeySize)
	}

	block, err := aes.NewCipher(k)
	if err != nil {
		return fmt.Errorf("creating cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("creating GCM: %w", err)
	}

	id := KeyID(k)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.keys[id] != nil {
		clear(k)

		return nil
	}

	s.keys[id] = &keyEntry{raw: k, aead: aead}

	return nil
}

func (s *Store) removeKey(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e := s.keys[id]; e != nil {
		clear(e.raw)
		delete(s.keys, id)
	}
}

// KeyID returns the ID of the key new secrets are sealed with.
func (s *Store) KeyID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.primary
}

func (s *Store) key(id string) (*keyEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e := s.keys[id]
	if e == nil {
		return nil, fmt.Errorf("%w (key %s)", ErrUnknownKey, id)
	}

	return e, nil
}

func aad(id string) []byte { return []byte(aadPrefix + id) }

func seal(e *keyEntry, id string, plaintext []byte) (nonce, ciphertext []byte, err error) {
	nonce = make([]byte, e.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("generating nonce: %w", err)
	}

	return nonce, e.aead.Seal(nil, nonce, plaintext, aad(id)), nil
}

func (s *Store) open(row *store.SecretRow) ([]byte, error) {
	e, err := s.key(row.KeyID)
	if err != nil {
		return nil, err
	}

	if len(row.Nonce) != e.aead.NonceSize() {
		return nil, ErrDecrypt
	}

	pt, err := e.aead.Open(nil, row.Nonce, row.Ciphertext, aad(row.ID))
	if err != nil {
		return nil, ErrDecrypt
	}

	return pt, nil
}

// Put encrypts plaintext and stores it under id, replacing any previous value. The caller keeps ownership of plaintext and may clear it afterwards.
func (s *Store) Put(ctx context.Context, id string, plaintext []byte) error {
	if id == "" {
		return errors.New("secret ID must not be empty")
	}

	s.writeMu.RLock()
	defer s.writeMu.RUnlock()

	keyID := s.KeyID()

	e, err := s.key(keyID)
	if err != nil {
		return err
	}

	nonce, ct, err := seal(e, id, plaintext)
	if err != nil {
		return err
	}

	if err := s.repo.Put(ctx, store.SecretRow{ID: id, KeyID: keyID, Nonce: nonce, Ciphertext: ct}); err != nil {
		return fmt.Errorf("storing secret %q: %w", id, err)
	}

	return nil
}

// Get returns the plaintext of id, or ErrNotFound. The caller owns the returned slice and should clear it when done.
func (s *Store) Get(ctx context.Context, id string) ([]byte, error) {
	row, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	pt, err := s.open(&row)
	if err != nil {
		return nil, fmt.Errorf("secret %q: %w", id, err)
	}

	return pt, nil
}

// Delete removes id. Deleting a missing secret is not an error.
func (s *Store) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

// Rotate re-encrypts every secret under newKey in a single transaction and makes it the current key. Store takes ownership of newKey. Keys used before stay loaded until the process exits, so rows read from a snapshot taken mid-rotation still open.
//
// With a generated key file the file is replaced crash-safely. With an operator-supplied key, secrets.key must be updated to the new key before the next restart, or the rotated secrets will be unreadable.
func (s *Store) Rotate(ctx context.Context, newKey []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if len(newKey) != KeySize {
		return fmt.Errorf("key must be %d bytes", KeySize)
	}

	newID := KeyID(newKey)
	if _, err := s.key(newID); err == nil {
		return errors.New("the new key is already loaded; rotate to a fresh key")
	}

	pending := ""
	if s.managedFile != "" {
		pending = s.managedFile + ".new"
		if err := writeKeyFile(pending, newKey); err != nil {
			return err
		}
	}

	fail := func(err error) error {
		s.removeKey(newID)

		if pending != "" {
			_ = os.Remove(pending)
		}

		return err
	}

	if err := s.addKey(newKey); err != nil {
		return fail(err)
	}

	next, err := s.key(newID)
	if err != nil {
		return fail(err)
	}

	err = s.repo.Rewrite(ctx, func(row *store.SecretRow) error {
		pt, err := s.open(row)
		if err != nil {
			return fmt.Errorf("secret %q: %w", row.ID, err)
		}
		defer clear(pt)

		row.Nonce, row.Ciphertext, err = seal(next, row.ID, pt)
		row.KeyID = newID

		return err
	})
	if err != nil {
		return fail(fmt.Errorf("rotating secrets: %w", err))
	}

	s.mu.Lock()
	old := s.primary
	s.primary = newID
	s.mu.Unlock()

	if pending != "" {
		if err := os.Rename(pending, s.managedFile); err != nil {
			// The database is committed; Open finishes the rename from the .new file next start.
			return fmt.Errorf("replacing key file (it will be completed on restart): %w", err)
		}
	} else {
		s.log.Warn("secrets rotated to a new key; update secrets.key before restarting or stored secrets will be unreadable", "key_id", newID, "previous_key_id", old)
	}

	s.log.Info("rotated secrets key", "key_id", newID, "previous_key_id", old)

	return nil
}

// Close clears key material from memory. The Store must not be used afterwards.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, e := range s.keys {
		clear(e.raw)
		delete(s.keys, id)
	}

	return nil
}
