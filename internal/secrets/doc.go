// Package secrets encrypts credentials (Kodi passwords, storage keys, tokens) before they reach the database.
//
// Each secret is sealed with AES-256-GCM under the current key, with a fresh random nonce and the secret's ID as associated data, so a ciphertext copied onto another row fails to open. Rows record the ID of the key that sealed them, so after a rotation rows sealed under any key still held in memory remain readable.
//
// The key comes from secrets.key (or ENCORE_SECRETS_KEY_FILE), then secrets.key_file, and otherwise from a key file generated in the data directory. A generated key protects against a leaked database or backup on its own, but not against someone who can read the whole data directory.
package secrets
