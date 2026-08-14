package analysis

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
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

// canonicalDirectTradeModel is the direct model production implements:
// earliest eligible death, normalized five-second ticks, post-round events
// included.
func canonicalDirectTradeModel() hltvTradeModel {
	return hltvTradeModel{selection: hltvTradeEarliest, boundary: hltvTradeNormalizedTicks, postRound: true}
}

// hltvChainBridge names which intermediate kill may re-anchor a trade chain
// between a candidate death and its terminal revenge kill. Every chain model
// keeps production's other semantics: earliest eligible death, the normalized
// five-second tick boundary, and post-round inclusion.
type hltvChainBridge uint8

const (
	hltvChainNone hltvChainBridge = iota
	hltvChainKiller
	hltvChainRevengerAny
	hltvChainRevengerAssister
	hltvChainKillerOrRevengerAny
	hltvChainKillerOrRevengerAssister
)

func (bridge hltvChainBridge) String() string {
	names := []string{
		"production-control",
		"killer-chain",
		"revenger-any-chain",
		"revenger-assister-chain",
		"combined-killer-revenger-any",
		"combined-killer-revenger-assister",
	}
	return names[bridge]
}

func allHLTVChainBridges() []hltvChainBridge {
	return []hltvChainBridge{
		hltvChainNone,
		hltvChainKiller,
		hltvChainRevengerAny,
		hltvChainRevengerAssister,
		hltvChainKillerOrRevengerAny,
		hltvChainKillerOrRevengerAssister,
	}
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

// hltvTradeCase pins the two remaining classic-KAST aggregate differences and
// the 358-tick direct gap that production already scores correctly, which
// every candidate model must keep excluded.
type hltvTradeCase struct {
	label     string
	fixtureID string
	mapID     int
	steamID   uint64
	round     int
}

var hltvTradeCases = []hltvTradeCase{
	{label: "kairo-R9", fixtureID: "match-129241", mapID: 234944, steamID: 76561199048086137, round: 9},
	{label: "magixx-R22", fixtureID: "match-2396559", mapID: 234956, steamID: 76561199063238565, round: 22},
	{label: "mirage-R4-control", fixtureID: "match-129241", mapID: 234947, steamID: 76561198127259887, round: 4},
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

	names := make(map[hltvFixtureMapKey]map[uint64]string)
	wantKAST := make(map[hltvFixtureMapKey]map[uint64]int)
	for _, fixture := range fixtures {
		for _, expectedMap := range fixture.oracle.Maps {
			key := hltvFixtureMapKey{FixtureID: fixture.oracle.FixtureID, MapID: expectedMap.MapID}
			names[key] = make(map[uint64]string, len(expectedMap.Players))
			wantKAST[key] = make(map[uint64]int, len(expectedMap.Players))
			for _, player := range expectedMap.Players {
				names[key][player.SteamID] = player.HLTVName
				wantKAST[key][player.SteamID] = kastRounds(player.KASTPercent, expectedMap.Rounds)
			}
		}
	}

	canonical := canonicalDirectTradeModel()
	production := make(map[hltvFixtureMapKey][]map[uint64]bool)
	productionCounts := make(map[hltvFixtureMapKey]map[uint64]int)
	for _, fixture := range fixtures {
		for _, expectedMap := range fixture.oracle.Maps {
			key := hltvFixtureMapKey{FixtureID: fixture.oracle.FixtureID, MapID: expectedMap.MapID}
			byRound := evaluatedKASTByRound(fixture.maps[expectedMap.MapID],
				func(round *hltvTradeRoundTrace) map[uint64]bool { return modelTradedDeaths(round, canonical) })
			production[key] = byRound
			productionCounts[key] = kastVectorCounts(byRound)
		}
	}

	logHLTVTradeCaseRounds(t, fixtures, names)
	assertProductionControlInvariant(t, fixtures, canonical)

	for _, evaluation := range allEvaluatedTradeModels() {
		originalMatches, originalRows := 0, 0
		extraMatches, extraRows := 0, 0
		var mismatches, changedPlayerRounds, aggregateShifts, negativeControls, caseLines []string
		for i, fixture := range fixtures {
			for _, expectedMap := range fixture.oracle.Maps {
				key := hltvFixtureMapKey{FixtureID: fixture.oracle.FixtureID, MapID: expectedMap.MapID}
				traces := fixture.maps[expectedMap.MapID]
				byRound := evaluatedKASTByRound(traces, evaluation.traded)
				counts := kastVectorCounts(byRound)
				for _, expectedPlayer := range expectedMap.Players {
					got := counts[expectedPlayer.SteamID]
					want := wantKAST[key][expectedPlayer.SteamID]
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
					prodVector := playerKASTVector(production[key], expectedPlayer.SteamID)
					modelVector := playerKASTVector(byRound, expectedPlayer.SteamID)
					if !slices.Equal(prodVector, modelVector) {
						prodCount := productionCounts[key][expectedPlayer.SteamID]
						shift := classifyAggregateShift(true, prodCount, got, want)
						aggregateShifts = append(aggregateShifts, fmt.Sprintf("%s/%d/%s=%s(production=%d,model=%d,hltv=%d)",
							fixture.oracle.FixtureID, expectedMap.MapID, expectedPlayer.HLTVName, shift, prodCount, got, want))
						if prodCount == want && shift == hltvShiftFarther {
							negativeControls = append(negativeControls, fmt.Sprintf("%s/%d/%s",
								fixture.oracle.FixtureID, expectedMap.MapID, expectedPlayer.HLTVName))
						}
					}
				}
				for roundIndex := range byRound {
					for _, id := range slices.Sorted(maps.Keys(byRound[roundIndex])) {
						prodValue := production[key][roundIndex][id]
						modelValue := byRound[roundIndex][id]
						if prodValue != modelValue {
							changedPlayerRounds = append(changedPlayerRounds, fmt.Sprintf("%s/%d/%s/round=%d:%t->%t",
								fixture.oracle.FixtureID, expectedMap.MapID, hltvPlayerLabel(names[key], id), roundIndex+1, prodValue, modelValue))
						}
					}
				}
				for _, tradeCase := range hltvTradeCases {
					if tradeCase.fixtureID != key.FixtureID || tradeCase.mapID != key.MapID ||
						tradeCase.round < 1 || tradeCase.round > len(traces) {
						continue
					}
					tradedSet := evaluation.traded(&traces[tradeCase.round-1])
					caseLines = append(caseLines, fmt.Sprintf("case=%s traded=%t kast=%t",
						tradeCase.label, tradedSet[tradeCase.steamID], byRound[tradeCase.round-1][tradeCase.steamID]))
				}
			}
		}
		sort.Strings(mismatches)
		t.Logf("model=%s original=%d/%d additional=%d/%d combined=%d/%d mismatches=%v",
			evaluation.name, originalMatches, originalRows, extraMatches, extraRows,
			originalMatches+extraMatches, originalRows+extraRows, mismatches)
		t.Logf("model=%s changed_player_rounds=%d %v", evaluation.name, len(changedPlayerRounds), changedPlayerRounds)
		t.Logf("model=%s aggregate_shifts=%v independent_negative_controls=%v",
			evaluation.name, aggregateShifts, negativeControls)
		for _, line := range caseLines {
			t.Logf("model=%s %s", evaluation.name, line)
		}
	}

	t.Logf("positive controls: none are independently observable; HLTV publishes only map-level KAST aggregates, and the only two production/HLTV KAST differences are the kairo-R9 and magixx-R22 target rows themselves")
	reportChainStructure(t, fixtures, names)
}

type hltvEvaluatedTradeModel struct {
	name   string
	traded func(*hltvTradeRoundTrace) map[uint64]bool
}

// allEvaluatedTradeModels lists the original 18 direct combinations followed
// by the six focused chain models. The chain dimension is deliberately not
// multiplied across the unrelated attribution and boundary dimensions.
func allEvaluatedTradeModels() []hltvEvaluatedTradeModel {
	var evaluations []hltvEvaluatedTradeModel
	for _, model := range allHLTVTradeModels() {
		evaluations = append(evaluations, hltvEvaluatedTradeModel{
			name:   model.String(),
			traded: func(round *hltvTradeRoundTrace) map[uint64]bool { return modelTradedDeaths(round, model) },
		})
	}
	for _, bridge := range allHLTVChainBridges() {
		evaluations = append(evaluations, hltvEvaluatedTradeModel{
			name:   bridge.String(),
			traded: func(round *hltvTradeRoundTrace) map[uint64]bool { return chainTradedDeaths(round, bridge) },
		})
	}
	return evaluations
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
			assister:   trackerID(event.Assister),
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

// evaluatedKASTByRound derives the per-round classic-KAST booleans of every
// round participant under one traded-death model.
func evaluatedKASTByRound(rounds []hltvTradeRoundTrace, traded func(*hltvTradeRoundTrace) map[uint64]bool) []map[uint64]bool {
	byRound := make([]map[uint64]bool, len(rounds))
	for i := range rounds {
		round := &rounds[i]
		tradedSet := traded(round)
		kast := make(map[uint64]bool, len(round.participants))
		for player := range round.participants {
			kast[player] = round.enemyKills[player] > 0 || round.assists[player]&classicAssist != 0 ||
				round.finalAlive[player] || tradedSet[player]
		}
		byRound[i] = kast
	}
	return byRound
}

func kastVectorCounts(byRound []map[uint64]bool) map[uint64]int {
	counts := make(map[uint64]int)
	for _, kast := range byRound {
		for player, qualified := range kast {
			if qualified {
				counts[player]++
			}
		}
	}
	return counts
}

func playerKASTVector(byRound []map[uint64]bool, player uint64) []bool {
	vector := make([]bool, len(byRound))
	for i, kast := range byRound {
		vector[i] = kast[player]
	}
	return vector
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

// chainBridgePermits reports whether an intermediate kill may re-anchor the
// chain from death towards revenge under the bridge rule.
func chainBridgePermits(bridge hltvChainBridge, death, revenge, event hltvKillTrace) bool {
	switch bridge {
	case hltvChainKiller:
		return chainKillerActivity(death, event)
	case hltvChainRevengerAny:
		return chainRevengerActivity(revenge, event)
	case hltvChainRevengerAssister:
		return chainRevengerKilledAssister(death, revenge, event)
	case hltvChainKillerOrRevengerAny:
		return chainKillerActivity(death, event) || chainRevengerActivity(revenge, event)
	case hltvChainKillerOrRevengerAssister:
		return chainKillerActivity(death, event) || chainRevengerKilledAssister(death, revenge, event)
	default:
		return false
	}
}

// chainKillerActivity is a further enemy kill by the original killer.
func chainKillerActivity(death, event hltvKillTrace) bool {
	return event.killer != 0 && event.killer == death.killer && event.killerTeam != event.victimTeam
}

// chainRevengerActivity is any enemy kill by the eventual revenger.
func chainRevengerActivity(revenge, event hltvKillTrace) bool {
	return event.killer != 0 && event.killer == revenge.killer && event.killerTeam != event.victimTeam
}

// chainRevengerKilledAssister is the eventual revenger landing an enemy kill
// on the nonzero assister of the original death. A teamkill never bridges,
// even on the matching SteamID.
func chainRevengerKilledAssister(death, revenge, event hltvKillTrace) bool {
	return death.assister != 0 && event.killer == revenge.killer &&
		event.victim == death.assister && event.killerTeam != event.victimTeam
}

// chainEligible walks the events between a candidate death and the terminal
// revenge kill in order. A permitted bridge re-anchors the five-second window
// only while the chain is still active; once more than five seconds passed
// since the current anchor, no later event revives the candidate. Unrelated
// events never re-anchor. The terminal revenge must land inside five seconds
// of the final anchor, always through the normalized insideTradeWindow rule.
func chainEligible(round *hltvTradeRoundTrace, deathIndex, revengeIndex int, bridge hltvChainBridge) bool {
	death := round.kills[deathIndex]
	revenge := round.kills[revengeIndex]
	anchor := death
	for _, event := range round.kills[deathIndex+1 : revengeIndex] {
		if !chainBridgePermits(bridge, death, revenge, event) {
			continue
		}
		if !insideTradeWindow(anchor.tick, event.tick, round.tickTime, tradeWindow) {
			return false
		}
		anchor = event
	}
	return insideTradeWindow(anchor.tick, revenge.tick, round.tickTime, tradeWindow)
}

// chainTradedDeaths applies one chain model to a recorded round. Candidate
// deaths for an enemy terminal revenge kill R are earlier deaths D with
// D.killer == R.victim and D.victimTeam == R.killerTeam. Each terminal
// revenge credits exactly one death: the earliest chain-eligible death by
// event order.
func chainTradedDeaths(round *hltvTradeRoundTrace, bridge hltvChainBridge) map[uint64]bool {
	traded := make(map[uint64]bool)
	for revengeIndex, revenge := range round.kills {
		if revenge.killer == 0 || revenge.killerTeam == revenge.victimTeam {
			continue
		}
		for deathIndex, death := range round.kills[:revengeIndex] {
			if death.killer != revenge.victim || death.victimTeam != revenge.killerTeam {
				continue
			}
			if chainEligible(round, deathIndex, revengeIndex, bridge) {
				traded[death.victim] = true
				break
			}
		}
	}
	return traded
}

type hltvAggregateShift string

const (
	hltvShiftUnchanged     hltvAggregateShift = "unchanged"
	hltvShiftCloser        hltvAggregateShift = "closer"
	hltvShiftFarther       hltvAggregateShift = "farther"
	hltvShiftIndeterminate hltvAggregateShift = "aggregate-indeterminate"
)

// classifyAggregateShift compares one player-map against production and HLTV.
// Counts alone cannot prove per-round agreement, so an unchanged verdict
// requires the exact per-round vector to match production.
func classifyAggregateShift(vectorChanged bool, productionCount, modelCount, hltvCount int) hltvAggregateShift {
	if !vectorChanged {
		return hltvShiftUnchanged
	}
	productionDistance := intAbs(productionCount - hltvCount)
	modelDistance := intAbs(modelCount - hltvCount)
	switch {
	case modelDistance < productionDistance:
		return hltvShiftCloser
	case modelDistance > productionDistance:
		return hltvShiftFarther
	default:
		return hltvShiftIndeterminate
	}
}

func intAbs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func hltvPlayerLabel(names map[uint64]string, id uint64) string {
	if id == 0 {
		return "World"
	}
	if name, ok := names[id]; ok {
		return name
	}
	return strconv.FormatUint(id, 10)
}

// logHLTVTradeCaseRounds prints the full kill sequence of the three pinned
// rounds once so every model verdict can be checked against the raw events.
func logHLTVTradeCaseRounds(t *testing.T, fixtures []hltvModelFixture, names map[hltvFixtureMapKey]map[uint64]string) {
	t.Helper()
	for _, tradeCase := range hltvTradeCases {
		round := findHLTVTradeCaseRound(fixtures, tradeCase)
		if round == nil {
			t.Errorf("case %s references missing fixture/map/round %s/%d/%d",
				tradeCase.label, tradeCase.fixtureID, tradeCase.mapID, tradeCase.round)
			continue
		}
		key := hltvFixtureMapKey{FixtureID: tradeCase.fixtureID, MapID: tradeCase.mapID}
		kills := make([]string, len(round.kills))
		for i, kill := range round.kills {
			label := fmt.Sprintf("%s->%s@%d", hltvPlayerLabel(names[key], kill.killer),
				hltvPlayerLabel(names[key], kill.victim), kill.tick)
			if kill.assister != 0 {
				label += fmt.Sprintf("(+%s)", hltvPlayerLabel(names[key], kill.assister))
			}
			kills[i] = label
		}
		t.Logf("case=%s round=%d kills=%s", tradeCase.label, tradeCase.round, strings.Join(kills, ","))
	}
}

func findHLTVTradeCaseRound(fixtures []hltvModelFixture, tradeCase hltvTradeCase) *hltvTradeRoundTrace {
	for _, fixture := range fixtures {
		if fixture.oracle.FixtureID != tradeCase.fixtureID {
			continue
		}
		traces := fixture.maps[tradeCase.mapID]
		if tradeCase.round >= 1 && tradeCase.round <= len(traces) {
			return &traces[tradeCase.round-1]
		}
	}
	return nil
}

// assertProductionControlInvariant fails when the production-control chain
// model diverges from the canonical direct model on any player-round KAST
// boolean or on any scored round's traded-death set. Aggregate equality
// would be insufficient: opposite round changes cancel. The traded-set check
// matters separately because another K/A/S reason could conceal a different
// trade attribution behind an identical KAST boolean.
func assertProductionControlInvariant(t *testing.T, fixtures []hltvModelFixture, canonical hltvTradeModel) {
	t.Helper()
	mismatched := 0
	for _, fixture := range fixtures {
		for _, expectedMap := range fixture.oracle.Maps {
			traces := fixture.maps[expectedMap.MapID]
			direct := evaluatedKASTByRound(traces, func(round *hltvTradeRoundTrace) map[uint64]bool {
				return modelTradedDeaths(round, canonical)
			})
			control := evaluatedKASTByRound(traces, func(round *hltvTradeRoundTrace) map[uint64]bool {
				return chainTradedDeaths(round, hltvChainNone)
			})
			for roundIndex := range traces {
				directTraded := modelTradedDeaths(&traces[roundIndex], canonical)
				controlTraded := chainTradedDeaths(&traces[roundIndex], hltvChainNone)
				if !maps.Equal(directTraded, controlTraded) {
					mismatched++
					t.Errorf("production-control traded set diverges from %s on %s/%d round %d: direct=%v control=%v",
						canonical, fixture.oracle.FixtureID, expectedMap.MapID, roundIndex+1,
						slices.Sorted(maps.Keys(directTraded)), slices.Sorted(maps.Keys(controlTraded)))
				}
				for _, id := range slices.Sorted(maps.Keys(direct[roundIndex])) {
					if direct[roundIndex][id] != control[roundIndex][id] {
						mismatched++
						t.Errorf("production-control diverges from %s on %s/%d round %d player %d: direct=%t control=%t",
							canonical, fixture.oracle.FixtureID, expectedMap.MapID, roundIndex+1, id,
							direct[roundIndex][id], control[roundIndex][id])
					}
				}
			}
		}
	}
	if mismatched == 0 {
		t.Logf("invariant: production-control matches %s on every player-round KAST boolean and every scored round's traded-death set across all eight maps", canonical)
	}
}

// reportChainStructure enumerates the deterministic structural material the
// chain models could act on. These are structural counts, not independent
// controls. The counting unit for the three bridge categories is one
// (scored round, death index D, bridge index B, revenge index R) kill tuple
// with D.killer == R.victim, D.victimTeam == R.killerTeam and D < B < R in
// event order. A chain-only row is one (bridge model, D, R) pair whose direct
// gap is outside the normalized five-second window while every permitted
// chain link stays inside it. Output follows fixture, oracle map, round and
// kill order, so it is deterministic.
func reportChainStructure(t *testing.T, fixtures []hltvModelFixture, names map[hltvFixtureMapKey]map[uint64]string) {
	t.Helper()
	t.Logf("structure counting unit: bridge triples are (scored round, D, B, R) kill tuples per category; chain-only rows are (bridge model, D, R) pairs with the direct gap outside the normalized window and every permitted link inside it")
	for _, fixture := range fixtures {
		for _, expectedMap := range fixture.oracle.Maps {
			key := hltvFixtureMapKey{FixtureID: fixture.oracle.FixtureID, MapID: expectedMap.MapID}
			traces := fixture.maps[expectedMap.MapID]
			killerTriples, revengerAnyTriples, revengerAssisterTriples := 0, 0, 0
			var chainOnly []string
			for roundIndex := range traces {
				round := &traces[roundIndex]
				for revengeIndex, revenge := range round.kills {
					if revenge.killer == 0 || revenge.killerTeam == revenge.victimTeam {
						continue
					}
					for deathIndex, death := range round.kills[:revengeIndex] {
						if death.killer != revenge.victim || death.victimTeam != revenge.killerTeam {
							continue
						}
						for _, event := range round.kills[deathIndex+1 : revengeIndex] {
							if chainKillerActivity(death, event) {
								killerTriples++
							}
							if chainRevengerActivity(revenge, event) {
								revengerAnyTriples++
							}
							if chainRevengerKilledAssister(death, revenge, event) {
								revengerAssisterTriples++
							}
						}
						if insideTradeWindow(death.tick, revenge.tick, round.tickTime, tradeWindow) {
							continue
						}
						for _, bridge := range allHLTVChainBridges()[1:] {
							if chainEligible(round, deathIndex, revengeIndex, bridge) {
								chainOnly = append(chainOnly, fmt.Sprintf(
									"round=%d bridge=%s victim=%s death_tick=%d revenge_tick=%d direct_ticks=%d revenger=%s",
									roundIndex+1, bridge, hltvPlayerLabel(names[key], death.victim), death.tick,
									revenge.tick, revenge.tick-death.tick, hltvPlayerLabel(names[key], revenge.killer)))
							}
						}
					}
				}
			}
			t.Logf("structure %s/%d/%s killer_bridge_triples=%d revenger_any_triples=%d revenger_assister_triples=%d chain_only_rows=%d",
				fixture.oracle.FixtureID, expectedMap.MapID, expectedMap.Name,
				killerTriples, revengerAnyTriples, revengerAssisterTriples, len(chainOnly))
			for _, row := range chainOnly {
				t.Logf("chain_only %s/%d/%s %s", fixture.oracle.FixtureID, expectedMap.MapID, expectedMap.Name, row)
			}
		}
	}
}

const (
	chainOriginalKiller   uint64 = 1 // dies to the terminal revenge kill
	chainRevenger         uint64 = 2
	chainFirstVictim      uint64 = 3
	chainSecondVictim     uint64 = 4
	chainOriginalAssister uint64 = 5 // enemy assister on the first death
	chainThirdVictim      uint64 = 6
	chainOtherEnemy       uint64 = 7 // enemy unrelated to the first death
	chainBystander        uint64 = 8 // revenger-side teammate; bridges nothing
)

func chainTestKill(tick int, killer, victim uint64, killerTeam, victimTeam common.Team, assister uint64) hltvKillTrace {
	return hltvKillTrace{
		tick:       tick,
		killer:     killer,
		victim:     victim,
		killerTeam: killerTeam,
		victimTeam: victimTeam,
		assister:   assister,
	}
}

// TestChainTradeSemantics drives the chain models with synthetic rounds at 64
// ticks per second, where the normalized five-second window admits direct
// gaps up to 321 ticks. Every scenario asserts all six models, and the
// production-control result must equal the canonical direct model.
func TestChainTradeSemantics(t *testing.T) {
	attacker := common.TeamTerrorists
	defender := common.TeamCounterTerrorists
	tests := []struct {
		name   string
		kills  []hltvKillTrace
		expect map[hltvChainBridge][]uint64
	}{
		{
			name: "killer activity bridges the chain and moves credit to the earliest death",
			kills: []hltvKillTrace{
				chainTestKill(1000, chainOriginalKiller, chainFirstVictim, attacker, defender, 0),
				chainTestKill(1300, chainOriginalKiller, chainSecondVictim, attacker, defender, 0),
				chainTestKill(1600, chainRevenger, chainOriginalKiller, defender, attacker, 0),
			},
			expect: map[hltvChainBridge][]uint64{
				hltvChainNone:                     {chainSecondVictim},
				hltvChainKiller:                   {chainFirstVictim},
				hltvChainRevengerAny:              {chainSecondVictim},
				hltvChainRevengerAssister:         {chainSecondVictim},
				hltvChainKillerOrRevengerAny:      {chainFirstVictim},
				hltvChainKillerOrRevengerAssister: {chainFirstVictim},
			},
		},
		{
			name: "revenger activity bridges the chain",
			kills: []hltvKillTrace{
				chainTestKill(1000, chainOriginalKiller, chainFirstVictim, attacker, defender, 0),
				chainTestKill(1200, chainRevenger, chainOtherEnemy, defender, attacker, 0),
				chainTestKill(1500, chainRevenger, chainOriginalKiller, defender, attacker, 0),
			},
			expect: map[hltvChainBridge][]uint64{
				hltvChainNone:                     {},
				hltvChainKiller:                   {},
				hltvChainRevengerAny:              {chainFirstVictim},
				hltvChainRevengerAssister:         {},
				hltvChainKillerOrRevengerAny:      {chainFirstVictim},
				hltvChainKillerOrRevengerAssister: {},
			},
		},
		{
			name: "assister bridge accepts the original nonzero assister",
			kills: []hltvKillTrace{
				chainTestKill(1000, chainOriginalKiller, chainFirstVictim, attacker, defender, chainOriginalAssister),
				chainTestKill(1200, chainRevenger, chainOriginalAssister, defender, attacker, 0),
				chainTestKill(1500, chainRevenger, chainOriginalKiller, defender, attacker, 0),
			},
			expect: map[hltvChainBridge][]uint64{
				hltvChainNone:                     {},
				hltvChainKiller:                   {},
				hltvChainRevengerAny:              {chainFirstVictim},
				hltvChainRevengerAssister:         {chainFirstVictim},
				hltvChainKillerOrRevengerAny:      {chainFirstVictim},
				hltvChainKillerOrRevengerAssister: {chainFirstVictim},
			},
		},
		{
			name: "assister bridge rejects a kill on another victim",
			kills: []hltvKillTrace{
				chainTestKill(1000, chainOriginalKiller, chainFirstVictim, attacker, defender, chainOriginalAssister),
				chainTestKill(1200, chainRevenger, chainOtherEnemy, defender, attacker, 0),
				chainTestKill(1500, chainRevenger, chainOriginalKiller, defender, attacker, 0),
			},
			expect: map[hltvChainBridge][]uint64{
				hltvChainNone:                     {},
				hltvChainKiller:                   {},
				hltvChainRevengerAny:              {chainFirstVictim},
				hltvChainRevengerAssister:         {},
				hltvChainKillerOrRevengerAny:      {chainFirstVictim},
				hltvChainKillerOrRevengerAssister: {},
			},
		},
		{
			name: "assister bridge rejects a death without an assister",
			kills: []hltvKillTrace{
				chainTestKill(1000, chainOriginalKiller, chainFirstVictim, attacker, defender, 0),
				chainTestKill(1200, chainRevenger, chainOriginalAssister, defender, attacker, 0),
				chainTestKill(1500, chainRevenger, chainOriginalKiller, defender, attacker, 0),
			},
			expect: map[hltvChainBridge][]uint64{
				hltvChainNone:                     {},
				hltvChainKiller:                   {},
				hltvChainRevengerAny:              {chainFirstVictim},
				hltvChainRevengerAssister:         {},
				hltvChainKillerOrRevengerAny:      {chainFirstVictim},
				hltvChainKillerOrRevengerAssister: {},
			},
		},
		{
			name: "a teamkill on the assister's SteamID does not bridge",
			kills: []hltvKillTrace{
				chainTestKill(1000, chainOriginalKiller, chainFirstVictim, attacker, defender, chainOriginalAssister),
				chainTestKill(1200, chainRevenger, chainOriginalAssister, defender, defender, 0),
				chainTestKill(1500, chainRevenger, chainOriginalKiller, defender, attacker, 0),
			},
			expect: map[hltvChainBridge][]uint64{
				hltvChainNone:                     {},
				hltvChainKiller:                   {},
				hltvChainRevengerAny:              {},
				hltvChainRevengerAssister:         {},
				hltvChainKillerOrRevengerAny:      {},
				hltvChainKillerOrRevengerAssister: {},
			},
		},
		{
			name: "an unrelated kill does not bridge",
			kills: []hltvKillTrace{
				chainTestKill(1000, chainOriginalKiller, chainFirstVictim, attacker, defender, 0),
				chainTestKill(1200, chainBystander, chainOtherEnemy, defender, attacker, 0),
				chainTestKill(1500, chainRevenger, chainOriginalKiller, defender, attacker, 0),
			},
			expect: map[hltvChainBridge][]uint64{
				hltvChainNone:                     {},
				hltvChainKiller:                   {},
				hltvChainRevengerAny:              {},
				hltvChainRevengerAssister:         {},
				hltvChainKillerOrRevengerAny:      {},
				hltvChainKillerOrRevengerAssister: {},
			},
		},
		{
			name: "a link outside five seconds expires the chain",
			kills: []hltvKillTrace{
				chainTestKill(1000, chainOriginalKiller, chainFirstVictim, attacker, defender, 0),
				chainTestKill(1400, chainOriginalKiller, chainSecondVictim, attacker, defender, 0),
				chainTestKill(1500, chainRevenger, chainOriginalKiller, defender, attacker, 0),
			},
			expect: map[hltvChainBridge][]uint64{
				hltvChainNone:                     {chainSecondVictim},
				hltvChainKiller:                   {chainSecondVictim},
				hltvChainRevengerAny:              {chainSecondVictim},
				hltvChainRevengerAssister:         {chainSecondVictim},
				hltvChainKillerOrRevengerAny:      {chainSecondVictim},
				hltvChainKillerOrRevengerAssister: {chainSecondVictim},
			},
		},
		{
			name: "the 358-tick no-bridge control stays excluded",
			kills: []hltvKillTrace{
				chainTestKill(37913, chainOriginalKiller, chainFirstVictim, attacker, defender, 0),
				chainTestKill(38271, chainRevenger, chainOriginalKiller, defender, attacker, 0),
			},
			expect: map[hltvChainBridge][]uint64{
				hltvChainNone:                     {},
				hltvChainKiller:                   {},
				hltvChainRevengerAny:              {},
				hltvChainRevengerAssister:         {},
				hltvChainKillerOrRevengerAny:      {},
				hltvChainKillerOrRevengerAssister: {},
			},
		},
		{
			name: "one terminal revenge credits only the earliest eligible death",
			kills: []hltvKillTrace{
				chainTestKill(1000, chainOriginalKiller, chainFirstVictim, attacker, defender, 0),
				chainTestKill(1100, chainOriginalKiller, chainSecondVictim, attacker, defender, 0),
				chainTestKill(1300, chainRevenger, chainOriginalKiller, defender, attacker, 0),
			},
			expect: map[hltvChainBridge][]uint64{
				hltvChainNone:                     {chainFirstVictim},
				hltvChainKiller:                   {chainFirstVictim},
				hltvChainRevengerAny:              {chainFirstVictim},
				hltvChainRevengerAssister:         {chainFirstVictim},
				hltvChainKillerOrRevengerAny:      {chainFirstVictim},
				hltvChainKillerOrRevengerAssister: {chainFirstVictim},
			},
		},
		{
			name: "multiple permitted bridges extend one chain",
			kills: []hltvKillTrace{
				chainTestKill(1000, chainOriginalKiller, chainFirstVictim, attacker, defender, 0),
				chainTestKill(1300, chainOriginalKiller, chainSecondVictim, attacker, defender, 0),
				chainTestKill(1600, chainOriginalKiller, chainThirdVictim, attacker, defender, 0),
				chainTestKill(1900, chainRevenger, chainOriginalKiller, defender, attacker, 0),
			},
			expect: map[hltvChainBridge][]uint64{
				hltvChainNone:                     {chainThirdVictim},
				hltvChainKiller:                   {chainFirstVictim},
				hltvChainRevengerAny:              {chainThirdVictim},
				hltvChainRevengerAssister:         {chainThirdVictim},
				hltvChainKillerOrRevengerAny:      {chainFirstVictim},
				hltvChainKillerOrRevengerAssister: {chainFirstVictim},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			round := &hltvTradeRoundTrace{tickTime: time.Second / 64, kills: test.kills}
			for _, bridge := range allHLTVChainBridges() {
				want, listed := test.expect[bridge]
				if !listed {
					t.Fatalf("scenario lists no expectation for %s", bridge)
				}
				got := slices.Sorted(maps.Keys(chainTradedDeaths(round, bridge)))
				wantSorted := slices.Sorted(slices.Values(want))
				if !slices.Equal(got, wantSorted) {
					t.Errorf("%s traded %v, want %v", bridge, got, wantSorted)
				}
			}
			direct := modelTradedDeaths(round, canonicalDirectTradeModel())
			control := chainTradedDeaths(round, hltvChainNone)
			if !maps.Equal(direct, control) {
				t.Errorf("production-control traded %v, canonical direct model traded %v", control, direct)
			}
		})
	}
}

func TestClassifyAggregateShift(t *testing.T) {
	tests := []struct {
		name          string
		vectorChanged bool
		production    int
		model         int
		hltv          int
		want          hltvAggregateShift
	}{
		{name: "identical vector is unchanged", vectorChanged: false, production: 17, model: 17, hltv: 18, want: hltvShiftUnchanged},
		{name: "changed vector approaching HLTV is closer", vectorChanged: true, production: 17, model: 18, hltv: 18, want: hltvShiftCloser},
		{name: "changed vector leaving HLTV is farther", vectorChanged: true, production: 18, model: 19, hltv: 18, want: hltvShiftFarther},
		{name: "changed vector with equal counts is indeterminate", vectorChanged: true, production: 17, model: 17, hltv: 18, want: hltvShiftIndeterminate},
		{name: "changed vector equidistant from HLTV is indeterminate", vectorChanged: true, production: 17, model: 19, hltv: 18, want: hltvShiftIndeterminate},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyAggregateShift(test.vectorChanged, test.production, test.model, test.hltv)
			if got != test.want {
				t.Errorf("classifyAggregateShift(%t, %d, %d, %d) = %s, want %s",
					test.vectorChanged, test.production, test.model, test.hltv, got, test.want)
			}
		})
	}
}
