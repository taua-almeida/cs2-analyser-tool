package demoparser

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
			if math.Abs(pa+pb-1) > 1e-9 {
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
	if got := killPoints(tierRifle1, tierRifle1); math.Abs(got-1.10) > 1e-9 {
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
