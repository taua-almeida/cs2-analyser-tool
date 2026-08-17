package history

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestListMatchesEmpty pins that an empty history lists cleanly.
func TestListMatchesEmpty(t *testing.T) {
	db := openTestDB(t)
	matches, err := db.ListMatches(context.Background())
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("matches = %v, want none", matches)
	}
}

// TestListMatchesOrdering pins newest-analysis-first ordering with the full
// digest breaking timestamp ties — including the RFC3339Nano trap where
// lexicographic text order disagrees with chronological order: "…00.5Z"
// sorts before "…00Z" as text but is half a second later.
func TestListMatchesOrdering(t *testing.T) {
	db := openTestDB(t)
	wholeSecond := time.Date(2026, time.August, 2, 10, 0, 0, 0, time.UTC)
	halfPast := wholeSecond.Add(500 * time.Millisecond)

	storeFixtureMatch(t, db, testDigest(0xb), fixtureOptions{analysedAt: wholeSecond})
	storeFixtureMatch(t, db, testDigest(0xc), fixtureOptions{analysedAt: halfPast})
	// Two matches share one timestamp; their digests break the tie.
	storeFixtureMatch(t, db, testDigest(0xf), fixtureOptions{analysedAt: fixtureBase})
	storeFixtureMatch(t, db, testDigest(0xa), fixtureOptions{analysedAt: fixtureBase})

	matches, err := db.ListMatches(context.Background())
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	var order []string
	for _, match := range matches {
		order = append(order, match.SHA256)
	}
	want := []string{testDigest(0xc), testDigest(0xb), testDigest(0xa), testDigest(0xf)}
	if !slices.Equal(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}

// TestMatchByIDAcceptsFullDigestAndPrefixes pins lookup by the complete hash
// and by unique prefixes down to the eight-character minimum, uppercase
// input included.
func TestMatchByIDAcceptsFullDigestAndPrefixes(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	digest := storeFixtureMatch(t, db, testDigest(1), fixtureOptions{})
	letters := storeFixtureMatch(t, db, strings.Repeat("f", 64), fixtureOptions{})

	lookups := map[string]string{
		digest:                        digest,
		digest[:12]:                   digest,
		digest[:8]:                    digest,
		letters[:12]:                  letters,
		strings.ToUpper(letters[:12]): letters, // uppercase input normalizes
		strings.ToUpper(letters):      letters,
	}
	for id, want := range lookups {
		match, err := db.MatchByID(ctx, id)
		if err != nil {
			t.Errorf("MatchByID(%q): %v", id, err)
			continue
		}
		if match.SHA256 != want {
			t.Errorf("MatchByID(%q) = %s, want %s", id, match.SHA256, want)
		}
	}
}

// TestMatchByIDRejectsInvalidIDs pins the prefix rules: at least eight
// characters, hexadecimal only, no longer than a digest, and a clear
// not-found for unknown IDs.
func TestMatchByIDRejectsInvalidIDs(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	storeFixtureMatch(t, db, testDigest(1), fixtureOptions{})

	cases := map[string]string{
		"abc":                        "too short",
		"":                           "too short",
		"nothexnot":                  "not hexadecimal",
		"12345&78":                   "not hexadecimal",
		strings.Repeat("a", 65):      "longer than",
		"deadbeef":                   "no stored match",
		strings.Repeat("d", 64):      "no stored match",
		testDigest(1)[:8] + "ffffff": "no stored match",
	}
	for id, want := range cases {
		if _, err := db.MatchByID(ctx, id); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("MatchByID(%q) = %v, want %q", id, err, want)
		}
	}
}

// TestMatchByIDAmbiguousPrefix pins that a prefix matching several matches
// fails with a structured error carrying every candidate's full ID.
func TestMatchByIDAmbiguousPrefix(t *testing.T) {
	db := openTestDB(t)
	first := "aaaaaaaa" + strings.Repeat("1", 56)
	second := "aaaaaaaa" + strings.Repeat("2", 56)
	storeFixtureMatch(t, db, first, fixtureOptions{})
	storeFixtureMatch(t, db, second, fixtureOptions{})

	_, err := db.MatchByID(context.Background(), "aaaaaaaa")
	var ambiguous *AmbiguousMatchIDError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("err = %v, want an AmbiguousMatchIDError", err)
	}
	if ambiguous.Prefix != "aaaaaaaa" || !slices.Equal(ambiguous.Candidates, []string{first, second}) {
		t.Errorf("ambiguity = %+v, want both candidates under prefix aaaaaaaa", ambiguous)
	}
	for _, candidate := range []string{first, second} {
		if !strings.Contains(err.Error(), candidate) {
			t.Errorf("error %q does not name candidate %s", err, candidate)
		}
	}
}
