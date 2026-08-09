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
