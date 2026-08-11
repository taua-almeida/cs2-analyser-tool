// This file approximates HLTV's Rating 3.0, whose exact formula is
// proprietary. Every constant here is a calibration choice, not a published
// value, except where a comment cites one of HLTV's own examples. The
// methodology and its limits are documented in _docs/PLAYER_DATA.MD.
package demoparser

import (
	"math"

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

// tWinProbBase[t][ct] is the probability that the T side wins a round with
// t terrorists and ct counter-terrorists alive and the bomb not planted.
// The anchors are hand-set from community round-outcome statistics: even
// counts sit at 0.48 (maps average slightly CT-sided pre-plant), a one-player
// advantage is worth roughly 15-20 points, and lopsided counts saturate
// towards, but never at, certainty. An empty side is certain: with no CTs
// the Ts have won the round on elimination, and with no Ts and no bomb the
// round cannot be won anymore. The [0][0] entry is unreachable, since a
// round is over before both sides are empty.
var tWinProbBase = [6][6]float64{
	{0.50, 0.00, 0.00, 0.00, 0.00, 0.00},
	{1.00, 0.45, 0.20, 0.08, 0.03, 0.01},
	{1.00, 0.75, 0.48, 0.27, 0.13, 0.05},
	{1.00, 0.90, 0.70, 0.48, 0.30, 0.16},
	{1.00, 0.95, 0.85, 0.67, 0.48, 0.32},
	{1.00, 0.97, 0.92, 0.80, 0.65, 0.48},
}

// bombLogitShift is how far a planted bomb moves the round in the T side's
// favour, in log-odds. 0.9 lifts an even 5v5 from 0.48 pre-plant to 0.69
// post-plant. Applying the shift in log-odds space rather than adding to
// the probability keeps lopsided rounds lopsided: a 0.97 round moves to
// 0.99, not past certainty.
const bombLogitShift = 0.9

// tWinProbBombNoTs[ct] is the T-side win probability after the bomb is
// planted and every T is dead. The CTs still have to find and defuse it,
// which is harder with fewer of them left. Each value sits below the
// post-plant probability of the matching 1-T-alive round, so losing the
// last T can never read as an improvement.
var tWinProbBombNoTs = [6]float64{0, 0.15, 0.08, 0.05, 0.03, 0.02}

// tWinProbability estimates the chance the T side wins the round from the
// alive counts and the bomb state. This is the round-swing model: each kill
// is worth the difference it makes here. The table stops at 5v5, so larger
// modes clamp to it and kills beyond the fifth player move nothing.
func tWinProbability(tAlive, ctAlive int, bombPlanted bool) float64 {
	t := min(max(tAlive, 0), 5)
	ct := min(max(ctAlive, 0), 5)
	if bombPlanted && t == 0 && ct > 0 {
		return tWinProbBombNoTs[ct]
	}
	p := tWinProbBase[t][ct]
	if bombPlanted {
		p = shiftLogit(p, bombLogitShift)
	}
	return p
}

// teamWinProbability is tWinProbability seen from one side's perspective.
func teamWinProbability(team common.Team, tAlive, ctAlive int, bombPlanted bool) float64 {
	p := tWinProbability(tAlive, ctAlive, bombPlanted)
	if team == common.TeamCounterTerrorists {
		return 1 - p
	}
	return p
}

// shiftLogit moves a probability by delta in log-odds space. The certain
// outcomes 0 and 1 have no log-odds and stay where they are.
func shiftLogit(p, delta float64) float64 {
	if p <= 0 || p >= 1 {
		return p
	}
	odds := p / (1 - p) * math.Exp(delta)
	return odds / (1 + odds)
}

// The baselines are what an average player produces per round on each axis.
// Dividing by them normalizes every sub-rating to 1.0 for that average
// player. The values were measured over the calibration set in
// _docs/PLAYER_DATA.MD; update them together with those numbers.
const (
	baselineKillPoints = 0.68 // eco-adjusted kill points per round
	baselineEcoDamage  = 78.0 // eco-adjusted damage per round
	baselineSurvival   = 0.30 // eco-weighted rounds survived per round
	baselineKast       = 0.70 // eco-weighted rating-KAST rounds per round
	baselineMultiKill  = 0.27 // multi-kill points per round
)

// swingScale converts average round swing, which is zero for the average
// player by construction, into a sub-rating around 1.0. With 2.5, a player
// who single-handedly moves 4% of win probability per round rates 1.10 on
// this axis, roughly the spread the other sub-ratings show between an
// average and a strong player.
const swingScale = 2.5

// Blend weights of the six sub-ratings, summing to 1. HLTV does not publish
// theirs; these follow the emphasis of their Rating 3.0 write-up, with
// kills, damage and round swing carrying most of the rating and survival
// and multi-kills acting as smaller correctives.
const (
	weightKills     = 0.25
	weightDamage    = 0.20
	weightSurvival  = 0.10
	weightKast      = 0.15
	weightMultiKill = 0.10
	weightSwing     = 0.20
)

// multiKillPoints rewards the rounds a player killed several enemies in,
// escalating faster than linearly because each further kill in one round is
// rarer and harder than the last.
func multiKillPoints(m MultiKillRounds) float64 {
	return float64(m.K2)*1 + float64(m.K3)*2 + float64(m.K4)*4 + float64(m.K5)*7
}

// ratingRound holds a player's per-round averages on the six axes, ready to
// be measured against the baselines.
type ratingRound struct {
	killPoints float64
	ecoDamage  float64
	survival   float64
	kast       float64
	multiKill  float64
	swing      float64
}

// blendRating turns per-round averages into the six sub-ratings and their
// weighted blend. Swing is the one axis that can go negative, so it is
// floored at 0 like every other sub-rating's natural floor.
func blendRating(r ratingRound) RatingStats {
	s := RatingStats{
		Kills:      r.killPoints / baselineKillPoints,
		Damage:     r.ecoDamage / baselineEcoDamage,
		Survival:   r.survival / baselineSurvival,
		KAST:       r.kast / baselineKast,
		MultiKill:  r.multiKill / baselineMultiKill,
		RoundSwing: max(0, 1+r.swing*swingScale),
	}
	s.Value = weightKills*s.Kills +
		weightDamage*s.Damage +
		weightSurvival*s.Survival +
		weightKast*s.KAST +
		weightMultiKill*s.MultiKill +
		weightSwing*s.RoundSwing
	return s
}
