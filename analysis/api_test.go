// Package analysis_test exercises the public analysis API exactly as an
// importing Go module would: through exported identifiers only, with no
// access to package internals.
package analysis_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/taua-almeida/cs2-analyser-tool/analysis"
)

const mirageDemoPath = "testdata/mirage.dem"

// mirageDemo reads the pinned mirage fixture, skipping the test when the
// demo has not been downloaded (make download-test-demos).
func mirageDemo(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(mirageDemoPath)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("fixture %s not present; see testdata/README.md", mirageDemoPath)
	}
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return data
}

// closeProbe fails the test if the analysis package ever closes a
// caller-owned reader, which Analyse documents it never does.
type closeProbe struct {
	t *testing.T
	io.Reader
}

func (p *closeProbe) Close() error {
	p.t.Error("Analyse closed the caller-owned reader")
	return nil
}

// TestAnalyseReader parses a demo from an in-memory reader and requires the
// result to match AnalyseFile over the same bytes, so the reader-first entry
// point provably makes no filesystem assumptions and never closes its input.
func TestAnalyseReader(t *testing.T) {
	data := mirageDemo(t)

	fromReader, err := analysis.Analyse(context.Background(), &closeProbe{t: t, Reader: bytes.NewReader(data)})
	if err != nil {
		t.Fatalf("Analyse: %v", err)
	}
	if fromReader.Map.MapName != "de_mirage" {
		t.Errorf("map name = %q, want de_mirage", fromReader.Map.MapName)
	}
	if len(fromReader.Teams) != 2 {
		t.Errorf("teams = %d, want 2", len(fromReader.Teams))
	}
	if len(fromReader.Players) == 0 {
		t.Error("no players in analysis")
	}

	fromFile, err := analysis.AnalyseFile(context.Background(), mirageDemoPath)
	if err != nil {
		t.Fatalf("AnalyseFile: %v", err)
	}
	readerJSON, err := json.Marshal(fromReader)
	if err != nil {
		t.Fatalf("marshalling reader analysis: %v", err)
	}
	fileJSON, err := json.Marshal(fromFile)
	if err != nil {
		t.Fatalf("marshalling file analysis: %v", err)
	}
	if !bytes.Equal(readerJSON, fileJSON) {
		t.Error("Analyse over in-memory bytes differs from AnalyseFile on the same demo")
	}
}

// TestAnalyseEmptyReader pins that an ordinary empty stream comes back as
// an error carrying io.EOF's identity instead of escaping as the parser
// dependency's panic.
func TestAnalyseEmptyReader(t *testing.T) {
	_, err := analysis.Analyse(context.Background(), strings.NewReader(""))
	if err == nil {
		t.Fatal("Analyse succeeded on an empty stream")
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("err = %v, want io.EOF identity", err)
	}
}

// TestAnalyseFailingReader pins that a reader failing on its very first
// Read — before any demo byte arrives — surfaces its own error through the
// API instead of panicking.
func TestAnalyseFailingReader(t *testing.T) {
	transportErr := errors.New("transport failed")
	_, err := analysis.Analyse(context.Background(), readerFunc(func(p []byte) (int, error) {
		return 0, transportErr
	}))
	if err == nil {
		t.Fatal("Analyse succeeded on a failing stream")
	}
	if !errors.Is(err, transportErr) {
		t.Errorf("err = %v, want the reader's own error identity", err)
	}
}

// failAfterReader serves its inner reader's bytes and replaces the final
// io.EOF with a transport-style failure, modelling a stream that drops
// partway through a demo.
type failAfterReader struct {
	inner io.Reader
	err   error
}

func (f *failAfterReader) Read(p []byte) (int, error) {
	n, err := f.inner.Read(p)
	if err == io.EOF {
		err = f.err
	}
	return n, err
}

// TestAnalyseReaderFailingMidParse pins that a stream failing after parsing
// has consumed real demo frames still returns through the error result.
func TestAnalyseReaderFailingMidParse(t *testing.T) {
	data := mirageDemo(t)

	transportErr := errors.New("connection reset mid-demo")
	_, err := analysis.Analyse(context.Background(), &failAfterReader{
		inner: bytes.NewReader(data[:1<<21]),
		err:   transportErr,
	})
	if err == nil {
		t.Fatal("Analyse succeeded on a stream that died mid-demo")
	}
	if !errors.Is(err, transportErr) {
		t.Errorf("err = %v, want the reader's own error identity", err)
	}
}

func TestAnalyseFileOpenFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.dem")
	_, err := analysis.AnalyseFile(context.Background(), missing)
	if err == nil {
		t.Fatal("AnalyseFile succeeded on a missing path")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist identity", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not name the failing path", err)
	}
}

// TestAnalyseFileClosesItsFile pins AnalyseFile's ownership of the file it
// opens. Parse failures are where a leak would hide — the deferred close is
// shared with the success path — so several failed parses must leave the
// process's open-file count where it started.
func TestAnalyseFileClosesItsFile(t *testing.T) {
	garbage := filepath.Join(t.TempDir(), "garbage.dem")
	if err := os.WriteFile(garbage, []byte("not a demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := openFDs(t)
	for range 5 {
		if _, err := analysis.AnalyseFile(context.Background(), garbage); err == nil {
			t.Fatal("AnalyseFile parsed garbage without error")
		}
	}
	if after := openFDs(t); after != before {
		t.Errorf("open file descriptors went %d -> %d; AnalyseFile leaked its file", before, after)
	}
}

func openFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("cannot count open files on this OS: %v", err)
	}
	return len(entries)
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// TestAnalyseCancelledBeforeParsing pins that an already-cancelled context
// fails with the standard error identity before the reader is ever read.
func TestAnalyseCancelledBeforeParsing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reads := 0
	_, err := analysis.Analyse(ctx, readerFunc(func(p []byte) (int, error) {
		reads++
		return 0, io.EOF
	}))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled identity", err)
	}
	if reads != 0 {
		t.Errorf("reader was read %d times under a pre-cancelled context", reads)
	}
}

func TestAnalyseExpiredDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := analysis.Analyse(ctx, strings.NewReader(""))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded identity", err)
	}
}

// gateReader serves the demo's bytes while reporting when the parser first
// reads, so the test can cancel mid-demo instead of before or after it. Its
// Close doubles as the caller-ownership probe.
type gateReader struct {
	t       *testing.T
	inner   io.Reader
	started chan struct{}
	once    sync.Once
}

func (g *gateReader) Read(p []byte) (int, error) {
	g.once.Do(func() { close(g.started) })
	return g.inner.Read(p)
}

func (g *gateReader) Close() error {
	g.t.Error("Analyse closed the caller-owned reader")
	return nil
}

