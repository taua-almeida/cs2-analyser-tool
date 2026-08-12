package demoparser

import (
	"slices"
	"testing"
	"time"

	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// T players are 1..5, CT players are 11..15.
func fiveVsFive() map[uint64]common.Team {
	alive := make(map[uint64]common.Team)
	for id := uint64(1); id <= 5; id++ {
		alive[id] = common.TeamTerrorists
	}
	for id := uint64(11); id <= 15; id++ {
		alive[id] = common.TeamCounterTerrorists
	}
	return alive
}

func at(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}

func recordKill(rt *roundTracker, killer, victim uint64, killerTeam, victimTeam common.Team, assister uint64, byWorld bool, at time.Duration) {
	rt.kill(killer, victim, killerTeam, victimTeam, assister, false, byWorld, int(at/time.Millisecond), time.Millisecond)
}

// endRound decides and immediately finalizes a round, for tests that have
// no post-round events.
func endRound(rt *roundTracker, winner common.Team) roundOutcome {
	rt.markEnd(winner)
	return rt.finalize()
}

func TestAce(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	for i, victim := range []uint64{11, 12, 13, 14, 15} {
		recordKill(rt, 1, victim, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10+i))
	}

	outcome := endRound(rt, common.TeamTerrorists)
	if !outcome.played {
		t.Fatal("round should have been played")
	}
	if !slices.Contains(outcome.aces, uint64(1)) {
		t.Errorf("player 1 killed all five CTs, aces = %v", outcome.aces)
	}
	if !outcome.kast[1] {
		t.Error("player 1 should have KAST for the round")
	}
}

func TestFourKillsIsNotAce(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	for i, victim := range []uint64{11, 12, 13, 14} {
		recordKill(rt, 1, victim, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10+i))
	}

	if outcome := endRound(rt, common.TeamTerrorists); len(outcome.aces) != 0 {
		t.Errorf("four kills counted as ace: %v", outcome.aces)
	}
}

func TestTeamkillsDoNotMakeAnAce(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// Four enemy kills plus a teamkill must not be an ace.
	for i, victim := range []uint64{11, 12, 13, 14} {
		recordKill(rt, 1, victim, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10+i))
	}
	recordKill(rt, 1, 2, common.TeamTerrorists, common.TeamTerrorists, 0, false, at(20))

	if outcome := endRound(rt, common.TeamTerrorists); len(outcome.aces) != 0 {
		t.Errorf("teamkill counted towards ace: %v", outcome.aces)
	}
}

// killEnemies has player 1 kill n CTs in one round.
func killEnemies(rt *roundTracker, n int) {
	for i := range n {
		recordKill(rt, 1, uint64(11+i), common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10+i))
	}
}

// bucketOf runs a round in which player 1 gets n enemy kills and reports
// which multi-kill bucket it landed in, or 0 for no multi-kill at all.
// Landing in two buckets is the failure the exclusivity test is after, so
// it is reported here rather than silently collapsed.
func bucketOf(t *testing.T, n int) int {
	t.Helper()
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())
	killEnemies(rt, n)

	var got MultiKillRounds
	got.add(endRound(rt, common.TeamTerrorists).multiKills[1])
	switch got {
	case MultiKillRounds{}:
		return 0
	case MultiKillRounds{K2: 1}:
		return 2
	case MultiKillRounds{K3: 1}:
		return 3
	case MultiKillRounds{K4: 1}:
		return 4
	case MultiKillRounds{K5: 1}:
		return 5
	}
	t.Fatalf("%d enemy kills landed in more than one bucket: %+v", n, got)
	return 0
}

func TestMultiKillBuckets(t *testing.T) {
	// Buckets are exclusive: every round lands in exactly one of them, and
	// a single kill is no multi-kill at all.
	cases := []struct{ kills, bucket int }{
		{0, 0}, {1, 0}, {2, 2}, {3, 3}, {4, 4}, {5, 5},
	}
	for _, c := range cases {
		if got := bucketOf(t, c.kills); got != c.bucket {
			t.Errorf("%d enemy kills landed in bucket %d, want %d (0 = no multi-kill)", c.kills, got, c.bucket)
		}
	}
}

