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
		rt.kill(1, victim, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10+i))
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
		rt.kill(1, victim, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10+i))
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
		rt.kill(1, victim, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10+i))
	}
	rt.kill(1, 2, common.TeamTerrorists, common.TeamTerrorists, 0, false, at(20))

	if outcome := endRound(rt, common.TeamTerrorists); len(outcome.aces) != 0 {
		t.Errorf("teamkill counted towards ace: %v", outcome.aces)
	}
}

// killEnemies has player 1 kill n CTs in one round.
func killEnemies(rt *roundTracker, n int) {
	for i := range n {
		rt.kill(1, uint64(11+i), common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10+i))
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
	rt.kill(1, 16, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(20))

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
	rt.kill(1, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10))
	rt.kill(1, 2, common.TeamTerrorists, common.TeamTerrorists, 0, false, at(12))
	rt.kill(1, 3, common.TeamTerrorists, common.TeamTerrorists, 0, false, at(14))

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

func TestClutchWon(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// CTs kill four Ts, leaving player 1 in a 1v5.
	for i, victim := range []uint64{2, 3, 4, 5} {
		rt.kill(11, victim, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10+i))
	}
	// Player 1 wins the clutch.
	for i, victim := range []uint64{11, 12, 13, 14, 15} {
		rt.kill(1, victim, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(20+i))
	}

	if outcome := endRound(rt, common.TeamTerrorists); outcome.clutcher != 1 {
		t.Errorf("clutcher = %d, want player 1", outcome.clutcher)
	}
}

func TestClutchLostIsNotCounted(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	for i, victim := range []uint64{2, 3, 4, 5} {
		rt.kill(11, victim, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10+i))
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
		rt.kill(1, victim, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10+i))
	}

	if outcome := endRound(rt, common.TeamCounterTerrorists); outcome.clutcher != 11 {
		t.Errorf("clutcher = %d, want player 11 (clutch by defuse needs no kills)", outcome.clutcher)
	}
}

func TestOneVersusOneClutchGoesToWinner(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	for i, victim := range []uint64{2, 3, 4, 5} {
		rt.kill(11, victim, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10+i))
	}
	for i, victim := range []uint64{12, 13, 14, 15} {
		rt.kill(1, victim, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(20+i))
	}
	// 1v1 now: player 1 vs player 11. Player 1 wins.
	rt.kill(1, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(30))

	if outcome := endRound(rt, common.TeamTerrorists); outcome.clutcher != 1 {
		t.Errorf("clutcher = %d, want player 1", outcome.clutcher)
	}
}

func TestTradeKill(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	rt.kill(11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))
	isTrade := rt.kill(2, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(13))
	if !isTrade {
		t.Error("revenge kill 3s after the teammate died should be a trade")
	}

	outcome := endRound(rt, common.TeamCounterTerrorists)
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

	rt.kill(11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))
	if rt.kill(2, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(16)) {
		t.Error("revenge kill 6s later should not be a trade")
	}
	if outcome := endRound(rt, common.TeamCounterTerrorists); len(outcome.deathsTraded) != 0 {
		t.Errorf("death outside the trade window was counted as traded: %v", outcome.deathsTraded)
	}
}

func TestPostRoundDeathCanBeTraded(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	rt.markEnd(common.TeamCounterTerrorists)
	rt.kill(11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))
	isTrade := rt.kill(2, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(13))

	if !isTrade {
		t.Error("post-round death avenged inside the window should be a trade")
	}
	if outcome := rt.finalize(); !outcome.deathsTraded[1] {
		t.Errorf("post-round death was not marked as traded: %v", outcome.deathsTraded)
	}
}