// TestAnalyseCancelDuringParsing cancels once parsing has demonstrably
// started and requires Analyse to abandon the demo mid-parse with the
// context's own error identity.
func TestAnalyseCancelDuringParsing(t *testing.T) {
	data := mirageDemo(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gate := &gateReader{t: t, inner: bytes.NewReader(data), started: make(chan struct{})}
	go func() {
		<-gate.started
		cancel()
	}()

	result, err := analysis.Analyse(ctx, gate)
	if err == nil {
		t.Fatalf("Analyse parsed the whole demo (%d rounds) despite the cancellation", result.Map.TotalRounds)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled identity", err)
	}
}

// stallingReader models a stream that hangs: it serves inner's bytes (if
// any), then signals the stall and blocks until the test releases it.
type stallingReader struct {
	inner   io.Reader // bytes served before the stall; nil stalls immediately
	stalled chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *stallingReader) Read(p []byte) (int, error) {
	if s.inner != nil {
		n, err := s.inner.Read(p)
		if err != io.EOF {
			return n, err
		}
		if n > 0 {
			return n, nil
		}
	}
	s.once.Do(func() { close(s.stalled) })
	<-s.release
	return 0, io.EOF
}

// cancelStalledAnalyse runs Analyse over the stalling reader, cancels the
// context once the stream has demonstrably stalled, and requires Analyse to
// return promptly with the context's error identity — a pending r.Read must
// never pin the call.
func cancelStalledAnalyse(t *testing.T, stall *stallingReader) {
	t.Helper()
	t.Cleanup(func() { close(stall.release) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := analysis.Analyse(ctx, stall)
		done <- err
	}()

	select {
	case <-stall.stalled:
	case <-time.After(10 * time.Second):
		t.Fatal("Analyse never read the stream to its stall point")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled identity", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Analyse still blocked 10s after cancellation; the stalled read pinned the parse")
	}
}

// TestAnalyseCancelUnblocksStalledRead pins that cancellation reaches even a
// stream whose very first Read never returns — an upload that sends nothing
// — instead of waiting on the read that would never finish.
func TestAnalyseCancelUnblocksStalledRead(t *testing.T) {
	cancelStalledAnalyse(t, &stallingReader{
		stalled: make(chan struct{}),
		release: make(chan struct{}),
	})
}

// TestAnalyseCancelUnblocksStallMidParse stalls the stream only after real
// demo frames were consumed, so the blocked Read sits deep inside the
// parser's refill path when the context is cancelled.
func TestAnalyseCancelUnblocksStallMidParse(t *testing.T) {
	data := mirageDemo(t)

	cancelStalledAnalyse(t, &stallingReader{
		inner:   bytes.NewReader(data[:1<<21]),
		stalled: make(chan struct{}),
		release: make(chan struct{}),
	})
}

// seriesFixtureMap builds one valid competitive map analysis from exported
// fields only, the way an importer aggregating pre-parsed data would.
func seriesFixtureMap(mapName string, teamA, teamB []uint64, scoreA, scoreB int) *analysis.MapAnalysis {
	total := scoreA + scoreB
	players := make(map[uint64]*analysis.DemoPlayer, len(teamA)+len(teamB))
	for teamID, roster := range map[int][]uint64{1: teamA, 2: teamB} {
		for _, id := range roster {
			players[id] = &analysis.DemoPlayer{
				SteamID: id,
				Name:    fmt.Sprintf("player-%d", id),
				TeamID:  teamID,
				SideStats: analysis.SideStats{
					Rounds: analysis.SideCount{Total: total, CT: total / 2, T: total - total/2},
				},
			}
		}
	}
	return &analysis.MapAnalysis{
		GameMode: "competitive",
		Map:      analysis.MapData{MapName: mapName, TotalRounds: total, RoundsWonCT: scoreA, RoundsWonT: scoreB},
		Players:  players,
		Teams: []analysis.DemoTeam{
			{TeamID: 1, Name: "Alpha", Aliases: []string{"Alpha"}, Score: scoreA, Roster: slices.Sorted(slices.Values(teamA))},
			{TeamID: 2, Name: "Bravo", Aliases: []string{"Bravo"}, Score: scoreB, Roster: slices.Sorted(slices.Values(teamB))},
		},
	}
}

var (
	rosterA = []uint64{101, 102, 103, 104, 105}
	rosterB = []uint64{201, 202, 203, 204, 205}
)

func TestBuildSeriesPublicTypes(t *testing.T) {
	inputs := []analysis.SeriesMapInput{
		{Demo: seriesFixtureMap("de_mirage", rosterA, rosterB, 13, 7), SHA256: "aaa"},
		{Demo: seriesFixtureMap("de_ancient", rosterA, rosterB, 13, 11), SHA256: "bbb"},
	}

	series, err := analysis.BuildSeries(3, inputs)
	if err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}
	if series.BestOf != 3 || series.WinnerTeamID != 1 {
		t.Errorf("best_of %d winner %d, want best_of 3 won by team 1", series.BestOf, series.WinnerTeamID)
	}
	if len(series.Teams) != 2 {
		t.Fatalf("series teams = %d, want 2", len(series.Teams))
	}
	winner := series.Teams[0]
	if winner.MapsWon != 2 || winner.RoundsWon != 26 || !slices.Equal(winner.Roster, rosterA) {
		t.Errorf("winning team = %+v, want 2 maps, 26 rounds, roster %v", winner, rosterA)
	}
	if series.Teams[1].RoundsWon != 18 {
		t.Errorf("losing team rounds = %d, want 18", series.Teams[1].RoundsWon)
	}
	if len(series.Maps) != 2 {
		t.Fatalf("series maps = %d, want 2", len(series.Maps))
	}
	for i, seriesMap := range series.Maps {
		if seriesMap.SHA256 != inputs[i].SHA256 {
			t.Errorf("maps[%d].sha256 = %q, want supplied order kept", i, seriesMap.SHA256)
		}
		if seriesMap.WinnerTeamID != 1 {
			t.Errorf("maps[%d] winner = %d, want series team 1", i, seriesMap.WinnerTeamID)
		}
		want := []analysis.SeriesTeamAssignment{{MapTeamID: 1, SeriesTeamID: 1}, {MapTeamID: 2, SeriesTeamID: 2}}
		if !slices.Equal(seriesMap.TeamAssignments, want) {
			t.Errorf("maps[%d].team_assignments = %v, want %v", i, seriesMap.TeamAssignments, want)
		}
	}
	player := series.Players[101]
	if player == nil {
		t.Fatal("player 101 missing from series aggregate")
	}
	if player.MapsPlayed != 2 || player.Rounds != 44 || player.TeamID != 1 {
		t.Errorf("player 101 = %d maps, %d rounds, team %d; want 2, 44, 1", player.MapsPlayed, player.Rounds, player.TeamID)
	}
	// Maps built by hand carry no raw aggregation facts, so the series
	// rating must be null rather than approximated.
	if player.Rating != nil {
		t.Errorf("player 101 rating = %+v, want nil without raw facts", player.Rating)
	}
}