func TestMultiKillTopBucketMatchesAce(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// Six enemy kills need a CT who reconnected, but if it ever happens the
	// round has to stay a single 5k so the bucket cannot drift from ACEs.
	killEnemies(rt, 5)
	recordKill(rt, 1, 16, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(20))

	outcome := endRound(rt, common.TeamTerrorists)
	var got MultiKillRounds
	got.add(outcome.multiKills[1])
	if want := (MultiKillRounds{K5: 1}); got != want {
		t.Errorf("six kills gave %+v, want %+v", got, want)
	}
	if !slices.Contains(outcome.aces, uint64(1)) {
		t.Errorf("six kills must also be an ace, aces = %v", outcome.aces)
	}
}

func TestTeamkillsDoNotMakeAMultiKill(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// One enemy kill plus two teamkills is not a 3k, and not even a 2k.
	recordKill(rt, 1, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10))
	recordKill(rt, 1, 2, common.TeamTerrorists, common.TeamTerrorists, 0, false, at(12))
	recordKill(rt, 1, 3, common.TeamTerrorists, common.TeamTerrorists, 0, false, at(14))

	if outcome := endRound(rt, common.TeamTerrorists); len(outcome.multiKills) != 0 {
		t.Errorf("teamkills counted towards a multi-kill: %v", outcome.multiKills)
	}
}

func TestMultiKillCountsPostRoundKills(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())
	rt.markEnd(common.TeamTerrorists)

	// Exit frags are enemy kills like any other, the same way they already
	// count towards an ace.
	killEnemies(rt, 2)

	if outcome := rt.finalize(); outcome.multiKills[1] != 2 {
		t.Errorf("multiKills[1] = %d, want 2 post-round kills to count", outcome.multiKills[1])
	}
}

func TestFreezeTimeJoinKeepsSideAndDoesNotRevive(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())
	rt.disconnect(5)

	// Player 16 picks a side during freeze time and player 1 swaps to CT,
	// while player 5 is already gone. The refresh still lists all of them.
	roster := fiveVsFive()
	roster[16] = common.TeamCounterTerrorists
	roster[1] = common.TeamCounterTerrorists
	rt.joinRound(roster)

	outcome := endRound(rt, common.TeamCounterTerrorists)
	if outcome.participants[16] != common.TeamCounterTerrorists {
		t.Errorf("player 16 joined during freeze time and must play the round on CT, got %v", outcome.participants[16])
	}
	if !outcome.kast[16] {
		t.Error("player 16 survived the round they joined and must have KAST")
	}
	if outcome.participants[1] != common.TeamCounterTerrorists {
		t.Errorf("player 1 swapped sides during freeze time, got %v", outcome.participants[1])
	}
	if outcome.kast[5] {
		t.Error("player 5 left during freeze time and must not be revived into a survival")
	}
}

func TestFreezeTimeJoinerTradedDeathUsesJoinedSide(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(map[uint64]common.Team{
		1: common.TeamTerrorists, 11: common.TeamCounterTerrorists,
	})
	rt.joinRound(map[uint64]common.Team{
		1: common.TeamTerrorists, 11: common.TeamCounterTerrorists, 16: common.TeamCounterTerrorists,
	})

	recordKill(rt, 1, 16, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10))
	recordKill(rt, 11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(13))
	outcome := endRound(rt, common.TeamCounterTerrorists)

	a := liveAnalyser()
	a.players[16] = &DemoPlayer{SteamID: 16}
	a.applyRoundOutcomeWithTiers(outcome, a.roundTiers)
	if got, want := a.players[16].DeathsTraded, (SideCount{Total: 1, CT: 1}); got != want {
		t.Errorf("freeze-time joiner's deaths traded = %+v, want %+v", got, want)
	}
}

func TestClutchWon(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// CTs kill four Ts, leaving player 1 in a 1v5.
	for i, victim := range []uint64{2, 3, 4, 5} {
		recordKill(rt, 11, victim, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10+i))
	}
	// Player 1 wins the clutch.
	for i, victim := range []uint64{11, 12, 13, 14, 15} {
		recordKill(rt, 1, victim, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(20+i))
	}

	if outcome := endRound(rt, common.TeamTerrorists); outcome.clutcher != 1 {
		t.Errorf("clutcher = %d, want player 1", outcome.clutcher)
	}
}

