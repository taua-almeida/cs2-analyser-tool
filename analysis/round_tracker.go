package analysis

import (
	"time"

	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// tradeWindow is how long after a teammate's death a revenge kill still
// counts as a trade.
const tradeWindow = 5 * time.Second

// aceKills is the number of enemy kills in a single round that make an ace.
const aceKills = 5

// multiKillMin is the fewest enemy kills in a round that make it a
// multi-kill round, the smallest bucket being the 2k.
const multiKillMin = 2

// ratingAssistDamage is the damage a player must have dealt to a dying
// enemy for the kill to count as an assist in the rating's KAST. Rating 3.0
// raised it from the 25 that Rating 2.0 used.
const ratingAssistDamage = 40

var bothTeams = []common.Team{common.TeamTerrorists, common.TeamCounterTerrorists}

type deathRecord struct {
	killer     uint64
	victim     uint64
	victimTeam common.Team
	tick       int
}

type assistFacts uint8

const (
	classicAssist assistFacts = 1 << iota
	ratingAssist
)

// openingDuel is the first enemy kill of a round. A zero killer means the
// round had none.
type openingDuel struct {
	killer, victim         uint64
	killerTeam, victimTeam common.Team
}

// roundOutcome is what a finished round contributes to player stats.
type roundOutcome struct {
	played       bool
	decided      bool
	winner       common.Team // side that won, TeamUnassigned when undecided
	mvp          uint64
	aces         []uint64
	multiKills   map[uint64]int // enemy kills of the players that got at least a 2k
	tradeKills   map[uint64]int // revenge kills that traded one eligible teammate death
	clutcher     uint64         // winner-side player that won a 1vX, 0 if none
	opening      openingDuel
	openingWon   bool // the opening killer's team won the round
	participants map[uint64]common.Team
	liveSides    map[uint64]common.Team // roster when live play began; see the tracker field
	deathsTraded map[uint64]bool        // players with a death avenged inside the trade window
	kast         map[uint64]bool        // players that got a Kill, Assist, Survived or were Traded
	swing        map[uint64]float64     // per-player round-win-probability swing, zero-sum per round
	survived     map[uint64]bool        // players still alive when the round closed
	ratingKast   map[uint64]bool        // players with rating KAST credit; see finalize
}

// roundTracker keeps the per-round state needed for clutches, aces, trades
// and KAST. It is fed plain values instead of parser types so tests can
// drive it without a demo file.
type roundTracker struct {
	live        bool
	decided     bool
	winner      common.Team
	mvp         uint64
	bombPlanted bool
	startAlive  map[uint64]common.Team
	alive       map[uint64]common.Team
	// liveSides is the roster as of the moment live play begins. Unlike
	// startAlive it is replaced outright by the freeze-time refresh, so a
	// player who left during freeze time is absent instead of lingering on
	// a stale side — which is what logical-team resolution needs when a
	// halftime side switch lands inside freeze time.
	liveSides     map[uint64]common.Team
	enemyKills    map[uint64]int
	assists       map[uint64]assistFacts
	traded        map[uint64]bool
	tradeKills    map[uint64]int
	deaths        []deathRecord
	clutchers     map[common.Team]uint64
	opening       openingDuel
	damageTo      map[uint64]map[uint64]int // enemy damage this round, by victim then attacker
	damageAssists map[uint64]bool           // players with ratingAssistDamage on an enemy that died
	swing         map[uint64]float64        // round-win-probability swing per player
}

func newRoundTracker() *roundTracker {
	return &roundTracker{}
}

func (rt *roundTracker) startRound(alive map[uint64]common.Team) {
	rt.live = true
	rt.decided = false
	rt.winner = common.TeamUnassigned
	rt.mvp = 0
	rt.startAlive = make(map[uint64]common.Team, len(alive))
	rt.alive = make(map[uint64]common.Team, len(alive))
	rt.liveSides = make(map[uint64]common.Team, len(alive))
	for id, team := range alive {
		rt.startAlive[id] = team
		rt.alive[id] = team
		rt.liveSides[id] = team
	}
	rt.enemyKills = make(map[uint64]int)
	rt.assists = make(map[uint64]assistFacts)
	rt.traded = make(map[uint64]bool)
	rt.tradeKills = make(map[uint64]int)
	rt.deaths = nil
	rt.clutchers = make(map[common.Team]uint64)
	rt.opening = openingDuel{}
	rt.bombPlanted = false
	rt.damageTo = make(map[uint64]map[uint64]int)
	rt.damageAssists = make(map[uint64]bool)
	rt.swing = make(map[uint64]float64)
	rt.updateClutchCandidates()
}

func (rt *roundTracker) markMVP(player uint64) {
	if rt.live {
		rt.mvp = player
	}
}

// plantBomb records the bomb going down, which shifts the win probability
// behind round swing towards the T side for the rest of the round.
func (rt *roundTracker) plantBomb() {
	if !rt.live {
		return
	}
	rt.bombPlanted = true
}

// damage accumulates enemy damage for the rating's 40-damage assist rule.
// The caller is responsible for filtering out team, self and bomb damage,
// which the analyser's hurt handler already does for damage given.
func (rt *roundTracker) damage(attacker, victim uint64, hp int) {
	if !rt.live || attacker == 0 {
		return
	}
	victimDamage, ok := rt.damageTo[victim]
	if !ok {
		victimDamage = make(map[uint64]int)
		rt.damageTo[victim] = victimDamage
	}
	victimDamage[attacker] += hp
}

// joinRound folds the players on a side when live play begins into the
// round. Picking a side during freeze time happens after startRound took
// its snapshot, and those players play the round like anyone else. Someone
// who left in the meantime is not brought back to life.
func (rt *roundTracker) joinRound(alive map[uint64]common.Team) {
	if !rt.live {
		return
	}
	rt.liveSides = make(map[uint64]common.Team, len(alive))
	for id, team := range alive {
		rt.liveSides[id] = team
		_, started := rt.startAlive[id]
		_, stillAlive := rt.alive[id]
		// A player who swapped sides during freeze time plays the round on
		// the side they start it on, the one their kills count towards.
		rt.startAlive[id] = team
		if !started || stillAlive {
			rt.alive[id] = team
		}
	}
	rt.updateClutchCandidates()
}

// kill records a death in the current round. killer and assister are 0 for
// world deaths and suicides; byWorld marks deaths with no real cause (falls,
// match-end cleanup kills), as opposed to the bomb or an enemy. Tick is the
// server tick that carried the event, and tickTime is that tick's duration.
func (rt *roundTracker) kill(killer, victim uint64, killerTeam, victimTeam common.Team, assister uint64, assistedFlash, byWorld bool, tick int, tickTime time.Duration) {
	if !rt.live {
		return
	}

	if killer != 0 && killerTeam != victimTeam {
		// The first enemy kill is the round's opening duel. Teamkills,
		// suicides and world deaths never open a round, and once the round
		// is decided nothing can (a bomb explosion or exit frag in an
		// otherwise killless round is not an entry).
		if rt.opening.killer == 0 && !rt.decided {
			rt.opening = openingDuel{killer: killer, victim: victim, killerTeam: killerTeam, victimTeam: victimTeam}
		}
		rt.enemyKills[killer]++
		if assister != 0 {
			rt.assists[assister] |= ratingAssist
			if !assistedFlash {
				rt.assists[assister] |= classicAssist
			}
		}
		// Post-round kills swing nothing: the probability table has no
		// notion of a decided round, so without this guard an exit frag
		// would read as swinging a settled outcome.
		if !rt.decided {
			rt.recordSwing(killer, victim, killerTeam, victimTeam)
		}
		// Deliberately no rt.decided guard: deaths and revenge kills stay
		// eligible until finalize, so a post-round death can still be
		// avenged inside the window. One revenge kill credits the oldest
		// eligible death and then stops.
		for _, d := range rt.deaths {
			if d.killer == victim && d.victimTeam == killerTeam &&
				insideTradeWindow(d.tick, tick, tickTime, tradeWindow) {
				rt.traded[d.victim] = true
				rt.tradeKills[killer]++
				break
			}
		}
	}

	rt.deaths = append(rt.deaths, deathRecord{killer: killer, victim: victim, victimTeam: victimTeam, tick: tick})
	// Dying before the round officially ends cancels survival, including to
	// the post-round bomb explosion. Raw death totals are handled separately
	// by the analyser. Only match-end world cleanup kills are ignored once
	// the round is decided — and because
	// their victim keeps survival, they settle no 40-damage assists either:
	// damage into a survivor is no assist. Every real death settles them,
	// whatever caused it; enemy damage into a player finished by the bomb or
	// a fall still helped. The killer's own damage is their kill, not an
	// assist.
	if !rt.isPostRoundWorldCleanup(byWorld) {
		for attacker, hp := range rt.damageTo[victim] {
			if attacker != killer && hp >= ratingAssistDamage {
				rt.damageAssists[attacker] = true
			}
		}
		rt.remove(victim)
	}
}

// insideTradeWindow compares the closest possible instants in two event
// ticks. An event timestamp identifies an interval, not a point: tick n can
// occur at its end and tick n+1 at its beginning. Relative ticks avoid the
// phase-dependent rounding in demoinfocs' float32 absolute demo clock.
func insideTradeWindow(deathTick, revengeTick int, tickTime, window time.Duration) bool {
	if tickTime <= 0 || window < 0 || revengeTick < deathTick {
		return false
	}
	minimumElapsedTicks := max(0, revengeTick-deathTick-1)
	return time.Duration(minimumElapsedTicks)*tickTime <= window
}

// recordSwing credits the killer with the round-win-probability gain of the
// victim's death and debits the victim the same amount, so swing sums to
// zero across each round: what one player earns, an opponent paid for.
// Suicides, teamkills and world deaths reach this through no path, which
// means they move no swing in this first version.
func (rt *roundTracker) recordSwing(killer, victim uint64, killerTeam, victimTeam common.Team) {
	if _, isAlive := rt.alive[victim]; !isAlive {
		return
	}
	tAlive, ctAlive := rt.aliveCounts()
	before := teamWinProbability(killerTeam, tAlive, ctAlive, rt.bombPlanted)
	if victimTeam == common.TeamTerrorists {
		tAlive--
	} else {
		ctAlive--
	}
	after := teamWinProbability(killerTeam, tAlive, ctAlive, rt.bombPlanted)
	delta := after - before
	rt.swing[killer] += delta
	rt.swing[victim] -= delta
}

func (rt *roundTracker) aliveCounts() (tAlive, ctAlive int) {
	for _, team := range rt.alive {
		switch team {
		case common.TeamTerrorists:
			tAlive++
		case common.TeamCounterTerrorists:
			ctAlive++
		}
	}
	return tAlive, ctAlive
}

// isPostRound reports whether RoundEnd has decided the current or most
// recently finalized round. The decision remains available after finalize
// because CS2 can dispatch death events after RoundEndOfficial; startRound
// clears it.
func (rt *roundTracker) isPostRound() bool {
	return rt.decided
}

// isPostRoundWorldCleanup identifies World events after the round result is
// known. The analyser separately checks the game phase before excluding one
// from raw death totals.
func (rt *roundTracker) isPostRoundWorldCleanup(byWorld bool) bool {
	return rt.isPostRound() && byWorld
}

// disconnect handles a player leaving mid-round: they count as dead for
// the round, unless the round is already decided (players leaving after
// the final whistle keep their survival).
func (rt *roundTracker) disconnect(player uint64) {
	if rt.decided {
		return
	}
	rt.remove(player)
}

// remove takes a player out of the alive set.
func (rt *roundTracker) remove(player uint64) {
	if !rt.live {
		return
	}
	delete(rt.alive, player)
	rt.updateClutchCandidates()
}

// updateClutchCandidates marks the last alive player of a team as that
// team's clutch candidate the moment they are left alone against at least
// one enemy. The candidacy sticks for the rest of the round. Once the round
// is decided, post-round deaths cannot start a clutch anymore.
func (rt *roundTracker) updateClutchCandidates() {
	if rt.decided {
		return
	}
	for _, team := range bothTeams {
		if _, started := rt.clutchers[team]; started {
			continue
		}
		if last, enemies := rt.lastAlive(team); last != 0 && enemies >= 1 {
			rt.clutchers[team] = last
		}
	}
}

// lastAlive returns the only alive member of team (0 when the team has zero
// or more than one player alive) and the number of alive enemies.
func (rt *roundTracker) lastAlive(team common.Team) (uint64, int) {
	var last uint64
	count, enemies := 0, 0
	for id, t := range rt.alive {
		if t == team {
			count++
			last = id
		} else {
			enemies++
		}
	}
	if count != 1 {
		return 0, enemies
	}
	return last, enemies
}

// markEnd records the winner when the round is decided. The round stays
// open until finalize so that deaths in the post-round seconds (bomb
// explosions, exit frags) still cancel survival credit.
func (rt *roundTracker) markEnd(winner common.Team) {
	if !rt.live {
		return
	}
	rt.decided = true
	rt.winner = winner
}

// finalize closes the round, normally at the official round end, and
// returns what it contributes to player stats.
func (rt *roundTracker) finalize() roundOutcome {
	if !rt.live {
		return roundOutcome{}
	}
	rt.live = false

	outcome := roundOutcome{
		played:       true,
		decided:      rt.decided,
		winner:       rt.winner,
		mvp:          rt.mvp,
		multiKills:   make(map[uint64]int),
		tradeKills:   rt.tradeKills,
		clutcher:     rt.clutchers[rt.winner],
		opening:      rt.opening,
		openingWon:   rt.opening.killer != 0 && rt.opening.killerTeam == rt.winner,
		participants: rt.startAlive,
		liveSides:    rt.liveSides,
		deathsTraded: make(map[uint64]bool),
		kast:         make(map[uint64]bool),
		swing:        rt.swing,
		survived:     make(map[uint64]bool),
		ratingKast:   make(map[uint64]bool),
	}
	for id, kills := range rt.enemyKills {
		if kills >= aceKills {
			outcome.aces = append(outcome.aces, id)
		}
		if kills >= multiKillMin {
			outcome.multiKills[id] = kills
		}
	}
	for id := range rt.startAlive {
		_, survived := rt.alive[id]
		if rt.traded[id] {
			outcome.deathsTraded[id] = true
		}
		if survived || rt.enemyKills[id] > 0 || rt.assists[id]&classicAssist != 0 || rt.traded[id] {
			outcome.kast[id] = true
		}
		if survived {
			outcome.survived[id] = true
		}
		// Rating KAST keeps every demo assist, including flash assists, and
		// adds contributors that meet its separate 40-damage rule.
		if outcome.kast[id] || rt.assists[id]&ratingAssist != 0 || rt.damageAssists[id] {
			outcome.ratingKast[id] = true
		}
	}
	return outcome
}
