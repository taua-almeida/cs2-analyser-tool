package demoparser

import (
	"math"
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
	playing     []*common.Player
	warmup      bool
	currentTime time.Duration
}

func (p *matchParser) GameState() demoinfocs.GameState {
	return matchGameState{playing: p.playing, warmup: p.warmup}
}

func (p *matchParser) CurrentTime() time.Duration { return p.currentTime }

type matchGameState struct {
	demoinfocs.GameState
	playing []*common.Player
	warmup  bool
}

func (g matchGameState) IsWarmupPeriod() bool { return g.warmup }

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
		kastRounds:  make(map[uint64]SideCount),
		sideDamage:  make(map[uint64]SideCount),
		openingWins: make(map[uint64]int),
		lastHealth:  make(map[uint64]int),
		flashEnds:   make(map[uint64]time.Duration),
	}
}

const (
	shooterID = uint64(1)
	victimID  = uint64(2)
)

func player(id uint64, name string, team common.Team) *common.Player {
	return &common.Player{SteamID64: id, UserID: int(id), Name: name, Team: team}
}

// botPlayer builds a bot, whose SteamID64 is always 0 like every other bot's.
func botPlayer(userID int, name string, team common.Team) *common.Player {
	return &common.Player{IsBot: true, UserID: userID, Name: name, Team: team}
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

	if got := a.kastRounds[victimID].Total; got != 1 {
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

	if got := a.kastRounds[victimID].Total; got != 0 {
		t.Errorf("victim kast rounds = %d, want 0: they were killed before the official round end", got)
	}
	// The shooter keeps their survival, which also proves the round was
	// finalized at all rather than the whole outcome being dropped.
	if got := a.kastRounds[shooterID].Total; got != 1 {
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

// playRound runs a whole round through the handlers, with the kills that
// happened in it.
func playRound(a *analyser, winner common.Team, kills ...events.Kill) {
	a.onRoundStart(events.RoundStart{})
	for _, kill := range kills {
		a.onKill(kill)
	}
	a.onRoundEnd(events.RoundEnd{Winner: winner})
	a.onRoundEndOfficial(events.RoundEndOfficial{})
}

// hurtBy lands one hit for the given damage, starting the victim from full
// health. Stub players report no health of their own, so the round start
// leaves them on 0 and the double-counting cap would swallow the hit.
func hurtBy(a *analyser, attacker, victim *common.Player, damage int) {
	a.lastHealth[trackerID(victim)] = 100
	a.onPlayerHurt(events.PlayerHurt{
		Player:            victim,
		Attacker:          attacker,
		Weapon:            &common.Equipment{Type: common.EqAK47},
		HealthDamageTaken: damage,
		Health:            100 - damage,
	})
}

// closeTo compares rates, which come out of a division that rarely lands on
// an exact float.
func closeTo(got, want float64) bool {
	return math.Abs(got-want) < 1e-9
}

// Sides swap at halftime, so a kill has to land on the side the player was
// on when they got it, never on the one they finish the match on.
func TestSideStatsFollowTheHalftimeSwap(t *testing.T) {
	first := player(shooterID, "first", common.TeamTerrorists)
	second := player(victimID, "second", common.TeamCounterTerrorists)
	a := liveAnalyser(first, second)

	playRound(a, common.TeamTerrorists, events.Kill{
		Killer: first, Victim: second, Weapon: &common.Equipment{Type: common.EqAK47},
	})

	// Halftime. Both change ends, and the one who died takes revenge.
	first.Team, second.Team = common.TeamCounterTerrorists, common.TeamTerrorists
	playRound(a, common.TeamTerrorists, events.Kill{
		Killer: second, Victim: first, Weapon: &common.Equipment{Type: common.EqAK47},
	})

	// Both players did the same thing on the same sides, one half apart, so
	// they end on identical splits however they finished the match.
	for _, id := range []uint64{shooterID, victimID} {
		side := a.players[id].SideStats
		if want := (SideCount{Total: 2, CT: 1, T: 1}); side.Rounds != want {
			t.Errorf("player %d rounds = %+v, want %+v", id, side.Rounds, want)
		}
		if want := (SideCount{Total: 1, T: 1}); side.Kills != want {
			t.Errorf("player %d kills = %+v, want %+v: they fragged on the T side", id, side.Kills, want)
		}
		if want := (SideCount{Total: 1, CT: 1}); side.Deaths != want {
			t.Errorf("player %d deaths = %+v, want %+v: they died on the CT side", id, side.Deaths, want)
		}
	}
}

// Per-side ADR and KAST divide by the rounds the player spent on that side,
// the only denominator there is once sides swap. The match-wide pair keeps
// dividing by the rounds of the whole match, so the three differ.
func TestSideAdrAndKastDivideByRoundsOnThatSide(t *testing.T) {
	shooter := player(shooterID, "shooter", common.TeamTerrorists)
	victim := player(victimID, "victim", common.TeamCounterTerrorists)
	a := liveAnalyser(shooter, victim)

	// Three rounds as T: 30 damage in each, surviving the first two and
	// dying in the third with no kill, assist or trade to keep KAST.
	for round := range 3 {
		a.onRoundStart(events.RoundStart{})
		hurtBy(a, shooter, victim, 30)
		if round == 2 {
			a.onKill(events.Kill{Killer: victim, Victim: shooter, Weapon: &common.Equipment{Type: common.EqAK47}})
		}
		a.onRoundEnd(events.RoundEnd{Winner: common.TeamCounterTerrorists})
		a.onRoundEndOfficial(events.RoundEndOfficial{})
	}

	// Halftime, then a single round on CT with 60 damage and a survival.
	shooter.Team, victim.Team = common.TeamCounterTerrorists, common.TeamTerrorists
	a.onRoundStart(events.RoundStart{})
	hurtBy(a, shooter, victim, 60)
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamCounterTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	a.derive(4)

	p := a.players[shooterID]
	if want := (SideCount{Total: 4, CT: 1, T: 3}); p.SideStats.Rounds != want {
		t.Fatalf("rounds = %+v, want %+v", p.SideStats.Rounds, want)
	}
	// 90 damage over three T rounds, 60 over the one CT round.
	if got := p.SideStats.ADR; !closeTo(got.T, 30) || !closeTo(got.CT, 60) {
		t.Errorf("side ADR = %+v, want {CT: 60, T: 30}", got)
	}
	// KAST in two of the three T rounds, and in the only CT one.
	if got := p.SideStats.KAST; !closeTo(got.T, 100*2.0/3) || !closeTo(got.CT, 100) {
		t.Errorf("side KAST = %+v, want {CT: 100, T: 66.67}", got)
	}
	// The match-wide pair divides 150 damage and 3 KAST rounds by all four
	// rounds instead, landing between the two sides.
	if got := p.AssistStats.ADR; !closeTo(got, 37.5) {
		t.Errorf("match ADR = %v, want 37.5", got)
	}
	if got := p.PlayerMapStats.KAST; !closeTo(got, 75) {
		t.Errorf("match KAST = %v, want 75", got)
	}
}

// Freeze time runs after RoundStart has snapshotted the roster, and a
// player who picks a side inside that window still plays the whole round.
// Their damage and KAST are attributed to that side, so the side's round
// count has to move with them or the rates divide by nothing.
func TestFreezeTimeJoinerPlaysTheRound(t *testing.T) {
	holder := player(shooterID, "holder", common.TeamCounterTerrorists)
	joiner := player(victimID, "joiner", common.TeamSpectators)
	a := liveAnalyser(holder, joiner)

	a.onRoundStart(events.RoundStart{})
	joiner.Team = common.TeamTerrorists
	a.onRoundFreezetimeEnd(events.RoundFreezetimeEnd{})
	hurtBy(a, joiner, holder, 40)
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	a.derive(1)

	p := a.players[victimID]
	if want := (SideCount{Total: 1, T: 1}); p.SideStats.Rounds != want {
		t.Fatalf("joiner rounds = %+v, want %+v", p.SideStats.Rounds, want)
	}
	if got := p.SideStats.ADR.T; !closeTo(got, 40) {
		t.Errorf("joiner T-side ADR = %v, want 40: 40 damage over the one round they played", got)
	}
	if got := p.SideStats.KAST.T; !closeTo(got, 100) {
		t.Errorf("joiner T-side KAST = %v, want 100: they survived the round they joined", got)
	}
}

// Bots all report SteamID64 0, so a kill between two different bots must
// not be misread as one bot suiciding on itself (both sides comparing
// equal). Getting that wrong drops the bot kill as a killerless event,
// which would let a later human kill wrongly become the round's opener.
func TestBotOnBotKillDoesNotStealTheOpeningDuel(t *testing.T) {
	botT := botPlayer(101, "bot-t", common.TeamTerrorists)
	botCT := botPlayer(102, "bot-ct", common.TeamCounterTerrorists)
	shooter := player(shooterID, "shooter", common.TeamTerrorists)
	victim := player(victimID, "victim", common.TeamCounterTerrorists)
	a := liveAnalyser(botT, botCT, shooter, victim)

	a.onRoundStart(events.RoundStart{})
	a.onKill(events.Kill{Killer: botT, Victim: botCT, Weapon: &common.Equipment{Type: common.EqAK47}})
	a.onKill(events.Kill{Killer: shooter, Victim: victim, Weapon: &common.Equipment{Type: common.EqAK47}})
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	if got := a.players[shooterID].OpeningDuelStats.OpeningKills.Total; got != 0 {
		t.Errorf("shooter opening kills = %d, want 0: the bot-on-bot kill opened the round first", got)
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