func TestBuildSeriesAmbiguityError(t *testing.T) {
	rosterC := []uint64{301, 302, 303, 304, 305}
	rosterD := []uint64{401, 402, 403, 404, 405}
	inputs := []analysis.SeriesMapInput{
		{Demo: seriesFixtureMap("de_mirage", rosterA, rosterB, 13, 7), SHA256: "aaa"},
		{Demo: seriesFixtureMap("de_ancient", rosterC, rosterD, 13, 11), SHA256: "bbb"},
	}

	_, err := analysis.BuildSeries(3, inputs)
	var ambiguity *analysis.SeriesTeamAmbiguityError
	if !errors.As(err, &ambiguity) {
		t.Fatalf("err = %v, want a SeriesTeamAmbiguityError", err)
	}
	if len(ambiguity.Candidates) != 2 || len(ambiguity.Maps) != 2 {
		t.Errorf("ambiguity carries %d candidates over %d maps, want 2 and 2",
			len(ambiguity.Candidates), len(ambiguity.Maps))
	}
}

func TestBuildSeriesConflictError(t *testing.T) {
	mixed1 := []uint64{101, 102, 103, 201, 202}
	mixed2 := []uint64{104, 105, 203, 204, 205}
	inputs := []analysis.SeriesMapInput{
		{Demo: seriesFixtureMap("de_mirage", rosterA, rosterB, 13, 7), SHA256: "aaa"},
		{Demo: seriesFixtureMap("de_ancient", mixed1, mixed2, 13, 11), SHA256: "bbb"},
	}

	_, err := analysis.BuildSeries(3, inputs)
	var conflict *analysis.SeriesTeamConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a SeriesTeamConflictError", err)
	}
	if len(conflict.Candidates) == 0 {
		t.Fatal("conflict carries no candidate assignments")
	}
	for i, candidate := range conflict.Candidates {
		if len(candidate.Conflicts) == 0 {
			t.Errorf("candidate %d lists no conflicting SteamIDs", i)
		}
	}
}

func TestBuildSeriesFormatValidation(t *testing.T) {
	if _, err := analysis.BuildSeries(4, nil); err == nil || !strings.Contains(err.Error(), "must be 3 or 5") {
		t.Errorf("BuildSeries(4) err = %v, want the format rule", err)
	}
	one := []analysis.SeriesMapInput{{Demo: seriesFixtureMap("de_mirage", rosterA, rosterB, 13, 7), SHA256: "aaa"}}
	if _, err := analysis.BuildSeries(3, one); err == nil || !strings.Contains(err.Error(), "2 or 3 maps") {
		t.Errorf("BuildSeries(3, one map) err = %v, want the completed-series rule", err)
	}
}

func TestSelectSeriesPlayersAliasAmbiguity(t *testing.T) {
	second := seriesFixtureMap("de_ancient", rosterA, rosterB, 13, 11)
	// The same display name on two SteamIDs makes that alias ambiguous.
	second.Players[201].Name = "player-101"
	inputs := []analysis.SeriesMapInput{
		{Demo: seriesFixtureMap("de_mirage", rosterA, rosterB, 13, 7), SHA256: "aaa"},
		{Demo: second, SHA256: "bbb"},
	}
	series, err := analysis.BuildSeries(3, inputs)
	if err != nil {
		t.Fatalf("BuildSeries: %v", err)
	}

	_, err = analysis.SelectSeriesPlayers(series, []string{"player-101"})
	var aliasErr *analysis.PlayerAliasAmbiguityError
	if !errors.As(err, &aliasErr) {
		t.Fatalf("err = %v, want a PlayerAliasAmbiguityError", err)
	}
	if aliasErr.Alias != "player-101" || !slices.Equal(aliasErr.SteamIDs, []uint64{101, 201}) {
		t.Errorf("alias error = %+v, want alias player-101 with SteamIDs [101 201]", aliasErr)
	}
}

