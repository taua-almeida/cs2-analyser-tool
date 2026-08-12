package demoparser

import (
	"math"
	"testing"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	events "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	st "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/sendtables"
)

// The handlers under test only ask the parser for round state, who is
// playing, and the current time, so these stubs answer those questions and
// leave the embedded interfaces nil.
type matchParser struct {
	demoinfocs.Parser
	playing     []*common.Player
	warmup      bool
	gamePhase   common.GamePhase
	currentTime time.Duration
}

func (p *matchParser) GameState() demoinfocs.GameState {
	return matchGameState{playing: p.playing, warmup: p.warmup, gamePhase: p.gamePhase}
}

func (p *matchParser) CurrentTime() time.Duration { return p.currentTime }

type matchGameState struct {
	demoinfocs.GameState
	playing   []*common.Player
	warmup    bool
	gamePhase common.GamePhase
}

func (g matchGameState) IsWarmupPeriod() bool { return g.warmup }

func (g matchGameState) GamePhase() common.GamePhase { return g.gamePhase }

func (g matchGameState) Participants() demoinfocs.Participants {
	return matchParticipants{playing: g.playing}
}

type matchParticipants struct {
	demoinfocs.Participants
	playing []*common.Player
}

func (p matchParticipants) Playing() []*common.Player { return p.playing }

func liveAnalyser(playing ...*common.Player) *analyser {
	return newAnalyser(&matchParser{playing: playing})
}

const (
	shooterID = uint64(1)
	victimID  = uint64(2)
)

func player(id uint64, name string, team common.Team) *common.Player {
	return &common.Player{SteamID64: id, UserID: int(id), Name: name, Team: team}
}

type matchEntity struct {
	st.Entity
	properties map[string]st.PropertyValue
}

func (e matchEntity) PropertyValue(name string) (st.PropertyValue, bool) {
	value, ok := e.properties[name]
	return value, ok
}

func (e matchEntity) PropertyValueMust(name string) st.PropertyValue {
	if value, ok := e.properties[name]; ok {
		return value
	}
	panic("missing test entity property: " + name)
}

func playerWithCoachingTeam(id uint64, name string, team, coachingTeam common.Team) *common.Player {
	p := player(id, name, team)
	p.Entity = matchEntity{properties: map[string]st.PropertyValue{
		"m_iCoachingTeam": {Any: int32(coachingTeam)},
		"m_iMVPs":         {Any: int32(0)},
	}}
	return p
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

func TestPlayingRosterExcludesCoachingController(t *testing.T) {
	for _, test := range []struct {
		name string
		team common.Team
	}{
		{name: "terrorist", team: common.TeamTerrorists},
		{name: "counter-terrorist", team: common.TeamCounterTerrorists},
	} {
		t.Run(test.name, func(t *testing.T) {
			const coachID = uint64(3)
			competitor := player(shooterID, "competitor", common.TeamTerrorists)
			coach := playerWithCoachingTeam(coachID, "coach", test.team, test.team)
			a := liveAnalyser(competitor, coach)

			roster := a.playingRoster()

			if _, ok := roster[coachID]; ok {
				t.Error("coaching controller was included in the round roster")
			}
			if _, ok := roster[shooterID]; !ok {
				t.Error("competitor was excluded from the round roster")
			}
			if _, ok := a.players[coachID]; ok {
				t.Error("coaching controller was exported as a player")
			}

			a.syncScoreboardMVPs()
			if _, ok := a.players[coachID]; ok {
				t.Error("scoreboard MVP sync exported the coaching controller")
			}
		})
	}
}

func TestUnsetCoachingTeamKeepsPlayerEligible(t *testing.T) {
	competitor := player(shooterID, "competitor", common.TeamTerrorists)
	competitor.Entity = matchEntity{properties: map[string]st.PropertyValue{
		"m_iCoachingTeam": {},
	}}
	a := liveAnalyser(competitor)

	if a.ensurePlayer(competitor) == nil {
		t.Error("player with an unset coaching team was excluded")
	}
}

func TestCoachingControllerEventsDoNotAffectCompetitiveRound(t *testing.T) {
	competitor := player(shooterID, "competitor", common.TeamTerrorists)
	opponent := player(victimID, "opponent", common.TeamCounterTerrorists)
	tCoach := playerWithCoachingTeam(3, "t-coach", common.TeamTerrorists, common.TeamTerrorists)
	ctCoach := playerWithCoachingTeam(4, "ct-coach", common.TeamCounterTerrorists, common.TeamCounterTerrorists)
	a := liveAnalyser(competitor, opponent, tCoach, ctCoach)
	a.onRoundStart(events.RoundStart{})
	a.lastHealth[victimID] = 100

	a.onPlayerHurt(events.PlayerHurt{
		Player: ctCoach, Attacker: competitor, Weapon: &common.Equipment{Type: common.EqAK47},
		HealthDamageTaken: 40, Health: 60,
	})
	a.onPlayerHurt(events.PlayerHurt{
		Player: opponent, Attacker: tCoach, Weapon: &common.Equipment{Type: common.EqAK47},
		HealthDamageTaken: 40, Health: 60,
	})
	a.onKill(events.Kill{
		Killer: competitor, Victim: ctCoach, Weapon: &common.Equipment{Type: common.EqAK47},
	})
	a.onKill(events.Kill{
		Killer: tCoach, Victim: opponent, Weapon: &common.Equipment{Type: common.EqAK47},
	})
	a.onBombPlanted(events.BombPlanted{BombEvent: events.BombEvent{Player: tCoach}})

	if got := a.players[shooterID].KillStats.Total; got != 0 {
		t.Errorf("competitor kills = %d, want 0", got)
	}
	if got := a.players[shooterID].AssistStats.DamageGiven; got != 0 {
		t.Errorf("competitor damage = %d, want 0", got)
	}
	if got := a.players[victimID].Deaths; got != 0 {
		t.Errorf("opponent deaths = %d, want 0", got)
	}
	if got := a.lastHealth[victimID]; got != 100 {
		t.Errorf("opponent tracked health = %d, want 100", got)
	}
	if a.tracker.bombPlanted {
		t.Error("coach plant changed the competitive round state")
	}

	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})
	for _, id := range []uint64{shooterID, victimID} {
		if got := a.players[id].SideStats.Rounds.Total; got != 1 {
			t.Errorf("player %d rounds = %d, want 1", id, got)
		}
		if got := a.kastRounds[id].Total; got != 1 {
			t.Errorf("player %d KAST rounds = %d, want 1", id, got)
		}
	}
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

