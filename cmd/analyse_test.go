package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestValidateSeriesFlags pins the flag contract: the single-demo flows stay
// valid only while --best-of is genuinely unset, an explicitly supplied
// value outside 3 and 5 is always rejected (zero included), several demos
// never infer a series, and --best-of only accepts completed-series map
// counts. All of this fails before any demo is opened or parsed.
func TestValidateSeriesFlags(t *testing.T) {
	tests := []struct {
		name      string
		bestOf    int
		bestOfSet bool
		demoCount int
		wantErr   string
	}{
		{name: "picker flow", bestOf: 0, demoCount: 0},
		{name: "single demo", bestOf: 0, demoCount: 1},
		{name: "multi demo without best-of", bestOf: 0, demoCount: 2, wantErr: "need an explicit series format"},
		{name: "explicit best-of 0 without demos", bestOf: 0, bestOfSet: true, demoCount: 0, wantErr: "invalid --best-of 0"},
		{name: "explicit best-of 0 with one demo", bestOf: 0, bestOfSet: true, demoCount: 1, wantErr: "invalid --best-of 0"},
		{name: "best-of 4", bestOf: 4, bestOfSet: true, demoCount: 3, wantErr: "invalid --best-of 4"},
		{name: "bo3 no demos", bestOf: 3, bestOfSet: true, demoCount: 0, wantErr: "2 or 3 --demo values"},
		{name: "bo3 one demo", bestOf: 3, bestOfSet: true, demoCount: 1, wantErr: "2 or 3 --demo values"},
		{name: "bo3 two demos", bestOf: 3, bestOfSet: true, demoCount: 2},
		{name: "bo3 three demos", bestOf: 3, bestOfSet: true, demoCount: 3},
		{name: "bo3 four demos", bestOf: 3, bestOfSet: true, demoCount: 4, wantErr: "2 or 3 --demo values"},
		{name: "bo5 two demos", bestOf: 5, bestOfSet: true, demoCount: 2, wantErr: "3, 4 or 5 --demo values"},
		{name: "bo5 three demos", bestOf: 5, bestOfSet: true, demoCount: 3},
		{name: "bo5 five demos", bestOf: 5, bestOfSet: true, demoCount: 5},
		{name: "bo5 six demos", bestOf: 5, bestOfSet: true, demoCount: 6, wantErr: "3, 4 or 5 --demo values"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSeriesFlags(test.bestOf, test.bestOfSet, test.demoCount)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("validateSeriesFlags(%d, %t, %d) = %v, want nil", test.bestOf, test.bestOfSet, test.demoCount, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validateSeriesFlags(%d, %t, %d) = %v, want it to contain %q",
					test.bestOf, test.bestOfSet, test.demoCount, err, test.wantErr)
			}
		})
	}
}

// TestRepeatedDemoFlagPreservesOrder parses the real analyse flag set:
// repeated --demo values must keep their command-line order, and a path
// containing a comma must stay one path (a StringSlice would split it).
func TestRepeatedDemoFlagPreservesOrder(t *testing.T) {
	t.Cleanup(func() { demoPaths = nil })
	args := []string{"--demo", "second/map2.dem", "-d", "first/map1.dem", "--demo", "dir,with,commas/map3.dem"}
	if err := analyseCmd.ParseFlags(args); err != nil {
		t.Fatalf("parsing flags: %v", err)
	}
	want := []string{"second/map2.dem", "first/map1.dem", "dir,with,commas/map3.dem"}
	if !slices.Equal(demoPaths, want) {
		t.Fatalf("demoPaths = %q, want the command-line order %q", demoPaths, want)
	}
}

// TestDemoHasherDigestsWholeStream pins the hash-while-parse contract: the
// digest always covers the complete stream, whether the consumer read all of
// it, part of it — trailing bytes are drained — or none of it.
func TestDemoHasherDigestsWholeStream(t *testing.T) {
	content := []byte("demo header, frames, and trailing bytes no parser reads")
	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])

	for _, consumed := range []int{0, 10, len(content)} {
		hasher := newDemoHasher(bytes.NewReader(content))
		if _, err := io.CopyN(io.Discard, hasher, int64(consumed)); err != nil {
			t.Fatalf("consuming %d bytes: %v", consumed, err)
		}
		digest, err := hasher.digest()
		if err != nil {
			t.Fatalf("digest after consuming %d bytes: %v", consumed, err)
		}
		if digest != want {
			t.Errorf("digest after consuming %d bytes = %s, want the full-content %s", consumed, digest, want)
		}
	}
}

// TestAnalyseAndHashDemoFileFailures pins that a missing path reports the
// open failure and that a stream failing to parse returns the parse error
// with no digest, since a digest of unparsed content identifies nothing.
func TestAnalyseAndHashDemoFileFailures(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := analyseAndHashDemoFile(context.Background(), filepath.Join(dir, "missing.dem")); err == nil ||
		!strings.Contains(err.Error(), "opening demo file") {
		t.Errorf("missing file error = %v, want an opening failure", err)
	}

	garbage := filepath.Join(dir, "garbage.dem")
	if err := os.WriteFile(garbage, []byte("not a demo"), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	demo, digest, err := analyseAndHashDemoFile(context.Background(), garbage)
	if err == nil {
		t.Fatal("analyseAndHashDemoFile parsed garbage without error")
	}
	if demo != nil || digest != "" {
		t.Errorf("failed parse returned demo %v digest %q, want neither", demo, digest)
	}
}

// TestAnalyseAndHashDemoFile parses the public mirage fixture once and
// requires the digest to equal an independent SHA-256 of the same file: the
// stream fed to the parser and the stream that was hashed are the same bytes,
// trailing ones included.
func TestAnalyseAndHashDemoFile(t *testing.T) {
	const fixture = "../analysis/testdata/mirage.dem"
	data, err := os.ReadFile(fixture)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("fixture %s not present; run make download-test-demos", fixture)
	}
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	sum := sha256.Sum256(data)

	demo, digest, err := analyseAndHashDemoFile(context.Background(), fixture)
	if err != nil {
		t.Fatalf("analyseAndHashDemoFile: %v", err)
	}
	if want := hex.EncodeToString(sum[:]); digest != want {
		t.Errorf("digest = %s, want %s", digest, want)
	}
	if demo.Map.MapName != "de_mirage" {
		t.Errorf("map name = %q, want de_mirage", demo.Map.MapName)
	}
	if demo.PlayerAggregationFacts() == nil {
		t.Error("parsed demo carries no aggregation facts")
	}
}

// TestRecordSeriesDigestRejectsDuplicates pins the in-series duplicate rule:
// the first repeated digest fails naming both supplied paths, while distinct
// digests register freely.
func TestRecordSeriesDigestRejectsDuplicates(t *testing.T) {
	paths := []string{"a/map1.dem", "b/map2.dem", "c/renamed-copy.dem"}
	seen := make(map[string]int)
	if err := recordSeriesDigest(seen, paths, 0, "1111"); err != nil {
		t.Fatalf("first digest: %v", err)
	}
	if err := recordSeriesDigest(seen, paths, 1, "2222"); err != nil {
		t.Fatalf("second digest: %v", err)
	}
	err := recordSeriesDigest(seen, paths, 2, "1111")
	if err == nil || !strings.Contains(err.Error(), "repeats the content") {
		t.Fatalf("repeated digest error = %v, want duplicate-content rejection", err)
	}
	if !strings.Contains(err.Error(), paths[0]) || !strings.Contains(err.Error(), paths[2]) {
		t.Errorf("duplicate error %q does not name both paths", err)
	}
}
