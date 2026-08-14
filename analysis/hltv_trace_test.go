package analysis

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	events "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

const (
	hltvTraceDemoEnv    = "HLTV_TRACE_DEMO"
	hltvTraceSteamIDEnv = "HLTV_TRACE_STEAM_ID"
)

type hltvRoundTrace struct {
	startedAtRound   int
	tickTime         time.Duration
	participants     map[uint64]common.Team
	kills            []hltvKillTrace
	disconnects      []hltvDisconnectTrace
	enemyKills       map[uint64]int
	assists          map[uint64]assistFacts
	traded           map[uint64]bool
	damageAssists    map[uint64]bool
	survivedRoundEnd map[uint64]bool
	survivedOfficial map[uint64]bool
	finalAlive       map[uint64]bool
	winner           common.Team
}

type hltvKillTrace struct {
	frame         int
	tick          int
	at            time.Duration
	killer        uint64
	killerName    string
	killerTeam    common.Team
	victim        uint64
	victimName    string
	victimTeam    common.Team
	assister      uint64
	assisterName  string
	assistedFlash bool
	cause         string
	postRound     bool
}

type hltvDisconnectTrace struct {
	frame  int
	tick   int
	player uint64
	name   string
}

// TestTraceHLTVRoundEvidence is an opt-in, read-only diagnostic. It records
// the event-level facts used by classic KAST without adding trace-only state
// to production. Set HLTV_TRACE_STEAM_ID to focus the output on one player;
// omit it to print every participant's final round reasons.
func TestTraceHLTVRoundEvidence(t *testing.T) {
	demoPath := os.Getenv(hltvTraceDemoEnv)
	if demoPath == "" {
		t.Skipf("set %s to trace one external demo", hltvTraceDemoEnv)
	}
	target := parseTraceSteamID(t, os.Getenv(hltvTraceSteamIDEnv))

	file, err := os.Open(demoPath)
	if err != nil {
		t.Fatalf("opening trace demo: %v", err)
	}
	defer file.Close()

	parser := demoinfocs.NewParser(file)
	defer parser.Close()
	analysis := newAnalyser(parser)
	analysis.registerHandlers()

	var current *hltvRoundTrace
	var names = make(map[uint64]string)
	finishCurrent := func(totalRounds int) {
		if current == nil {
			return
		}
		scored := totalRounds > current.startedAtRound
		logHLTVRoundTrace(t, current, scored, target, names)
		current = nil
	}
	snapshot := func() {
		if current == nil {
			return
		}
		current.participants = maps.Clone(analysis.tracker.startAlive)
		current.enemyKills = maps.Clone(analysis.tracker.enemyKills)
		current.assists = maps.Clone(analysis.tracker.assists)
		current.traded = maps.Clone(analysis.tracker.traded)
		current.damageAssists = maps.Clone(analysis.tracker.damageAssists)
		current.finalAlive = aliveSet(analysis.tracker.alive)
	}
	parser.RegisterEventHandler(func(events.RoundStart) {
		state := parser.GameState()
		finishCurrent(state.TotalRoundsPlayed())
		current = &hltvRoundTrace{
			startedAtRound: state.TotalRoundsPlayed(),
			tickTime:       parser.TickTime(),
		}
		rememberTraceRoster(names, state.Participants().Playing())
		snapshot()
		t.Logf("RoundStart frame=%d tick=%d rounds=%d phase=%s warmup=%t",
			parser.CurrentFrame(), state.IngameTick(), state.TotalRoundsPlayed(), state.GamePhase(), state.IsWarmupPeriod())
	})
	parser.RegisterEventHandler(func(events.RoundFreezetimeEnd) {
		rememberTraceRoster(names, parser.GameState().Participants().Playing())
		snapshot()
	})
	parser.RegisterEventHandler(func(e events.Kill) {
		if current == nil || e.Victim == nil {
			return
		}
		kill := hltvKillTrace{
			frame:         parser.CurrentFrame(),
			tick:          parser.GameState().IngameTick(),
			at:            parser.CurrentTime(),
			killer:        trackerID(e.Killer),
			killerName:    tracePlayerName(e.Killer),
			killerTeam:    tracePlayerTeam(e.Killer),
			victim:        trackerID(e.Victim),
			victimName:    tracePlayerName(e.Victim),
			victimTeam:    e.Victim.Team,
			assister:      trackerID(e.Assister),
			assisterName:  tracePlayerName(e.Assister),
			assistedFlash: e.AssistedFlash,
			cause:         traceKillCause(e),
			postRound:     analysis.tracker.isPostRound(),
		}
		current.kills = append(current.kills, kill)
		rememberTracePlayer(names, e.Killer)
		rememberTracePlayer(names, e.Victim)
		rememberTracePlayer(names, e.Assister)
		snapshot()
	})
	parser.RegisterEventHandler(func(e events.PlayerDisconnected) {
		if current == nil || e.Player == nil {
			return
		}
		current.disconnects = append(current.disconnects, hltvDisconnectTrace{
			frame: parser.CurrentFrame(), tick: parser.GameState().IngameTick(),
			player: trackerID(e.Player), name: tracePlayerName(e.Player),
		})
		snapshot()
	})
	parser.RegisterEventHandler(func(e events.RoundEnd) {
		if current == nil {
			return
		}
		current.winner = e.Winner
		snapshot()
		current.survivedRoundEnd = maps.Clone(current.finalAlive)
		state := parser.GameState()
		t.Logf("RoundEnd frame=%d tick=%d rounds=%d phase=%s winner=%v",
			parser.CurrentFrame(), state.IngameTick(), state.TotalRoundsPlayed(), state.GamePhase(), e.Winner)
	})
	parser.RegisterEventHandler(func(events.RoundEndOfficial) {
		if current == nil {
			return
		}
		snapshot()
		current.survivedOfficial = maps.Clone(current.finalAlive)
		state := parser.GameState()
		t.Logf("RoundEndOfficial frame=%d tick=%d rounds=%d phase=%s",
			parser.CurrentFrame(), state.IngameTick(), state.TotalRoundsPlayed(), state.GamePhase())
	})

	if err := parser.ParseToEnd(); err != nil {
		t.Fatalf("parsing trace demo: %v", err)
	}
	snapshot()
	state := parser.GameState()
	t.Logf("ParseEnd frame=%d tick=%d rounds=%d phase=%s",
		parser.CurrentFrame(), state.IngameTick(), state.TotalRoundsPlayed(), state.GamePhase())
	finishCurrent(state.TotalRoundsPlayed())
	analysis.finalise()

	ids := slices.Sorted(maps.Keys(analysis.players))
	for _, id := range ids {
		if target != 0 && id != target {
			continue
		}
		player := analysis.players[id]
		t.Logf("player=%s steam=%d participant_rounds=%d KAST=%.1f",
			player.Name, id, player.SideStats.Rounds.Total, player.PlayerMapStats.KAST)
	}
}

