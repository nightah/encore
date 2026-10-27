package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// verifyChecksum checks data against the first entry of a sha256sum-format file ("<hex>  <name>").
func verifyChecksum(data []byte, sumPath string) error {
	b, err := os.ReadFile(sumPath) //nolint:gosec // G304: path comes from the -sum flag of a dev tool.
	if err != nil {
		return err
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return fmt.Errorf("%s: empty checksum file", sumPath)
	}
	want := strings.ToLower(fields[0])
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("spec checksum mismatch: got sha256 %s, %s pins %s", got, sumPath, want)
	}
	return nil
}