func TestClutchLostIsNotCounted(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	for i, victim := range []uint64{2, 3, 4, 5} {
		recordKill(rt, 11, victim, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10+i))
	}

	// CTs win: their side never was in a clutch, so nobody gets one.
	if outcome := endRound(rt, common.TeamCounterTerrorists); outcome.clutcher != 0 {
		t.Errorf("clutcher = %d, want none", outcome.clutcher)
	}
}

func TestClutchWonWithoutKills(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// Ts kill four CTs; player 11 is alone in a 1v5 and wins by defuse.
	for i, victim := range []uint64{12, 13, 14, 15} {
		recordKill(rt, 1, victim, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10+i))
	}

	if outcome := endRound(rt, common.TeamCounterTerrorists); outcome.clutcher != 11 {
		t.Errorf("clutcher = %d, want player 11 (clutch by defuse needs no kills)", outcome.clutcher)
	}
}

func TestOneVersusOneClutchGoesToWinner(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	for i, victim := range []uint64{2, 3, 4, 5} {
		recordKill(rt, 11, victim, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10+i))
	}
	for i, victim := range []uint64{12, 13, 14, 15} {
		recordKill(rt, 1, victim, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(20+i))
	}
	// 1v1 now: player 1 vs player 11. Player 1 wins.
	recordKill(rt, 1, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(30))

	if outcome := endRound(rt, common.TeamTerrorists); outcome.clutcher != 1 {
		t.Errorf("clutcher = %d, want player 1", outcome.clutcher)
	}
}

func TestTradeKill(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	recordKill(rt, 11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))
	recordKill(rt, 2, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(13))

	outcome := endRound(rt, common.TeamCounterTerrorists)
	if outcome.tradeKills[2] != 1 {
		t.Errorf("trade kills = %v, want one for player 2", outcome.tradeKills)
	}
	if len(outcome.deathsTraded) != 1 || !outcome.deathsTraded[1] {
		t.Errorf("deaths traded = %v, want only player 1 counted once", outcome.deathsTraded)
	}
	if !outcome.kast[1] {
		t.Error("player 1's death was traded, so player 1 keeps KAST")
	}
}

func TestKillOutsideTradeWindowIsNoTrade(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	recordKill(rt, 11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))
	recordKill(rt, 2, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(16))
	if outcome := endRound(rt, common.TeamCounterTerrorists); len(outcome.deathsTraded) != 0 || len(outcome.tradeKills) != 0 {
		t.Errorf("outside-window trade = deaths %v, kills %v; want none", outcome.deathsTraded, outcome.tradeKills)
	}
}

func TestPostRoundDeathCanBeTraded(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	rt.markEnd(common.TeamCounterTerrorists)
	recordKill(rt, 11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))
	recordKill(rt, 2, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(13))

	if outcome := rt.finalize(); !outcome.deathsTraded[1] || outcome.tradeKills[2] != 1 {
		t.Errorf("post-round trade = deaths %v, kills %v; want player 1 traded by player 2", outcome.deathsTraded, outcome.tradeKills)
	}
}

func TestOneTradeKillCreditsTheOldestEligibleDeath(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	recordKill(rt, 11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))
	recordKill(rt, 11, 2, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(12))
	recordKill(rt, 3, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(14))

	outcome := endRound(rt, common.TeamTerrorists)
	if outcome.tradeKills[3] != 1 {
		t.Errorf("trade kills = %v, want one for player 3", outcome.tradeKills)
	}
	if len(outcome.deathsTraded) != 1 || !outcome.deathsTraded[1] {
		t.Errorf("deaths traded = %v, want only the oldest eligible death, player 1", outcome.deathsTraded)
	}
}