func parseTraceSteamID(t *testing.T, value string) uint64 {
	t.Helper()
	if value == "" {
		return 0
	}
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		t.Fatalf("%s must be a uint64: %v", hltvTraceSteamIDEnv, err)
	}
	return id
}

func logHLTVRoundTrace(t *testing.T, round *hltvRoundTrace, scored bool, target uint64, names map[uint64]string) {
	t.Helper()
	if !scored {
		t.Logf("round setup start_score=%d scored=false events=%d", round.startedAtRound, len(round.kills))
		return
	}
	roundNumber := round.startedAtRound + 1
	ids := slices.Sorted(maps.Keys(round.participants))
	for _, id := range ids {
		if target != 0 && target != id {
			continue
		}
		killed := round.enemyKills[id] > 0
		classicAssisted := round.assists[id]&classicAssist != 0
		flashAssisted := round.assists[id]&ratingAssist != 0 && !classicAssisted
		survivedEnd := round.survivedRoundEnd[id]
		survivedOfficial := round.survivedOfficial[id]
		survivedFinal := round.finalAlive[id]
		traded := round.traded[id]
		kast := killed || classicAssisted || survivedFinal || traded
		t.Logf("round=%d scored=true player=%s steam=%d side=%v K=%t A=%t flash_only=%t S_end=%t S_official=%t S_final=%t T=%t rating_damage_assist=%t KAST=%t winner=%v official=%t",
			roundNumber, names[id], id, round.participants[id], killed, classicAssisted, flashAssisted,
			survivedEnd, survivedOfficial, survivedFinal, traded, round.damageAssists[id], kast,
			round.winner, round.survivedOfficial != nil)
		logTraceDeaths(t, roundNumber, id, round, names)
	}
	for _, disconnect := range round.disconnects {
		if target == 0 || target == disconnect.player {
			t.Logf("round=%d disconnect player=%s steam=%d frame=%d tick=%d",
				roundNumber, disconnect.name, disconnect.player, disconnect.frame, disconnect.tick)
		}
	}
}

