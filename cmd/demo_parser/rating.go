// This file approximates HLTV's Rating 3.0, whose exact formula is
// proprietary. Every constant here is a calibration choice, not a published
// value, except where a comment cites one of HLTV's own examples. The
// methodology and its limits are documented in _docs/PLAYER_DATA.MD.
package demoparser

import (
	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// equipTier buckets a player's loadout into the six categories the eco
// adjustment distinguishes. The zero value is tierUnknown, which every duel
// helper treats as an even matchup, so missing inventory data can never skew
// a rating in either direction.
type equipTier int

const (
	tierUnknown equipTier = iota
	tierStarterPistol
	tierUpgradedPistol
	tierSMG // SMGs, shotguns and machine guns
	tierRifle2
	tierRifle1
	tierSniper
)

// tierStrength expresses how strongly each tier is expected to perform in a
// duel: tier a beats tier b with probability a/(a+b). The spread is anchored
// on HLTV's published example of a tier-1 rifle kill on a starter pistol
// being worth ~0.54 points, which pins the rifle-vs-starter win rate near
// 76%; the tiers between are spaced by buy-menu cost and effective range.
var tierStrength = [...]float64{
	tierUnknown:        0, // never read: duels with an unknown tier are even
	tierStarterPistol:  1.0,
	tierUpgradedPistol: 1.45,
	tierSMG:            2.0,
	tierRifle2:         2.55,
	tierRifle1:         3.1,
	tierSniper:         3.4,
}

// weaponTier classifies one weapon. Anything that is not a firearm maps to
// tierUnknown so playerTier skips it.
func weaponTier(t common.EquipmentType) equipTier {
	switch t {
	case common.EqAWP, common.EqScar20, common.EqG3SG1:
		return tierSniper
	case common.EqAK47, common.EqM4A4, common.EqM4A1, common.EqAUG, common.EqSG556:
		return tierRifle1
	case common.EqFamas, common.EqGalil, common.EqSSG08:
		return tierRifle2
	case common.EqMac10, common.EqMP9, common.EqMP7, common.EqMP5, common.EqUMP,
		common.EqP90, common.EqBizon,
		common.EqNova, common.EqXM1014, common.EqSwag7, common.EqSawedOff,
		common.EqM249, common.EqNegev:
		return tierSMG
	case common.EqDeagle, common.EqRevolver, common.EqP250, common.EqFiveSeven,
		common.EqTec9, common.EqCZ, common.EqDualBerettas:
		return tierUpgradedPistol
	case common.EqGlock, common.EqUSP, common.EqP2000:
		return tierStarterPistol
	}
	return tierUnknown
}

// playerTier is the strongest weapon tier in an inventory, following HLTV's
// framing of a duel as loadout against loadout rather than the weapon the
// kill happened to land with. A non-empty inventory with no firearm (knife,
// zeus) takes the weakest tier: those players fight with even less than a
// starter pistol, and tierUnknown would misread their duels as even. Only a
// missing inventory is genuinely unknown.
func playerTier(inventory map[int]*common.Equipment) equipTier {
	if len(inventory) == 0 {
		return tierUnknown
	}
	best := tierStarterPistol
	for _, equipment := range inventory {
		if equipment == nil {
			continue
		}
		if tier := weaponTier(equipment.Type); tier > best {
			best = tier
		}
	}
	return best
}

// duelWinProb is the probability that a player carrying tier a kills a
// player carrying tier b when they meet.
func duelWinProb(a, b equipTier) float64 {
	if a == tierUnknown || b == tierUnknown {
		return 0.5
	}
	return tierStrength[a] / (tierStrength[a] + tierStrength[b])
}

// evenKillPoints is what a kill in an economically even duel is worth, the
// value HLTV quotes for Rating 3.0.
const evenKillPoints = 1.10

// ecoDuelFactor scales a kill or damage by how hard the duel was: 1 for an
// even matchup, less against under-equipped opponents, more for an upset.
// Doubling (1 - winProb) is what pins the even duel at exactly 1.
func ecoDuelFactor(killer, victim equipTier) float64 {
	return 2 * (1 - duelWinProb(killer, victim))
}

// killPoints is the eco-adjusted worth of a kill: 1.10 for an even duel,
// ~0.54 for a tier-1 rifle killing a starter pistol (HLTV's example), and
// ~1.66 for that pistol winning the upset.
func killPoints(killer, victim equipTier) float64 {
	return evenKillPoints * ecoDuelFactor(killer, victim)
}

// ecoRoundWeight makes survival and KAST credit count for more on rounds
// the player was under-equipped, when staying useful is harder and less
// expected, and slightly less on rounds they out-gunned the enemy. The
// weight is the chance a tier-1 rifle would beat this loadout, normalized
// so a tier-1 rifle buy weighs exactly 1.
func ecoRoundWeight(tier equipTier) float64 {
	return 2 * duelWinProb(tierRifle1, tier)
}
