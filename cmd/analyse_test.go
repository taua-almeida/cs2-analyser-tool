package cmd

import (
	"crypto/sha256"
	"encoding/hex"
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

func TestHashDemoFilesRejectsDuplicateContent(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "map1.dem")
	second := filepath.Join(dir, "renamed-copy.dem")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("identical demo bytes"), 0o644); err != nil {
			t.Fatalf("writing fixture: %v", err)
		}
	}
	_, err := hashDemoFiles([]string{first, second})
	if err == nil || !strings.Contains(err.Error(), "repeats the content") {
		t.Fatalf("hashDemoFiles error = %v, want duplicate-content rejection", err)
	}
	if !strings.Contains(err.Error(), first) || !strings.Contains(err.Error(), second) {
		t.Errorf("duplicate error %q does not name both paths", err)
	}
}

func TestHashDemoFilesStreamsExactDigests(t *testing.T) {
	dir := t.TempDir()
	contents := []string{"map one bytes", "map two bytes"}
	paths := make([]string, len(contents))
	for i, content := range contents {
		paths[i] = filepath.Join(dir, strings.ReplaceAll(content, " ", "-"))
		if err := os.WriteFile(paths[i], []byte(content), 0o644); err != nil {
			t.Fatalf("writing fixture: %v", err)
		}
	}
	digests, err := hashDemoFiles(paths)
	if err != nil {
		t.Fatalf("hashDemoFiles: %v", err)
	}
	for i, content := range contents {
		sum := sha256.Sum256([]byte(content))
		if want := hex.EncodeToString(sum[:]); digests[i] != want {
			t.Errorf("digest[%d] = %s, want %s", i, digests[i], want)
		}
	}
}