func TestOneTradeKillCanTradeTwoDeaths(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	rt.kill(11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))
	rt.kill(11, 2, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(12))
	tradeKills := 0
	if rt.kill(3, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(14)) {
		tradeKills++
	}

	outcome := endRound(rt, common.TeamTerrorists)
	if tradeKills != 1 {
		t.Errorf("trade kills = %d, want one revenge kill", tradeKills)
	}
	if len(outcome.deathsTraded) != 2 || !outcome.deathsTraded[1] || !outcome.deathsTraded[2] {
		t.Errorf("deaths traded = %v, want players 1 and 2", outcome.deathsTraded)
	}
}

func TestDeathsTradedFollowTheHalftimeSwap(t *testing.T) {
	a := &analyser{
		players:    map[uint64]*DemoPlayer{1: {SteamID: 1}},
		kastRounds: make(map[uint64]SideCount),
	}
	rt := newRoundTracker()

	// Player 1 starts on T, dies, and has that death avenged by player 2.
	rt.startRound(map[uint64]common.Team{
		1: common.TeamTerrorists, 2: common.TeamTerrorists, 11: common.TeamCounterTerrorists,
	})
	rt.kill(11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))
	rt.kill(2, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(13))
	a.applyRoundOutcome(endRound(rt, common.TeamTerrorists))

	// After halftime the same players swap sides and repeat the trade.
	rt.startRound(map[uint64]common.Team{
		1: common.TeamCounterTerrorists, 2: common.TeamCounterTerrorists, 11: common.TeamTerrorists,
	})
	rt.kill(11, 1, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(20))
	rt.kill(2, 11, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(23))
	a.applyRoundOutcome(endRound(rt, common.TeamCounterTerrorists))

	if got, want := a.players[1].DeathsTraded, (SideCount{Total: 2, CT: 1, T: 1}); got != want {
		t.Errorf("deaths traded = %+v, want %+v after the side swap", got, want)
	}
}

func TestKastSurvivorAndUninvolvedVictim(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// Player 5 dies untraded with no kills or assists; everyone else survives.
	rt.kill(11, 5, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))

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

	rt.kill(11, 5, common.TeamCounterTerrorists, common.TeamTerrorists, 12, false, at(10))
	// Assister 12 then dies untraded: KAST must survive through the assist.
	rt.kill(1, 12, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(20))

	if outcome := endRound(rt, common.TeamCounterTerrorists); !outcome.kast[12] {
		t.Error("player 12 assisted and must have KAST")
	}
}

func TestSuicideAndWorldDeaths(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// Killer id 0 marks world deaths and suicides.
	rt.kill(0, 1, common.TeamUnassigned, common.TeamTerrorists, 0, true, at(10))

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

	if rt.kill(1, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10)) {
		t.Error("kill before any round start should not be a trade")
	}
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
	rt.kill(11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(70))

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

	// Dying to the bomb explosion counts as dying in the round (HLTV
	// convention), even though the kill event lands after the whistle.
	rt.kill(0, 1, common.TeamUnassigned, common.TeamTerrorists, 0, false, at(70))

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
	rt.kill(0, 1, common.TeamUnassigned, common.TeamTerrorists, 0, true, at(70))

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
		rt.kill(11, victim, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(70+i))
	}

	if outcome := rt.finalize(); outcome.clutcher != 0 {
		t.Errorf("clutcher = %d, want none after the round was already decided", outcome.clutcher)
	}
}

func TestOpeningDuelGoesToFirstEnemyKill(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	rt.kill(11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))
	rt.kill(2, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(12))

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

	rt.kill(11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))

	if outcome := endRound(rt, common.TeamTerrorists); outcome.openingWon {
		t.Error("the opening killer's team lost the round, openingWon must be false")
	}
}

func TestOpeningDuelSkipsTeamkillsAndWorldDeaths(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// A teamkill and a fall death come first; neither opens the round.
	rt.kill(1, 2, common.TeamTerrorists, common.TeamTerrorists, 0, false, at(5))
	rt.kill(0, 3, common.TeamUnassigned, common.TeamTerrorists, 0, true, at(8))
	rt.kill(11, 4, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))

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
	rt.kill(11, 1, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(70))

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
