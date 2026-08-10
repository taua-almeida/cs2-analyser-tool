package demoparser

import (
	"testing"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	events "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// onPlayerHurt only asks the parser whether the demo is in warmup, so these
// stubs answer that one question and leave the embedded interfaces nil.
type liveParser struct{ demoinfocs.Parser }

func (liveParser) GameState() demoinfocs.GameState { return liveGameState{} }

type liveGameState struct{ demoinfocs.GameState }

func (liveGameState) IsWarmupPeriod() bool { return false }

func liveAnalyser() *analyser {
	return &analyser{
		parser:     liveParser{},
		players:    make(map[uint64]*DemoPlayer),
		tracker:    newRoundTracker(),
		kastRounds: make(map[uint64]int),
		lastHealth: make(map[uint64]int),
	}
}

const (
	shooterID = uint64(1)
	victimID  = uint64(2)
)

// hurt builds a PlayerHurt from an enemy shooter, where healthAfter is the
// victim's health once the event has been applied.
func hurt(weapon common.EquipmentType, damageTaken, healthAfter int) events.PlayerHurt {
	return events.PlayerHurt{
		Player:            &common.Player{SteamID64: victimID, Name: "victim", Team: common.TeamCounterTerrorists},
		Attacker:          &common.Player{SteamID64: shooterID, Name: "shooter", Team: common.TeamTerrorists},
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
