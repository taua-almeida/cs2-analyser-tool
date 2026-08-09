package demoparser

import (
	"time"

	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// tradeWindow is how long after a teammate's death a revenge kill still
// counts as a trade.
const tradeWindow = 5 * time.Second

// aceKills is the number of enemy kills in a single round that make an ace.
const aceKills = 5

var bothTeams = []common.Team{common.TeamTerrorists, common.TeamCounterTerrorists}

type deathRecord struct {
	killer     uint64
	victim     uint64
	victimTeam common.Team
	at         time.Duration
}

// roundOutcome is what a finished round contributes to player stats.
type roundOutcome struct {
	played       bool
	aces         []uint64
	clutcher     uint64 // winner-side player that won a 1vX, 0 if none
	participants map[uint64]common.Team
	kast         map[uint64]bool // players that got a Kill, Assist, Survived or were Traded
}

// roundTracker keeps the per-round state needed for clutches, aces, trades
// and KAST. It is fed plain values instead of parser types so tests can
// drive it without a demo file.
type roundTracker struct {
	live       bool
	ending     bool
	winner     common.Team
	startAlive map[uint64]common.Team
	alive      map[uint64]common.Team
	enemyKills map[uint64]int
	assists    map[uint64]bool
	traded     map[uint64]bool
	deaths     []deathRecord
	clutchers  map[common.Team]uint64
}

func newRoundTracker() *roundTracker {
	return &roundTracker{}
}

func (rt *roundTracker) startRound(alive map[uint64]common.Team) {
	rt.live = true
	rt.ending = false
	rt.winner = common.TeamUnassigned
	rt.startAlive = make(map[uint64]common.Team, len(alive))
	rt.alive = make(map[uint64]common.Team, len(alive))
	for id, team := range alive {
		rt.startAlive[id] = team
		rt.alive[id] = team
	}
	rt.enemyKills = make(map[uint64]int)
	rt.assists = make(map[uint64]bool)
	rt.traded = make(map[uint64]bool)
	rt.deaths = nil
	rt.clutchers = make(map[common.Team]uint64)
	rt.updateClutchCandidates()
}

// kill records a death in the current round. killer and assister are 0 for
// world deaths and suicides; byWorld marks deaths with no real cause (falls,
// match-end cleanup kills), as opposed to the bomb or an enemy. It reports
// whether the kill traded the death of one of the killer's teammates.
func (rt *roundTracker) kill(killer, victim uint64, killerTeam, victimTeam common.Team, assister uint64, byWorld bool, at time.Duration) bool {
	if !rt.live {
		return false
	}

	isTrade := false
	if killer != 0 && killerTeam != victimTeam {
		rt.enemyKills[killer]++
		if assister != 0 {
			rt.assists[assister] = true
		}
		for _, d := range rt.deaths {
			if d.killer == victim && d.victimTeam == killerTeam && at-d.at <= tradeWindow {
				rt.traded[d.victim] = true
				isTrade = true
			}
		}
	}

	rt.deaths = append(rt.deaths, deathRecord{killer: killer, victim: victim, victimTeam: victimTeam, at: at})
	// Dying before the round officially ends cancels survival, including to
	// the post-round bomb explosion (HLTV convention). Only match-end world
	// cleanup kills are ignored once the round is decided.
	if !(rt.ending && byWorld) {
		rt.remove(victim)
	}
	return isTrade
}

// disconnect handles a player leaving mid-round: they count as dead for
// the round, unless the round is already decided (players leaving after
// the final whistle keep their survival).
func (rt *roundTracker) disconnect(player uint64) {
	if rt.ending {
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
	if rt.ending {
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
	rt.ending = true
	rt.winner = winner
}

// finalize closes the round, normally at the official round end, and
// returns what it contributes to player stats.
func (rt *roundTracker) finalize() roundOutcome {
	if !rt.live {
		return roundOutcome{}
	}
	rt.live = false
	rt.ending = false

	outcome := roundOutcome{
		played:       true,
		clutcher:     rt.clutchers[rt.winner],
		participants: rt.startAlive,
		kast:         make(map[uint64]bool),
	}
	for id, kills := range rt.enemyKills {
		if kills >= aceKills {
			outcome.aces = append(outcome.aces, id)
		}
	}
	for id := range rt.startAlive {
		_, survived := rt.alive[id]
		if survived || rt.enemyKills[id] > 0 || rt.assists[id] || rt.traded[id] {
			outcome.kast[id] = true
		}
	}
	return outcome
}
