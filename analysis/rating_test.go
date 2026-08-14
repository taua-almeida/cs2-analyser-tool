package analysis

import (
	"math"
	"testing"

	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

func inventoryOf(types ...common.EquipmentType) map[int]*common.Equipment {
	inventory := make(map[int]*common.Equipment, len(types))
	for i, t := range types {
		inventory[i] = &common.Equipment{Type: t}
	}
	return inventory
}

func TestPlayerTierTakesTheStrongestWeapon(t *testing.T) {
	cases := []struct {
		name      string
		inventory map[int]*common.Equipment
		want      equipTier
	}{
		{"full rifle buy", inventoryOf(common.EqKnife, common.EqUSP, common.EqAK47, common.EqFlash), tierRifle1},
		{"awp beats the rifle beside it", inventoryOf(common.EqAWP, common.EqM4A4), tierSniper},
		{"force buy", inventoryOf(common.EqKnife, common.EqDeagle), tierUpgradedPistol},
		{"smg beats its pistol", inventoryOf(common.EqGlock, common.EqMac10), tierSMG},
		{"scout is a tier-2 rifle", inventoryOf(common.EqSSG08, common.EqTec9), tierRifle2},
		{"eco", inventoryOf(common.EqKnife, common.EqGlock), tierStarterPistol},
		{"knife only is a starter, not unknown", inventoryOf(common.EqKnife), tierStarterPistol},
		{"no inventory data", nil, tierUnknown},
	}
	for _, c := range cases {
		if got := playerTier(c.inventory); got != c.want {
			t.Errorf("%s: playerTier = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDuelWinProbIsAProbability(t *testing.T) {
	tiers := []equipTier{tierStarterPistol, tierUpgradedPistol, tierSMG, tierRifle2, tierRifle1, tierSniper}
	for _, a := range tiers {
		if p := duelWinProb(a, a); p != 0.5 {
			t.Errorf("even duel at tier %v has win prob %v, want 0.5", a, p)
		}
		for _, b := range tiers {
			pa, pb := duelWinProb(a, b), duelWinProb(b, a)
			if !closeTo(pa+pb, 1) {
				t.Errorf("duelWinProb(%v,%v)=%v and reverse=%v do not sum to 1", a, b, pa, pb)
			}
			if a > b && pa <= 0.5 {
				t.Errorf("higher tier %v beats %v with prob %v, want > 0.5", a, b, pa)
			}
		}
	}
	if p := duelWinProb(tierUnknown, tierSniper); p != 0.5 {
		t.Errorf("unknown tier should give an even duel, got %v", p)
	}
}

// TestKillPointsMatchHLTVAnchors pins the two duel values HLTV has published
// for Rating 3.0: an even-economy kill is worth ~1.10 points and a tier-1
// rifle kill on a starter pistol ~0.54. The strengths in tierStrength were
// chosen to hit these anchors, so a change here means recalibrating, not
// adjusting the test.
func TestKillPointsMatchHLTVAnchors(t *testing.T) {
	if got := killPoints(tierRifle1, tierRifle1); !closeTo(got, 1.10) {
		t.Errorf("even duel kill worth %v points, want 1.10", got)
	}
	if got := killPoints(tierRifle1, tierStarterPistol); math.Abs(got-0.54) > 0.01 {
		t.Errorf("rifle kill on a starter pistol worth %.3f points, want ~0.54", got)
	}
	upset := killPoints(tierStarterPistol, tierRifle1)
	if upset <= 1.10 {
		t.Errorf("pistol upset over a rifle worth %v points, want more than an even duel", upset)
	}
}

func TestTWinProbabilityAnchors(t *testing.T) {
	cases := []struct {
		name         string
		tAlive, ct   int
		bombPlanted  bool
		want         float64
		wantAbsError float64
	}{
		{"no CTs left is a T win", 3, 0, false, 1, 0},
		{"no Ts and no bomb is a T loss", 0, 3, false, 0, 0},
		{"no Ts with the bomb down still leans CT", 0, 3, true, 0.05, 0},
		{"bomb down with nobody alive explodes for the Ts", 0, 0, true, 1, 0},
		{"even 5v5 leans slightly CT", 5, 5, false, 0.48, 0.001},
		{"post-plant 5v5 leans T", 5, 5, true, 0.69, 0.01},
		{"oversized counts clamp to the 5v5 table", 8, 7, false, 0.48, 0.001},
	}
	for _, c := range cases {
		got := tWinProbability(c.tAlive, c.ct, c.bombPlanted)
		if math.Abs(got-c.want) > c.wantAbsError {
			t.Errorf("%s: tWinProbability(%d,%d,%v) = %v, want %v",
				c.name, c.tAlive, c.ct, c.bombPlanted, got, c.want)
		}
	}
}

// TestTWinProbabilityIsMonotonic checks the property round swing depends
// on: a teammate dying can never help, an enemy dying can never hurt, and
// the planted bomb never favours the CTs. Without it a kill could be worth
// negative swing.
func TestTWinProbabilityIsMonotonic(t *testing.T) {
	for tAlive := 0; tAlive <= 5; tAlive++ {
		for ct := 0; ct <= 5; ct++ {
			for _, bomb := range []bool{false, true} {
				p := tWinProbability(tAlive, ct, bomb)
				if p < 0 || p > 1 {
					t.Fatalf("tWinProbability(%d,%d,%v) = %v is not a probability", tAlive, ct, bomb, p)
				}
				if tAlive > 0 && tWinProbability(tAlive-1, ct, bomb) > p {
					t.Errorf("losing a T raises the T win chance at %dv%d bomb=%v", tAlive, ct, bomb)
				}
				if ct > 0 && tWinProbability(tAlive, ct-1, bomb) < p {
					t.Errorf("losing a CT lowers the T win chance at %dv%d bomb=%v", tAlive, ct, bomb)
				}
				if !bomb && tWinProbability(tAlive, ct, true) < p {
					t.Errorf("planting the bomb lowers the T win chance at %dv%d", tAlive, ct)
				}
			}
		}
	}
}

func TestTeamWinProbabilitiesSumToOne(t *testing.T) {
	pT := teamWinProbability(common.TeamTerrorists, 3, 4, true)
	pCT := teamWinProbability(common.TeamCounterTerrorists, 3, 4, true)
	if !closeTo(pT+pCT, 1) {
		t.Errorf("perspectives sum to %v, want 1", pT+pCT)
	}
}

// TestBlendRatingOfBaselineAveragesIsOne is the calibration contract: a
// player who produces exactly the baseline on every axis is the definition
// of average and must rate exactly 1.00, which also proves the blend
// weights sum to 1.
func TestBlendRatingOfBaselineAveragesIsOne(t *testing.T) {
	got := blendRating(ratingRound{
		killPoints: baselineKillPoints,
		ecoDamage:  baselineEcoDamage,
		survival:   baselineSurvival,
		kast:       baselineKast,
		multiKill:  baselineMultiKill,
		swing:      0,
	})
	for name, sub := range map[string]float64{
		"kills": got.Kills, "damage": got.Damage, "survival": got.Survival,
		"kast": got.KAST, "multi_kill": got.MultiKill, "round_swing": got.RoundSwing,
	} {
		if !closeTo(sub, 1) {
			t.Errorf("baseline %s sub-rating = %v, want 1", name, sub)
		}
	}
	if !closeTo(got.Value, 1) {
		t.Errorf("baseline rating = %v, want exactly 1.00", got.Value)
	}
}

func TestBlendRatingFloorsSwingAtZero(t *testing.T) {
	got := blendRating(ratingRound{swing: -10})
	if got.RoundSwing != 0 {
		t.Errorf("hopeless swing gives sub-rating %v, want the 0 floor", got.RoundSwing)
	}
}

func TestDeriveDividesRatingInputsByMatchRounds(t *testing.T) {
	a := liveAnalyser()
	a.players[1] = &DemoPlayer{SteamID: 1}
	// Two matches' worth of kill points in half the rounds is twice average.
	a.ecoKills[1] = baselineKillPoints * 8

	a.derive(4)

	if got := a.players[1].Rating.Kills; !closeTo(got, 2) {
		t.Errorf("kills sub-rating = %v, want 2", got)
	}
	if a.players[1].Rating.Value <= 0 {
		t.Error("rating value should be positive once any axis contributes")
	}
}

// TestDeriveSharesApproxPercentagesWithTheSubRatings pins the contract of
// approx_ekast_percent and approx_round_swing_percent: they publish the same
// per-round values blendRating normalizes, so the documented algebra between
// percentage and sub-rating holds exactly. Both accumulators are diluted by
// the whole match's rounds, never a participant count, matching ADR.
func TestDeriveSharesApproxPercentagesWithTheSubRatings(t *testing.T) {
	a := liveAnalyser()
	a.players[1] = &DemoPlayer{SteamID: 1}
	a.players[2] = &DemoPlayer{SteamID: 2}
	// Player 1 hits the baseline exactly; player 2 gives back the same
	// swing player 1 gained and, as a late joiner, holds two full-buy
	// KAST credits over the eight-round match.
	a.ecoKast[1] = baselineKast * 8
	a.roundSwing[1] = 0.32
	a.ecoKast[2] = 2
	a.roundSwing[2] = -0.32

	a.derive(8)

	one, two := a.players[1].PlayerMapStats, a.players[2].PlayerMapStats
	if !closeTo(one.ApproxEKASTPercent, 100*baselineKast) {
		t.Errorf("baseline eKAST percent = %v, want %v", one.ApproxEKASTPercent, 100*baselineKast)
	}
	if got := a.players[1].Rating.KAST; !closeTo(got, 1) {
		t.Errorf("baseline eKAST sub-rating = %v, want 1.00", got)
	}
	if !closeTo(one.ApproxRoundSwingPercent, 4) {
		t.Errorf("swing percent = %v, want +4.0", one.ApproxRoundSwingPercent)
	}
	if got := a.players[1].Rating.RoundSwing; !closeTo(got, 1.10) {
		t.Errorf("swing sub-rating for +4%% = %v, want 1.10", got)
	}
	if !closeTo(two.ApproxRoundSwingPercent, -4) {
		t.Errorf("negative swing percent = %v, want -4.0", two.ApproxRoundSwingPercent)
	}
	if got := a.players[2].Rating.RoundSwing; !closeTo(got, 0.90) {
		t.Errorf("swing sub-rating for -4%% = %v, want 0.90", got)
	}
	if !closeTo(two.ApproxEKASTPercent, 25) {
		t.Errorf("late joiner eKAST percent = %v, want 25 from 2 credits over all 8 match rounds", two.ApproxEKASTPercent)
	}
	if got := a.players[2].Rating.KAST; !closeTo(got, 0.25/baselineKast) {
		t.Errorf("late joiner eKAST sub-rating = %v, want %v", got, 0.25/baselineKast)
	}
}

// TestApproxEKASTOfFullBuyAndEcoCredit walks the two documented anchors: a
// full-buy credit every round is exactly 100% (sub-rating ~1.27), and eco
// weighting pushes the percentage past 100 without being clamped.
func TestApproxEKASTOfFullBuyAndEcoCredit(t *testing.T) {
	a := liveAnalyser()
	a.players[1] = &DemoPlayer{SteamID: 1}
	a.players[2] = &DemoPlayer{SteamID: 2}
	a.ecoKast[1] = 2 * ecoRoundWeight(tierRifle1)
	a.ecoKast[2] = 2 * ecoRoundWeight(tierStarterPistol)

	a.derive(2)

	if got := a.players[1].PlayerMapStats.ApproxEKASTPercent; !closeTo(got, 100) {
		t.Errorf("full-buy credit every round = %v%%, want exactly 100", got)
	}
	if got := a.players[1].Rating.KAST; !closeTo(got, 1/baselineKast) {
		t.Errorf("full-buy eKAST sub-rating = %v, want %v (~1.27)", got, 1/baselineKast)
	}
	got := a.players[2].PlayerMapStats.ApproxEKASTPercent
	if want := 100 * ecoRoundWeight(tierStarterPistol); !closeTo(got, want) {
		t.Errorf("eco credit every round = %v%%, want the unclamped %v (above 100)", got, want)
	}
}

func TestDeriveZeroRoundsProducesZeroApproxMetrics(t *testing.T) {
	a := liveAnalyser()
	a.players[1] = &DemoPlayer{SteamID: 1}
	a.ecoKast[1] = 3
	a.roundSwing[1] = -0.5

	a.derive(0)

	stats := a.players[1].PlayerMapStats
	for name, got := range map[string]float64{
		"approx_ekast_percent":       stats.ApproxEKASTPercent,
		"approx_round_swing_percent": stats.ApproxRoundSwingPercent,
	} {
		// NaN and ±Inf both fail this comparison, so one check covers
		// "zero, and never a division artifact".
		if got != 0 {
			t.Errorf("%s = %v over zero rounds, want a finite 0", name, got)
		}
	}
}

// TestExtremeNegativeSwingStaysSignedWhileSubRatingFloors preserves the
// information the sub-rating's floor discards: the percentage must not be
// derived backward from the floored value.
func TestExtremeNegativeSwingStaysSignedWhileSubRatingFloors(t *testing.T) {
	a := liveAnalyser()
	a.players[1] = &DemoPlayer{SteamID: 1}
	a.roundSwing[1] = -0.45

	a.derive(1)

	if got := a.players[1].PlayerMapStats.ApproxRoundSwingPercent; !closeTo(got, -45) {
		t.Errorf("extreme swing percent = %v, want -45", got)
	}
	if got := a.players[1].Rating.RoundSwing; got != 0 {
		t.Errorf("swing sub-rating = %v, want the 0 floor", got)
	}
}

func TestEcoRoundWeight(t *testing.T) {
	if w := ecoRoundWeight(tierRifle1); w != 1 {
		t.Errorf("tier-1 rifle round weighs %v, want exactly 1", w)
	}
	if w := ecoRoundWeight(tierUnknown); w != 1 {
		t.Errorf("unknown tier round weighs %v, want the neutral 1", w)
	}
	if w := ecoRoundWeight(tierStarterPistol); w <= 1 {
		t.Errorf("starter pistol round weighs %v, want more than a full buy", w)
	}
	if w := ecoRoundWeight(tierSniper); w >= 1 {
		t.Errorf("sniper round weighs %v, want less than a tier-1 rifle", w)
	}
}
