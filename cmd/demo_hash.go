package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"

	"github.com/taua-almeida/cs2-analyser-tool/analysis"
)

// demoHasher streams a reader through SHA-256 while a consumer reads it, so
// the digest identifies exactly the bytes the consumer was given rather than
// a second read of a file that could have changed in between. Nothing is
// buffered beyond the consumer's own reads, so a demo is never held in
// memory whole.
type demoHasher struct {
	tee  io.Reader
	hash hash.Hash
}

func newDemoHasher(r io.Reader) *demoHasher {
	digest := sha256.New()
	return &demoHasher{tee: io.TeeReader(r, digest), hash: digest}
}

// Read serves the underlying stream, feeding every byte through the digest.
func (d *demoHasher) Read(p []byte) (int, error) {
	return d.tee.Read(p)
}

// digest drains whatever the consumer left unread — a demo parser stops at
// the final frame, not at EOF — and returns the lowercase hex SHA-256 of the
// complete stream.
func (d *demoHasher) digest() (string, error) {
	if _, err := io.Copy(io.Discard, d.tee); err != nil {
		return "", err
	}
	return hex.EncodeToString(d.hash.Sum(nil)), nil
}

// analyseAndHashDemoFile opens the demo at path once and parses it while the
// same byte stream runs through SHA-256, returning the analysis together
// with the digest of the exact bytes analysis.Analyse consumed. Trailing
// bytes the parser left unread are drained into the digest before it is
// finalized. The single-map and series flows both identify a demo's stored
// match by this digest.
func analyseAndHashDemoFile(ctx context.Context, path string) (*analysis.MapAnalysis, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("opening demo file: %w", err)
	}
	defer file.Close()

	hasher := newDemoHasher(file)
	demo, err := analysis.Analyse(ctx, hasher)
	if err != nil {
		return nil, "", err
	}
	digest, err := hasher.digest()
	if err != nil {
		return nil, "", fmt.Errorf("hashing %s: %w", path, err)
	}
	return demo, digest, nil
}