func TestTradeWindowUsesTickIntervals(t *testing.T) {
	const tick = time.Second / 64
	tests := []struct {
		name    string
		delta   int
		isTrade bool
	}{
		{name: "immediately below boundary", delta: 320, isTrade: true},
		{name: "on boundary", delta: 321, isTrade: true},
		{name: "immediately above boundary", delta: 322},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := insideTradeWindow(10_000, 10_000+tt.delta, tick, tradeWindow)
			if got != tt.isTrade {
				t.Errorf("trade %d ticks later with %s ticks = %t, want %t", tt.delta, tick, got, tt.isTrade)
			}
		})
	}
}

func TestTradeWindowIsInvariantToDemoClockPhase(t *testing.T) {
	const tick = time.Second / 64
	for _, start := range []int{0, 76_714, 16_000_000} {
		if !insideTradeWindow(start, start+321, tick, tradeWindow) {
			t.Errorf("boundary trade changed at absolute tick %d", start)
		}
		if insideTradeWindow(start, start+322, tick, tradeWindow) {
			t.Errorf("outside trade changed at absolute tick %d", start)
		}
	}
}

func TestTradeWindowIgnoresFloat32AbsoluteTimestampRounding(t *testing.T) {
	const tickSeconds = float32(1.0 / 64.0)
	deltas := make(map[time.Duration]bool)
	for _, start := range []int{1, 1_000_000, 16_000_000, 100_000_000} {
		deathTime := time.Duration(float32(start) * tickSeconds * float32(time.Second))
		revengeTime := time.Duration(float32(start+321) * tickSeconds * float32(time.Second))
		deltas[revengeTime-deathTime] = true
		if !insideTradeWindow(start, start+321, time.Second/64, tradeWindow) {
			t.Errorf("float32-derived times %s and %s changed a tick-normalized boundary trade", deathTime, revengeTime)
		}
	}
	if len(deltas) < 2 {
		t.Fatalf("test phases produced one float32 elapsed time %v; fixture does not exercise absolute-clock rounding", deltas)
	}
}

func TestTradeWindowPreservesFractionalConfiguration(t *testing.T) {
	const (
		tick   = 16 * time.Millisecond
		window = 5*time.Second + 7*time.Millisecond
	)
	if !insideTradeWindow(100, 413, tick, window) { // minimum elapsed: 4.992s
		t.Error("last tick below a fractional window was excluded")
	}
	if insideTradeWindow(100, 414, tick, window) { // minimum elapsed: 5.008s
		t.Error("first tick above a fractional window was included")
	}
}

func TestTradeWindowRejectsUnavailableResolution(t *testing.T) {
	for _, tickTime := range []time.Duration{0, -1} {
		if insideTradeWindow(100, 101, tickTime, tradeWindow) {
			t.Errorf("tick duration %s produced a trade", tickTime)
		}
	}
}

func TestDeathsTradedFollowTheHalftimeSwap(t *testing.T) {
	a := liveAnalyser()
	a.players[1] = &DemoPlayer{SteamID: 1}
	rt := newRoundTracker()

	// Player 1 starts on T, dies, and has that death avenged by player 2.
	rt.startRound(map[uint64]common.Team{
		1: common.TeamTerrorists, 2: common.TeamTerrorists, 11: common.TeamCounterTerrorists,
	})
	recordKill(rt, 11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))
	recordKill(rt, 2, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(13))
	a.applyRoundOutcomeWithTiers(endRound(rt, common.TeamTerrorists), a.roundTiers)

	// After halftime the same players swap sides and repeat the trade.
	rt.startRound(map[uint64]common.Team{
		1: common.TeamCounterTerrorists, 2: common.TeamCounterTerrorists, 11: common.TeamTerrorists,
	})
	recordKill(rt, 11, 1, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(20))
	recordKill(rt, 2, 11, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(23))
	a.applyRoundOutcomeWithTiers(endRound(rt, common.TeamCounterTerrorists), a.roundTiers)

	if got, want := a.players[1].DeathsTraded, (SideCount{Total: 2, CT: 1, T: 1}); got != want {
		t.Errorf("deaths traded = %+v, want %+v after the side swap", got, want)
	}
}