func logTraceDeaths(t *testing.T, roundNumber int, player uint64, round *hltvRoundTrace, names map[uint64]string) {
	t.Helper()
	for i, death := range round.kills {
		if death.victim != player {
			continue
		}
		t.Logf("round=%d death player=%s killer=%s cause=%s frame=%d tick=%d time=%s post_round=%t assister=%s assister_steam=%d flash=%t",
			roundNumber, names[player], death.killerName, death.cause, death.frame, death.tick,
			death.at, death.postRound, death.assisterName, death.assister, death.assistedFlash)
		for offset, revenge := range round.kills[i+1:] {
			if death.killer == 0 || revenge.killerTeam != death.victimTeam || revenge.victim != death.killer {
				continue
			}
			revengeIndex := i + 1 + offset
			tickDelta := revenge.tick - death.tick
			timeDelta := revenge.at - death.at
			productionEligible := insideTradeWindow(death.tick, revenge.tick, round.tickTime, tradeWindow)
			productionCredited := productionTradeVictim(round, revengeIndex) == death.victim
			t.Logf("round=%d trade_candidate death=%s trading_kill=%s->%s death_tick=%d kill_tick=%d tick_delta=%d frame_delta=%d time_delta=%s kill_post_round=%t exact_5s=%t production_window=%t production_credited=%t sequence=%s",
				roundNumber, names[player], revenge.killerName, revenge.victimName, death.tick, revenge.tick,
				tickDelta, revenge.frame-death.frame, timeDelta, revenge.postRound, timeDelta <= 5*time.Second,
				productionEligible, productionCredited, formatTraceSequence(round.kills))
		}
	}
}

func productionTradeVictim(round *hltvRoundTrace, revengeIndex int) uint64 {
	revenge := round.kills[revengeIndex]
	for _, death := range round.kills[:revengeIndex] {
		if death.killer == revenge.victim && death.victimTeam == revenge.killerTeam &&
			insideTradeWindow(death.tick, revenge.tick, round.tickTime, tradeWindow) {
			return death.victim
		}
	}
	return 0
}

func tracePlayerName(player *common.Player) string {
	if player == nil {
		return "World"
	}
	return player.Name
}

func tracePlayerTeam(player *common.Player) common.Team {
	if player == nil {
		return common.TeamUnassigned
	}
	return player.Team
}

func rememberTracePlayer(names map[uint64]string, player *common.Player) {
	if id := trackerID(player); id != 0 {
		names[id] = tracePlayerName(player)
	}
}

func rememberTraceRoster(names map[uint64]string, players []*common.Player) {
	for _, player := range players {
		rememberTracePlayer(names, player)
	}
}

func traceKillCause(event events.Kill) string {
	if event.Weapon == nil {
		return "unknown"
	}
	return event.Weapon.String()
}

func aliveSet(source map[uint64]common.Team) map[uint64]bool {
	result := make(map[uint64]bool, len(source))
	for id := range source {
		result[id] = true
	}
	return result
}

func formatTraceKill(kill hltvKillTrace) string {
	return fmt.Sprintf("%s->%s@%d", kill.killerName, kill.victimName, kill.tick)
}

func formatTraceSequence(kills []hltvKillTrace) string {
	values := make([]string, len(kills))
	for i, kill := range kills {
		values[i] = formatTraceKill(kill)
	}
	return strings.Join(values, ",")
}
