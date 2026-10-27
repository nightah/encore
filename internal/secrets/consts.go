package secrets

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

// KeyFileName is the generated key's file name within the data directory.
const KeyFileName = "secrets.key"

// keyIDVersion prefixes key IDs and names the envelope format: AES-256-GCM, 12-byte random nonce, associated data aadPrefix+secret ID.
const keyIDVersion = "v1"

const aadPrefix = "encore/secrets/v1\x00"