func TestKastSurvivorAndUninvolvedVictim(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// Player 5 dies untraded with no kills or assists; everyone else survives.
	recordKill(rt, 11, 5, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))

	outcome := endRound(rt, common.TeamCounterTerrorists)
	if outcome.kast[5] {
		t.Error("player 5 died with no kill/assist/trade and must not have KAST")
	}
	if !outcome.kast[2] {
		t.Error("player 2 survived and must have KAST")
	}
	if !outcome.kast[11] {
		t.Error("player 11 killed and must have KAST")
	}
}

func TestAssisterGetsKast(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	recordKill(rt, 11, 5, common.TeamCounterTerrorists, common.TeamTerrorists, 12, false, at(10))
	// Assister 12 then dies untraded: KAST must survive through the assist.
	recordKill(rt, 1, 12, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(20))

	if outcome := endRound(rt, common.TeamCounterTerrorists); !outcome.kast[12] {
		t.Error("player 12 assisted and must have KAST")
	}
}

func TestFlashAssistOnlyCountsForRatingKAST(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	rt.kill(11, 5, common.TeamCounterTerrorists, common.TeamTerrorists, 12, true, false, 10, time.Second)
	// The flash assister then dies without another qualifying classic-KAST fact.
	recordKill(rt, 1, 12, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(20))

	outcome := endRound(rt, common.TeamCounterTerrorists)
	if outcome.kast[12] {
		t.Error("flash assist alone must not qualify classic KAST")
	}
	if !outcome.ratingKast[12] {
		t.Error("flash assist must remain a rating-KAST assist")
	}
}

func TestSuicideAndWorldDeaths(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// Killer id 0 marks world deaths and suicides.
	recordKill(rt, 0, 1, common.TeamUnassigned, common.TeamTerrorists, 0, true, at(10))

	outcome := endRound(rt, common.TeamCounterTerrorists)
	if len(outcome.aces) != 0 {
		t.Errorf("world death produced aces: %v", outcome.aces)
	}
	if outcome.kast[1] {
		t.Error("player 1 fell to death uninvolved and must not have KAST")
	}
}

func TestDisconnectCanStartClutch(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// Four Ts rage quit, leaving player 5 alone versus five CTs.
	for _, id := range []uint64{1, 2, 3, 4} {
		rt.disconnect(id)
	}

	if outcome := endRound(rt, common.TeamTerrorists); outcome.clutcher != 5 {
		t.Errorf("clutcher = %d, want player 5", outcome.clutcher)
	}
}

func TestPostRoundDisconnectKeepsSurvival(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())
	rt.markEnd(common.TeamCounterTerrorists)

	// Player 11 leaves right after the final whistle.
	rt.disconnect(11)

	if outcome := rt.finalize(); !outcome.kast[11] {
		t.Error("player 11 survived the round and leaving post-round must not cancel it")
	}
}

func TestEventsBeforeRoundStartAreIgnored(t *testing.T) {
	rt := newRoundTracker()

	recordKill(rt, 1, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10))
	rt.remove(11)
	if outcome := endRound(rt, common.TeamTerrorists); outcome.played {
		t.Error("round end without round start should not count as played")
	}
}

func TestPostRoundExitFragCancelsSurvival(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())
	rt.markEnd(common.TeamCounterTerrorists)

	// A real enemy kill after the round is decided still cancels survival.
	recordKill(rt, 11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(70))

	outcome := rt.finalize()
	if outcome.kast[1] {
		t.Error("player 1 was exit-fragged post-round and must not get survival KAST")
	}
	if !outcome.kast[2] {
		t.Error("player 2 survived the whole round and must have KAST")
	}
}

func TestPostRoundBombDeathCancelsSurvival(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())
	rt.markEnd(common.TeamCounterTerrorists)

	// The event still cancels survival. Raw totals apply their separate
	// round/game-phase rule in the analyser.
	recordKill(rt, 0, 1, common.TeamUnassigned, common.TeamTerrorists, 0, false, at(70))

	if outcome := rt.finalize(); outcome.kast[1] {
		t.Error("player 1 died to the bomb and must not get survival KAST")
	}
}