func TestPostRoundBombDeathDoesNotCountRawDeath(t *testing.T) {
	a, _, victim := liveRound()
	victim.Inventory = map[int]*common.Equipment{1: {Type: common.EqSmoke}}
	a.parser.(*matchParser).gamePhase = common.GamePhaseGameHalfEnded
	a.onRoundEnd(events.RoundEnd{
		Winner: common.TeamTerrorists,
		Reason: events.RoundEndReasonTargetBombed,
	})

	a.onKill(events.Kill{Victim: victim, Weapon: &common.Equipment{Type: common.EqBomb}})

	got := a.players[victimID]
	if got.Deaths != 0 {
		t.Errorf("deaths = %d, want 0", got.Deaths)
	}
	if got.SideStats.Deaths != (SideCount{}) {
		t.Errorf("side deaths = %+v, want none", got.SideStats.Deaths)
	}
	if got.UtilityStats.UnusedUtilityValue != 300 {
		t.Errorf("unused utility value = %d, want 300", got.UtilityStats.UnusedUtilityValue)
	}
}

func TestRoundEndingBombDeathCountsRawDeathDuringActiveGamePhase(t *testing.T) {
	a, _, victim := liveRound()
	a.parser.(*matchParser).gamePhase = common.GamePhaseStartGamePhase
	a.onRoundEnd(events.RoundEnd{
		Winner: common.TeamTerrorists,
		Reason: events.RoundEndReasonTargetBombed,
	})

	a.onKill(events.Kill{Victim: victim, Weapon: &common.Equipment{Type: common.EqBomb}})

	got := a.players[victimID]
	if got.Deaths != 1 {
		t.Errorf("deaths = %d, want 1", got.Deaths)
	}
	if want := (SideCount{Total: 1, CT: 1}); got.SideStats.Deaths != want {
		t.Errorf("side deaths = %+v, want %+v", got.SideStats.Deaths, want)
	}
}