// TestPlayerAggregationFactsAPI exercises the exact-facts snapshot the way an
// importer would: parse a real demo, snapshot the facts, and require them to
// cover exactly the players, reconcile with the published derived rates, and
// stay defensive against mutation. A hand-built MapAnalysis must instead
// report its facts as unavailable.
func TestPlayerAggregationFactsAPI(t *testing.T) {
	demo, err := analysis.Analyse(context.Background(), bytes.NewReader(mirageDemo(t)))
	if err != nil {
		t.Fatalf("Analyse: %v", err)
	}

	facts := demo.PlayerAggregationFacts()
	if len(facts) != len(demo.Players) {
		t.Fatalf("facts for %d players, want %d", len(facts), len(demo.Players))
	}
	rounds := demo.Map.TotalRounds
	if rounds == 0 {
		t.Fatal("fixture reports 0 rounds")
	}
	for id, player := range demo.Players {
		playerFacts, ok := facts[id]
		if !ok {
			t.Fatalf("player %d has no facts", id)
		}
		if playerFacts.SideDamage.Total != player.AssistStats.DamageGiven {
			t.Errorf("player %d side damage total = %d, want damage given %d",
				id, playerFacts.SideDamage.Total, player.AssistStats.DamageGiven)
		}
		if kast := 100 * float64(playerFacts.KASTRounds.Total) / float64(rounds); kast != player.PlayerMapStats.KAST {
			t.Errorf("player %d KAST from facts = %v, want published %v", id, kast, player.PlayerMapStats.KAST)
		}
	}

	// Mutating the snapshot must not reach the analysis.
	for id := range facts {
		facts[id] = analysis.PlayerAggregationFacts{}
	}
	for id, fresh := range demo.PlayerAggregationFacts() {
		if fresh.SideDamage.Total != demo.Players[id].AssistStats.DamageGiven {
			t.Fatalf("player %d facts changed after mutating an earlier snapshot", id)
		}
	}

	handBuilt := seriesFixtureMap("de_mirage", rosterA, rosterB, 13, 7)
	if got := handBuilt.PlayerAggregationFacts(); got != nil {
		t.Errorf("hand-built MapAnalysis facts = %v, want nil (unavailable)", got)
	}
}

// TestExportedSurfaceHidesDemoinfocs parses the package source and fails if
// any exported type, field, function or method signature references a
// demoinfocs import. The parser dependency must stay replaceable without
// callers ever seeing it.
func TestExportedSurfaceHidesDemoinfocs(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("listing package directory: %v", err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		t.Fatal("no package source files found")
	}

	for _, file := range files {
		demoinfocsImports := map[string]bool{}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if !strings.Contains(path, "demoinfocs-golang") {
				continue
			}
			name := path[strings.LastIndex(path, "/")+1:]
			if imp.Name != nil {
				name = imp.Name.Name
			}
			demoinfocsImports[name] = true
		}
		if len(demoinfocsImports) == 0 {
			continue
		}
		flagLeaks := func(root ast.Node, what string) {
			ast.Inspect(root, func(n ast.Node) bool {
				selector, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if ident, ok := selector.X.(*ast.Ident); ok && demoinfocsImports[ident.Name] {
					t.Errorf("%s: %s references demoinfocs type %s.%s",
						fset.Position(selector.Pos()), what, ident.Name, selector.Sel.Name)
				}
				return true
			})
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() || !receiverExported(d) {
					continue
				}
				flagLeaks(d.Type, "exported function "+d.Name.Name)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if s, ok := spec.(*ast.TypeSpec); ok && s.Name.IsExported() {
						flagLeaks(s.Type, "exported type "+s.Name.Name)
					}
				}
			}
		}
	}
}

// receiverExported reports whether a method's receiver type is itself part
// of the public surface; functions have no receiver and always count.
func receiverExported(d *ast.FuncDecl) bool {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return true
	}
	expr := d.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	return ok && ident.IsExported()
}