func TestMatchEndWorldDeathKeepsSurvival(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())
	rt.markEnd(common.TeamCounterTerrorists)

	// Match-end cleanup kills (weapon World, no killer) are artifacts, not
	// real deaths.
	recordKill(rt, 0, 1, common.TeamUnassigned, common.TeamTerrorists, 0, true, at(70))

	if outcome := rt.finalize(); !outcome.kast[1] {
		t.Error("player 1 survived the round; the match-end world kill must not cancel it")
	}
}

func TestNoClutchCandidacyAfterRoundDecided(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())
	rt.markEnd(common.TeamTerrorists)

	// Post-round exit frags leave player 1 as the only T alive.
	for i, victim := range []uint64{2, 3, 4, 5} {
		recordKill(rt, 11, victim, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(70+i))
	}

	if outcome := rt.finalize(); outcome.clutcher != 0 {
		t.Errorf("clutcher = %d, want none after the round was already decided", outcome.clutcher)
	}
}

func TestOpeningDuelGoesToFirstEnemyKill(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	recordKill(rt, 11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))
	recordKill(rt, 2, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(12))

	outcome := endRound(rt, common.TeamCounterTerrorists)
	want := openingDuel{killer: 11, victim: 1, killerTeam: common.TeamCounterTerrorists, victimTeam: common.TeamTerrorists}
	if outcome.opening != want {
		t.Errorf("opening = %+v, want %+v", outcome.opening, want)
	}
	if !outcome.openingWon {
		t.Error("player 11's team took the opening kill and won, openingWon must be true")
	}
}

func TestOpeningDuelInLostRoundIsNotWon(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	recordKill(rt, 11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))

	if outcome := endRound(rt, common.TeamTerrorists); outcome.openingWon {
		t.Error("the opening killer's team lost the round, openingWon must be false")
	}
}

func TestOpeningDuelSkipsTeamkillsAndWorldDeaths(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// A teamkill and a fall death come first; neither opens the round.
	recordKill(rt, 1, 2, common.TeamTerrorists, common.TeamTerrorists, 0, false, at(5))
	recordKill(rt, 0, 3, common.TeamUnassigned, common.TeamTerrorists, 0, true, at(8))
	recordKill(rt, 11, 4, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))

	outcome := endRound(rt, common.TeamCounterTerrorists)
	if outcome.opening.killer != 11 || outcome.opening.victim != 4 {
		t.Errorf("opening = %+v, want the first enemy kill (11 on 4)", outcome.opening)
	}
}

func TestRoundWithoutKillsHasNoOpeningDuel(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	outcome := endRound(rt, common.TeamCounterTerrorists)
	if outcome.opening.killer != 0 {
		t.Errorf("opening = %+v, want none", outcome.opening)
	}
	if outcome.openingWon {
		t.Error("a round without an opening kill cannot be an opening success")
	}
}

func TestPostRoundKillIsNotAnOpeningDuel(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())
	rt.markEnd(common.TeamCounterTerrorists)

	// An exit frag in an otherwise killless round is no entry.
	recordKill(rt, 11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(70))

	if outcome := rt.finalize(); outcome.opening.killer != 0 {
		t.Errorf("opening = %+v, want none after the round was decided", outcome.opening)
	}
}

func TestGetPlayerBestWeapon(t *testing.T) {
	weapon := GetPlayerBestWeapon(map[string]int{"AK-47": 12, "AWP": 7, "Glock-18": 1})
	if weapon != "AK-47" {
		t.Errorf("best weapon = %q, want AK-47", weapon)
	}
	if GetPlayerBestWeapon(nil) != "" {
		t.Error("no kills should give no best weapon")
	}
}

func TestGetPlayersToAnalyseMatchesCaseInsensitive(t *testing.T) {
	players := map[uint64]*DemoPlayer{
		100: {SteamID: 100, Name: "s1mple"},
		200: {SteamID: 200, Name: "NiKo"},
	}
	got := GetPlayersToAnalyse(players, []string{"S1MPLE"})
	if len(got) != 1 || got[100] == nil {
		t.Errorf("expected to match s1mple case-insensitively, got %v", got)
	}
}
