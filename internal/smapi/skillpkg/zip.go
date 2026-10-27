package skillpkg

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"time"
)

// epoch is the timestamp of every zip entry, so identical content zips to identical bytes.
var epoch = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// Zip returns the package as SMAPI imports it.
func (p *Package) Zip() ([]byte, error) {
	var buf bytes.Buffer
	if err := writeZip(&buf, p.Files); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Export writes the package plus an ask-resources.json, for users who deploy with the ASK CLI instead: unzip it and run `ask deploy`.
func (p *Package) Export(w io.Writer) error {
	files := append([]File{{Name: "ask-resources.json", Data: []byte(askResources)}}, p.Files...)

	return writeZip(w, files)
}

func writeZip(w io.Writer, files []File) error {
	zw := zip.NewWriter(w)

	for _, f := range files {
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: epoch})
		if err != nil {
			return fmt.Errorf("skillpkg: adding %s: %w", f.Name, err)
		}

		if _, err := fw.Write(f.Data); err != nil {
			return fmt.Errorf("skillpkg: writing %s: %w", f.Name, err)
		}
	}

	if err := zw.Close(); err != nil {
		return fmt.Errorf("skillpkg: finishing zip: %w", err)
	}

	return nil
}

// hashFiles hashes names and contents with length prefixes, so no two different packages collide by concatenation.
func hashFiles(files []File) string {
	h := sha256.New()

	var n [8]byte

	for _, f := range files {
		for _, b := range [][]byte{[]byte(f.Name), f.Data} {
			binary.BigEndian.PutUint64(n[:], uint64(len(b)))
			h.Write(n[:])
			h.Write(b)
		}
	}

	return hex.EncodeToString(h.Sum(nil))
}