func TestPostRoundBombDeathStillCancelsSurvivalKast(t *testing.T) {
	a, _, victim := liveRound()
	a.parser.(*matchParser).gamePhase = common.GamePhaseGameHalfEnded
	a.onRoundEnd(events.RoundEnd{
		Winner: common.TeamTerrorists,
		Reason: events.RoundEndReasonTargetBombed,
	})
	a.onKill(events.Kill{Victim: victim, Weapon: &common.Equipment{Type: common.EqBomb}})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	if got := a.kastRounds[victimID].Total; got != 0 {
		t.Errorf("victim KAST rounds = %d, want 0: the bomb death still cancels survival", got)
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

func TestPregameKnifeRoundIsExcludedBeforeFirstScoredRound(t *testing.T) {
	shooter := player(shooterID, "shooter", common.TeamTerrorists)
	victim := player(victimID, "victim", common.TeamCounterTerrorists)
	assister := player(3, "assister", common.TeamTerrorists)
	parser := &matchParser{
		playing:   []*common.Player{shooter, victim},
		gamePhase: common.GamePhasePregame,
	}
	a := newAnalyser(parser)
	knife := &common.Equipment{Type: common.EqKnife}

	a.onRoundStart(events.RoundStart{})
	a.onRoundFreezetimeEnd(events.RoundFreezetimeEnd{})
	a.onBombPlanted(events.BombPlanted{})
	a.onPlayerHurt(events.PlayerHurt{
		Player: victim, Attacker: shooter, Weapon: knife, HealthDamageTaken: 100, Health: 0,
	})
	a.onKill(events.Kill{
		Killer: shooter, Victim: victim, Assister: assister, Weapon: knife, IsHeadshot: true,
	})
	a.onPlayerFlashed(events.PlayerFlashed{Player: victim, Attacker: shooter})
	a.onGrenadeProjectileThrow(grenadeThrow(shooter, common.EqFlash))
	a.onRoundMVP(events.RoundMVPAnnouncement{Player: shooter})
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	if len(a.players) != 0 {
		t.Fatalf("pregame round created player statistics: %+v", a.players)
	}

	parser.gamePhase = common.GamePhaseStartGamePhase
	a.onRoundStart(events.RoundStart{})
	hurtBy(a, shooter, victim, 30)
	a.onKill(events.Kill{
		Killer: shooter, Victim: victim, Weapon: &common.Equipment{Type: common.EqAK47},
	})
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	gotShooter := a.players[shooterID]
	if gotShooter.KillStats.Total != 1 || gotShooter.AssistStats.DamageGiven != 30 {
		t.Errorf("scored-round kills/damage = %d/%d, want 1/30",
			gotShooter.KillStats.Total, gotShooter.AssistStats.DamageGiven)
	}
	if gotShooter.SideStats.Rounds.Total != 1 {
		t.Errorf("scored rounds = %d, want 1", gotShooter.SideStats.Rounds.Total)
	}
	if gotShooter.OpeningDuelStats.OpeningKills.Total != 1 {
		t.Errorf("opening kills = %d, want 1", gotShooter.OpeningDuelStats.OpeningKills.Total)
	}
	if gotShooter.PlayerMapStats.MVPs != 0 || gotShooter.UtilityStats.GrenadesThrown.Total != 0 {
		t.Errorf("pregame MVP/grenade leaked into stats: MVPs=%d grenades=%d",
			gotShooter.PlayerMapStats.MVPs, gotShooter.UtilityStats.GrenadesThrown.Total)
	}
	gotVictim := a.players[victimID]
	if gotVictim.Deaths != 1 || gotVictim.SideStats.Rounds.Total != 1 {
		t.Errorf("scored-round deaths/rounds = %d/%d, want 1/1",
			gotVictim.Deaths, gotVictim.SideStats.Rounds.Total)
	}
}

func TestDemoStartingWithCompetitiveRoundKeepsFirstRound(t *testing.T) {
	shooter := player(shooterID, "shooter", common.TeamTerrorists)
	victim := player(victimID, "victim", common.TeamCounterTerrorists)
	parser := &matchParser{
		playing:   []*common.Player{shooter, victim},
		gamePhase: common.GamePhaseStartGamePhase,
	}
	a := newAnalyser(parser)

	playRound(a, common.TeamTerrorists, events.Kill{
		Killer: shooter, Victim: victim, Weapon: &common.Equipment{Type: common.EqAK47},
	})

	if got := a.players[shooterID].KillStats.Total; got != 1 {
		t.Errorf("kills = %d, want 1", got)
	}
	if got := a.players[shooterID].SideStats.Rounds.Total; got != 1 {
		t.Errorf("rounds = %d, want 1", got)
	}
	if got := a.players[victimID].Deaths; got != 1 {
		t.Errorf("deaths = %d, want 1", got)
	}
}

func TestNextRoundFinalizesDecidedRoundWithoutOfficialEndOnce(t *testing.T) {
	a, shooter, victim := liveRound()
	a.onKill(events.Kill{
		Killer: shooter, Victim: victim, Weapon: &common.Equipment{Type: common.EqAK47},
	})
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	a.onRoundStart(events.RoundStart{})

	if got := a.players[shooterID].SideStats.Rounds.Total; got != 1 {
		t.Fatalf("rounds after fallback finalization = %d, want 1", got)
	}
	if got := a.players[shooterID].OpeningDuelStats.OpeningKills.Total; got != 1 {
		t.Fatalf("opening kills after fallback finalization = %d, want 1", got)
	}

	a.onRoundEnd(events.RoundEnd{Winner: common.TeamCounterTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})
	if got := a.players[shooterID].SideStats.Rounds.Total; got != 2 {
		t.Errorf("rounds after the next official end = %d, want 2", got)
	}
	if got := a.players[shooterID].OpeningDuelStats.OpeningKills.Total; got != 1 {
		t.Errorf("first round was finalized more than once: opening kills = %d", got)
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
	joiner := playerWithCoachingTeam(victimID, "joiner", common.TeamSpectators, common.TeamUnassigned)
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
