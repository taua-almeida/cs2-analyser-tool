package demoparser

import (
	"testing"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	events "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// The handlers under test only ask the parser whether the demo is in
// warmup, who is playing, and what time it is, so these stubs answer those
// three questions and leave the embedded interfaces nil.
type matchParser struct {
	demoinfocs.Parser
	playing []*common.Player
}

func (p *matchParser) GameState() demoinfocs.GameState { return matchGameState{playing: p.playing} }

func (*matchParser) CurrentTime() time.Duration { return 0 }

type matchGameState struct {
	demoinfocs.GameState
	playing []*common.Player
}

func (matchGameState) IsWarmupPeriod() bool { return false }

func (g matchGameState) Participants() demoinfocs.Participants {
	return matchParticipants{playing: g.playing}
}

type matchParticipants struct {
	demoinfocs.Participants
	playing []*common.Player
}

func (p matchParticipants) Playing() []*common.Player { return p.playing }

func liveAnalyser(playing ...*common.Player) *analyser {
	return &analyser{
		parser:      &matchParser{playing: playing},
		players:     make(map[uint64]*DemoPlayer),
		tracker:     newRoundTracker(),
		kastRounds:  make(map[uint64]int),
		openingWins: make(map[uint64]int),
		lastHealth:  make(map[uint64]int),
	}
}

const (
	shooterID = uint64(1)
	victimID  = uint64(2)
)

func player(id uint64, name string, team common.Team) *common.Player {
	return &common.Player{SteamID64: id, UserID: int(id), Name: name, Team: team}
}

// hurt builds a PlayerHurt from an enemy shooter, where healthAfter is the
// victim's health once the event has been applied.
func hurt(weapon common.EquipmentType, damageTaken, healthAfter int) events.PlayerHurt {
	return events.PlayerHurt{
		Player:            player(victimID, "victim", common.TeamCounterTerrorists),
		Attacker:          player(shooterID, "shooter", common.TeamTerrorists),
		Weapon:            &common.Equipment{Type: weapon},
		HealthDamageTaken: damageTaken,
		Health:            healthAfter,
	}
}

func damageGiven(a *analyser) int {
	p := a.players[shooterID]
	if p == nil {
		return 0
	}
	return p.AssistStats.DamageGiven
}

// One shotgun blast lands several pellets on the same tick, and each pellet
// reports the damage the victim could have taken from the health they had
// when the tick started. Summing the raw numbers therefore counts the same
// health twice. Only the victim's real health drop may be credited.
func TestShotgunPelletsDoNotDoubleCountDamage(t *testing.T) {
	a := liveAnalyser()

	a.onPlayerHurt(hurt(common.EqNova, 40, 80))
	a.onPlayerHurt(hurt(common.EqNova, 40, 60))

	if got := damageGiven(a); got != 40 {
		t.Errorf("damage given = %d, want 40 (the victim went from 100 to 60 health)", got)
	}
}

// The cap must not shave damage off an ordinary single hit.
func TestSingleHitDamageIsCreditedInFull(t *testing.T) {
	a := liveAnalyser()

	a.onPlayerHurt(hurt(common.EqAK47, 27, 73))

	if got := damageGiven(a); got != 27 {
		t.Errorf("damage given = %d, want 27", got)
	}
}

// The victim can lose more health between two events than the shooter's
// own weapon reported, when something else hurt them at the same time. The
// shooter is only ever credited with what their weapon reported.
func TestDamageNeverExceedsWhatTheWeaponReported(t *testing.T) {
	a := liveAnalyser()

	a.onPlayerHurt(hurt(common.EqAK47, 10, 90))
	a.onPlayerHurt(hurt(common.EqAK47, 10, 50))

	if got := damageGiven(a); got != 20 {
		t.Errorf("damage given = %d, want 20 (the shooter reported two hits of 10)", got)
	}
}

// Health going up between two events (a fresh round the handler has not
// seen start yet) must never produce negative damage.
func TestHealingBetweenEventsGivesNoDamage(t *testing.T) {
	a := liveAnalyser()

	a.onPlayerHurt(hurt(common.EqAK47, 30, 70))
	a.onPlayerHurt(hurt(common.EqAK47, 30, 100))

	if got := damageGiven(a); got != 30 {
		t.Errorf("damage given = %d, want 30", got)
	}
}

// A round only counts as survived once it is officially over, so the
// handlers have to finalize on RoundEndOfficial rather than RoundEnd. The
// two tests below pin that wiring down: finalizing early would hand the
// victim a survival they did not earn.
func liveRound() (*analyser, *common.Player, *common.Player) {
	shooter := player(shooterID, "shooter", common.TeamTerrorists)
	victim := player(victimID, "victim", common.TeamCounterTerrorists)
	a := liveAnalyser(shooter, victim)
	a.onRoundStart(events.RoundStart{})
	return a, shooter, victim
}

func TestSurvivorsGetKastWhenTheRoundEndsOfficially(t *testing.T) {
	a, _, _ := liveRound()

	a.onRoundEnd(events.RoundEnd{Winner: common.TeamCounterTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	if got := a.kastRounds[victimID]; got != 1 {
		t.Errorf("victim kast rounds = %d, want 1: nobody died all round", got)
	}
}

func TestPostRoundDeathBeforeOfficialEndCancelsKast(t *testing.T) {
	a, shooter, victim := liveRound()

	// The exit frag lands after the round is decided but before it is
	// officially over, so the victim did not survive it.
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamCounterTerrorists})
	a.onKill(events.Kill{Killer: shooter, Victim: victim, Weapon: &common.Equipment{Type: common.EqAK47}})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	if got := a.kastRounds[victimID]; got != 0 {
		t.Errorf("victim kast rounds = %d, want 0: they were killed before the official round end", got)
	}
	// The shooter keeps their survival, which also proves the round was
	// finalized at all rather than the whole outcome being dropped.
	if got := a.kastRounds[shooterID]; got != 1 {
		t.Errorf("shooter kast rounds = %d, want 1", got)
	}
}

// The opening duel of the round has to land on both players' stats: an
// opening kill on the killer's side, an opening death on the victim's side,
// and a win counted towards the killer's opening success rate.
func TestOpeningDuelIsCredited(t *testing.T) {
	a, shooter, victim := liveRound()

	a.onKill(events.Kill{Killer: shooter, Victim: victim, Weapon: &common.Equipment{Type: common.EqAK47}})
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	kills := a.players[shooterID].OpeningDuelStats.OpeningKills
	if kills.Total != 1 || kills.T != 1 || kills.CT != 0 {
		t.Errorf("shooter opening kills = %+v, want one on the T side", kills)
	}
	deaths := a.players[victimID].OpeningDuelStats.OpeningDeaths
	if deaths.Total != 1 || deaths.CT != 1 || deaths.T != 0 {
		t.Errorf("victim opening deaths = %+v, want one on the CT side", deaths)
	}
	if got := a.openingWins[shooterID]; got != 1 {
		t.Errorf("shooter opening wins = %d, want 1: their team won the round", got)
	}
}

// The clutch goes to the last player alive on the winning side, so the
// winner carried by the RoundEnd event has to reach the tracker. A lone
// terrorist holding a 1v2 and winning is only credited if it does.
func TestClutchGoesToTheTeamThatWonTheRound(t *testing.T) {
	lone := player(shooterID, "lone", common.TeamTerrorists)
	first := player(3, "ct1", common.TeamCounterTerrorists)
	second := player(4, "ct2", common.TeamCounterTerrorists)
	a := liveAnalyser(lone, first, second)

	a.onRoundStart(events.RoundStart{})
	a.onKill(events.Kill{Killer: lone, Victim: first, Weapon: &common.Equipment{Type: common.EqAK47}})
	a.onKill(events.Kill{Killer: lone, Victim: second, Weapon: &common.Equipment{Type: common.EqAK47}})
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	if got := a.players[shooterID].PlayerMapStats.ClutchesWon; got != 1 {
		t.Errorf("clutches won = %d, want 1", got)
	}
}
