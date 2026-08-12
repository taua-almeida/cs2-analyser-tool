package demoparser

import (
	"fmt"
	"maps"
	"os"
	"sort"
	"testing"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	events "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

const evaluateHLTVTradeModelsEnv = "HLTV_EVALUATE_TRADE_MODELS"

type hltvTradeSelection uint8

const (
	hltvTradeEarliest hltvTradeSelection = iota
	hltvTradeNearest
	hltvTradeMultiple
)

type hltvTradeBoundary uint8

const (
	hltvTradeExactTime hltvTradeBoundary = iota
	hltvTradeTimeResolution
	hltvTradeNormalizedTicks
)

type hltvTradeModel struct {
	selection hltvTradeSelection
	boundary  hltvTradeBoundary
	postRound bool
}

func (model hltvTradeModel) String() string {
	selections := []string{"earliest", "nearest", "multiple"}
	boundaries := []string{"exact-time", "time-plus-resolution", "normalized-ticks"}
	return fmt.Sprintf("%s/%s/post=%t", selections[model.selection], boundaries[model.boundary], model.postRound)
}

type hltvModelFixture struct {
	oracle hltvOracle
	maps   map[int][]hltvTradeRoundTrace
}

// hltvTradeRoundTrace contains only the facts consumed by the model matrix;
// the richer evidence trace deliberately remains a separate diagnostic type.
type hltvTradeRoundTrace struct {
	tickTime     time.Duration
	participants map[uint64]common.Team
	kills        []hltvKillTrace
	enemyKills   map[uint64]int
	assists      map[uint64]assistFacts
	finalAlive   map[uint64]bool
}

// TestEvaluateHLTVTradeModels is an opt-in diagnostic matrix. It evaluates
// semantic models from the same recorded events; it never mutates production
// state or treats aggregate parity as permission to fit a custom cutoff.
func TestEvaluateHLTVTradeModels(t *testing.T) {
	if os.Getenv(evaluateHLTVTradeModelsEnv) == "" {
		t.Skipf("set %s=1 with the HLTV demo directories to evaluate trade models", evaluateHLTVTradeModelsEnv)
	}

	originalDirs := configuredDemoDirs(os.Getenv(hltvDemoDirEnv))
	extraDirs := configuredDemoDirs(os.Getenv(hltvExtraDemoDirsEnv))
	if len(originalDirs) == 0 || len(extraDirs) == 0 {
		t.Fatalf("%s and %s must both be configured", hltvDemoDirEnv, hltvExtraDemoDirsEnv)
	}

	fixtures := make([]hltvModelFixture, len(hltvOracleSpecs))
	oracles := make([]hltvOracle, len(hltvOracleSpecs))
	for i, spec := range hltvOracleSpecs {
		oracle := loadHLTVOracle(t, spec.Path)
		oracles[i] = oracle
		fixtures[i] = hltvModelFixture{oracle: oracle, maps: make(map[int][]hltvTradeRoundTrace)}
		dirs := originalDirs
		if spec.Extra {
			dirs = extraDirs
		}
		for _, expectedMap := range oracle.Maps {
			path, err := findDemo(dirs, expectedMap.DemoFile)
			if err != nil {
				t.Fatalf("locating %s: %v", expectedMap.DemoFile, err)
			}
			if err := verifyDemoChecksum(path, expectedMap.DemoSHA256); err != nil {
				t.Fatalf("verifying %s: %v", expectedMap.DemoFile, err)
			}
			traces := collectHLTVRoundTraces(t, path)
			if len(traces) != expectedMap.Rounds {
				t.Fatalf("%s collected %d scored rounds, want %d", expectedMap.DemoFile, len(traces), expectedMap.Rounds)
			}
			fixtures[i].maps[expectedMap.MapID] = traces
		}
	}
	validateHLTVOracleSet(t, hltvOracleSpecs, oracles)

	for _, model := range allHLTVTradeModels() {
		originalMatches, originalRows := 0, 0
		extraMatches, extraRows := 0, 0
		var mismatches []string
		for i, fixture := range fixtures {
			for _, expectedMap := range fixture.oracle.Maps {
				traces := fixture.maps[expectedMap.MapID]
				gotByPlayer := modelKASTRounds(traces, model)
				for _, expectedPlayer := range expectedMap.Players {
					got := gotByPlayer[expectedPlayer.SteamID]
					want := kastRounds(expectedPlayer.KASTPercent, expectedMap.Rounds)
					if hltvOracleSpecs[i].Extra {
						extraRows++
						if got == want {
							extraMatches++
						}
					} else {
						originalRows++
						if got == want {
							originalMatches++
						}
					}
					if got != want {
						mismatches = append(mismatches, fmt.Sprintf("%s/%d/%s=%d:%d",
							fixture.oracle.FixtureID, expectedMap.MapID, expectedPlayer.HLTVName, got, want))
					}
				}
			}
		}
		sort.Strings(mismatches)
		t.Logf("model=%s original=%d/%d extra=%d/%d mismatches=%v",
			model, originalMatches, originalRows, extraMatches, extraRows, mismatches)
	}
}

func allHLTVTradeModels() []hltvTradeModel {
	var models []hltvTradeModel
	for _, selection := range []hltvTradeSelection{hltvTradeEarliest, hltvTradeNearest, hltvTradeMultiple} {
		for _, boundary := range []hltvTradeBoundary{hltvTradeExactTime, hltvTradeTimeResolution, hltvTradeNormalizedTicks} {
			for _, postRound := range []bool{false, true} {
				models = append(models, hltvTradeModel{selection: selection, boundary: boundary, postRound: postRound})
			}
		}
	}
	return models
}

func collectHLTVRoundTraces(t *testing.T, demoPath string) []hltvTradeRoundTrace {
	t.Helper()
	file, err := os.Open(demoPath)
	if err != nil {
		t.Fatalf("opening trace demo: %v", err)
	}
	defer file.Close()

	parser := demoinfocs.NewParser(file)
	defer parser.Close()
	analysis := newAnalyser(parser)
	analysis.registerHandlers()

	var traces []hltvTradeRoundTrace
	var current *hltvTradeRoundTrace
	var startedAtRound int
	finishCurrent := func(totalRounds int) {
		if current == nil {
			return
		}
		if totalRounds > startedAtRound {
			traces = append(traces, *current)
		}
		current = nil
	}
	snapshot := func() {
		if current == nil {
			return
		}
		current.participants = maps.Clone(analysis.tracker.startAlive)
		current.enemyKills = maps.Clone(analysis.tracker.enemyKills)
		current.assists = maps.Clone(analysis.tracker.assists)
		current.finalAlive = aliveSet(analysis.tracker.alive)
	}

	parser.RegisterEventHandler(func(events.RoundStart) {
		state := parser.GameState()
		finishCurrent(state.TotalRoundsPlayed())
		startedAtRound = state.TotalRoundsPlayed()
		current = &hltvTradeRoundTrace{
			tickTime: parser.TickTime(),
		}
		snapshot()
	})
	parser.RegisterEventHandler(func(events.RoundFreezetimeEnd) { snapshot() })
	parser.RegisterEventHandler(func(event events.Kill) {
		if current == nil || event.Victim == nil {
			return
		}
		current.kills = append(current.kills, hltvKillTrace{
			frame:      parser.CurrentFrame(),
			tick:       parser.GameState().IngameTick(),
			at:         parser.CurrentTime(),
			killer:     trackerID(event.Killer),
			killerTeam: tracePlayerTeam(event.Killer),
			victim:     trackerID(event.Victim),
			victimTeam: event.Victim.Team,
			postRound:  analysis.tracker.isPostRound(),
		})
		snapshot()
	})
	parser.RegisterEventHandler(func(events.PlayerDisconnected) { snapshot() })
	parser.RegisterEventHandler(func(events.RoundEnd) { snapshot() })
	parser.RegisterEventHandler(func(events.RoundEndOfficial) { snapshot() })

	if err := parser.ParseToEnd(); err != nil {
		t.Fatalf("parsing trace demo: %v", err)
	}
	snapshot()
	finishCurrent(parser.GameState().TotalRoundsPlayed())
	return traces
}

func modelKASTRounds(rounds []hltvTradeRoundTrace, model hltvTradeModel) map[uint64]int {
	qualifying := make(map[uint64]int)
	for i := range rounds {
		round := &rounds[i]
		traded := modelTradedDeaths(round, model)
		for player := range round.participants {
			if round.enemyKills[player] > 0 || round.assists[player]&classicAssist != 0 ||
				round.finalAlive[player] || traded[player] {
				qualifying[player]++
			}
		}
	}
	return qualifying
}

func modelTradedDeaths(round *hltvTradeRoundTrace, model hltvTradeModel) map[uint64]bool {
	traded := make(map[uint64]bool)
	for revengeIndex, revenge := range round.kills {
		if revenge.killer == 0 || revenge.killerTeam == revenge.victimTeam ||
			(!model.postRound && revenge.postRound) {
			continue
		}
		var selected uint64
		found := false
		for _, death := range round.kills[:revengeIndex] {
			if death.killer != revenge.victim || death.victimTeam != revenge.killerTeam ||
				(!model.postRound && death.postRound) || !model.insideWindow(death, revenge, round.tickTime) {
				continue
			}
			switch model.selection {
			case hltvTradeEarliest:
				selected = death.victim
				found = true
			case hltvTradeNearest:
				selected = death.victim
				found = true
			case hltvTradeMultiple:
				traded[death.victim] = true
			}
			if model.selection == hltvTradeEarliest {
				break
			}
		}
		if found {
			traded[selected] = true
		}
	}
	return traded
}

func (model hltvTradeModel) insideWindow(death, revenge hltvKillTrace, tickTime time.Duration) bool {
	switch model.boundary {
	case hltvTradeExactTime:
		return revenge.at-death.at <= tradeWindow
	case hltvTradeTimeResolution:
		return revenge.at-death.at <= tradeWindow+tickTime
	case hltvTradeNormalizedTicks:
		return insideTradeWindow(death.tick, revenge.tick, tickTime, tradeWindow)
	default:
		return false
	}
}
