package demoparser

import (
	"testing"

	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

func TestSwingIsZeroSumAndFavoursTheKiller(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	recordKill(rt, 1, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10))
	recordKill(rt, 12, 2, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(20))

	outcome := endRound(rt, common.TeamTerrorists)
	if outcome.swing[1] <= 0 {
		t.Errorf("killer 1 has swing %v, want positive", outcome.swing[1])
	}
	if outcome.swing[11] >= 0 {
		t.Errorf("victim 11 has swing %v, want negative", outcome.swing[11])
	}
	var total float64
	for _, s := range outcome.swing {
		total += s
	}
	if !closeTo(total, 0) {
		t.Errorf("swing sums to %v across the round, want 0", total)
	}
}

// TestSwingDiminishesInWonRounds is the diminishing-returns property: the
// fifth kill of a wipe moves a nearly-decided round less than the opening
// kill moved an even one.
func TestSwingDiminishesInWonRounds(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	var swings []float64
	previous := 0.0
	for _, victim := range []uint64{11, 12, 13, 14, 15} {
		recordKill(rt, 1, victim, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10))
		outcomeSoFar := rt.swing[1]
		swings = append(swings, outcomeSoFar-previous)
		previous = outcomeSoFar
	}
	for i := 1; i < len(swings); i++ {
		if swings[i] >= swings[i-1] {
			t.Errorf("kill %d swung %v, kill %d swung %v; want strictly diminishing",
				i, swings[i-1], i+1, swings[i])
		}
	}
}

func TestPostRoundKillMovesNoSwing(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())
	rt.markEnd(common.TeamCounterTerrorists)

	recordKill(rt, 1, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(100))

	if outcome := rt.finalize(); outcome.swing[1] != 0 || outcome.swing[11] != 0 {
		t.Errorf("exit frag moved swing: killer %v, victim %v", outcome.swing[1], outcome.swing[11])
	}
}

// TestBombPlantShrinksTSwing compares the same T-side kill with and without
// the bomb down. Post-plant the Ts are already favoured, so the same kill
// moves the round less.
func TestBombPlantShrinksTSwing(t *testing.T) {
	killAfterPlant := func(planted bool) float64 {
		rt := newRoundTracker()
		rt.startRound(fiveVsFive())
		if planted {
			rt.plantBomb()
		}
		recordKill(rt, 1, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10))
		return endRound(rt, common.TeamTerrorists).swing[1]
	}

	prePlant, postPlant := killAfterPlant(false), killAfterPlant(true)
	if postPlant >= prePlant {
		t.Errorf("kill swings %v post-plant and %v pre-plant; the plant should already have banked part of the win", postPlant, prePlant)
	}
	if postPlant <= 0 {
		t.Errorf("post-plant kill swings %v, want still positive", postPlant)
	}
}

func TestFortyDamageBecomesARatingAssist(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// Player 2 softens the victim to exactly the threshold, player 3 stays
	// one point short, and player 1 finishes the job.
	rt.damage(2, 11, ratingAssistDamage)
	rt.damage(3, 11, ratingAssistDamage-1)
	recordKill(rt, 1, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10))
	// Ts lose so that survival cannot grant the credit instead.
	for _, victim := range []uint64{1, 2, 3, 4, 5} {
		recordKill(rt, 12, victim, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(20))
	}

	outcome := endRound(rt, common.TeamCounterTerrorists)
	if !outcome.ratingKast[2] {
		t.Errorf("40 damage on a dead enemy earned no rating KAST: %v", outcome.ratingKast)
	}
	if outcome.ratingKast[3] {
		t.Error("39 damage earned rating KAST, the threshold is 40")
	}
	if outcome.kast[2] {
		t.Error("damage assist leaked into the classic KAST, which only counts demo assist events")
	}
}

func TestDamageOnASurvivorIsNoRatingAssist(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	rt.damage(2, 11, 90)
	// Ts lose with no kills; 11 survives, so 2 has no KAST path at all.
	for _, victim := range []uint64{1, 2, 3, 4, 5} {
		recordKill(rt, 11, victim, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(20))
	}

	if outcome := endRound(rt, common.TeamCounterTerrorists); outcome.ratingKast[2] {
		t.Error("damage on a player who survived counted as a rating assist")
	}
}

// TestPostRoundCleanupDeathSettlesNoDamageAssist pins the pairing of the
// cleanup rule with the assist rule: a match-end world cleanup kill leaves
// its victim a survivor, and damage into a survivor is no assist.
func TestPostRoundCleanupDeathSettlesNoDamageAssist(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	// Player 2 softens 11 past the threshold and then dies with no other
	// KAST path; after the whistle, engine cleanup kills 11.
	rt.damage(2, 11, ratingAssistDamage)
	recordKill(rt, 11, 2, common.TeamCounterTerrorists, common.TeamTerrorists, 0, false, at(10))
	rt.markEnd(common.TeamCounterTerrorists)
	recordKill(rt, 0, 11, common.TeamUnassigned, common.TeamCounterTerrorists, 0, true, at(70))

	outcome := rt.finalize()
	if !outcome.survived[11] {
		t.Fatal("the cleanup kill must not cancel player 11's survival")
	}
	if outcome.ratingKast[2] {
		t.Error("damage into the cleanup survivor counted as a rating assist")
	}
}

func TestKillerDamageIsNotAlsoAnAssist(t *testing.T) {
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	rt.damage(1, 11, 100)
	recordKill(rt, 1, 11, common.TeamTerrorists, common.TeamCounterTerrorists, 0, false, at(10))

	if rt.damageAssists[1] {
		t.Error("the killer's own damage counted as an assist on their kill")
	}
}

// TestEcoWeightsFollowTheRoundTier survives an eco player and a full-buy
// player through the same round and expects the eco survival to weigh more
// once the analyser applies the round's tier snapshot to the outcome.
func TestEcoWeightsFollowTheRoundTier(t *testing.T) {
	a := liveAnalyser()
	a.roundTiers[1] = tierStarterPistol
	a.roundTiers[2] = tierRifle1
	rt := newRoundTracker()
	rt.startRound(fiveVsFive())

	a.applyRoundOutcomeWithTiers(endRound(rt, common.TeamTerrorists), a.roundTiers)

	if a.ecoSurvival[1] <= a.ecoSurvival[2] {
		t.Errorf("starter pistol survival weighs %v, full buy %v; want the eco worth more",
			a.ecoSurvival[1], a.ecoSurvival[2])
	}
	if a.ecoSurvival[3] != 1 {
		t.Errorf("survivor with no tier snapshot weighs %v, want the neutral 1", a.ecoSurvival[3])
	}
	if a.ecoKast[1] != a.ecoSurvival[1] {
		t.Errorf("rating KAST weight %v differs from the survival weight %v for the same round",
			a.ecoKast[1], a.ecoSurvival[1])
	}
}
