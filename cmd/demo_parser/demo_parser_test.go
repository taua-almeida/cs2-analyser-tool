package demoparser

import (
	"maps"
	"math"
	"testing"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	events "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	st "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/sendtables"
)

// The handlers under test only ask the parser for round state, who is
// playing, the current time, and its tick resolution, so these stubs answer
// those questions and leave the embedded interfaces nil.
type matchParser struct {
	demoinfocs.Parser
	playing      []*common.Player
	warmup       bool
	gamePhase    common.GamePhase
	currentTime  time.Duration
	currentFrame int
	tickTime     time.Duration
	ingameTick   int
	roundsPlayed int
	ctScore      int
	tScore       int
	ctClan       string
	tClan        string
}

func (p *matchParser) GameState() demoinfocs.GameState {
	return matchGameState{
		playing: p.playing, warmup: p.warmup, gamePhase: p.gamePhase, ingameTick: p.ingameTick,
		roundsPlayed: p.roundsPlayed, ctScore: p.ctScore, tScore: p.tScore,
		ctClan: p.ctClan, tClan: p.tClan,
	}
}

func (p *matchParser) CurrentTime() time.Duration { return p.currentTime }

func (p *matchParser) CurrentFrame() int { return p.currentFrame }

func (p *matchParser) TickTime() time.Duration { return p.tickTime }

type matchGameState struct {
	demoinfocs.GameState
	playing      []*common.Player
	warmup       bool
	gamePhase    common.GamePhase
	ingameTick   int
	roundsPlayed int
	ctScore      int
	tScore       int
	ctClan       string
	tClan        string
}

func (g matchGameState) IsWarmupPeriod() bool { return g.warmup }

func (g matchGameState) GamePhase() common.GamePhase { return g.gamePhase }

func (g matchGameState) IngameTick() int { return g.ingameTick }

func (g matchGameState) TotalRoundsPlayed() int { return g.roundsPlayed }

func (g matchGameState) TeamCounterTerrorists() *common.TeamState {
	return &common.TeamState{Entity: matchEntity{properties: map[string]st.PropertyValue{
		"m_iScore":         {Any: int32(g.ctScore)},
		"m_szClanTeamname": {Any: g.ctClan},
	}}}
}

func (g matchGameState) TeamTerrorists() *common.TeamState {
	return &common.TeamState{Entity: matchEntity{properties: map[string]st.PropertyValue{
		"m_iScore":         {Any: int32(g.tScore)},
		"m_szClanTeamname": {Any: g.tClan},
	}}}
}

func (g matchGameState) Participants() demoinfocs.Participants {
	return matchParticipants{playing: g.playing}
}

// Rules reports no recorded ConVars, so finalise resolves the game mode from
// the primary value alone.
func (g matchGameState) Rules() demoinfocs.GameRules { return matchRules{} }

type matchRules struct {
	demoinfocs.GameRules
}

func (r matchRules) ConVars() map[string]string { return nil }

type matchParticipants struct {
	demoinfocs.Participants
	playing []*common.Player
}

func (p matchParticipants) Playing() []*common.Player { return p.playing }

func liveAnalyser(playing ...*common.Player) *analyser {
	return newAnalyser(&matchParser{playing: playing, tickTime: time.Second / 64})
}

func markRoundScored(a *analyser) {
	a.parser.(*matchParser).roundsPlayed++
}

func endScoredRound(a *analyser, winner common.Team) {
	markRoundScored(a)
	a.onRoundEnd(events.RoundEnd{Winner: winner})
	a.onRoundEndOfficial(events.RoundEndOfficial{})
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
	return hurtEvent(
		player(shooterID, "shooter", common.TeamTerrorists),
		player(victimID, "victim", common.TeamCounterTerrorists),
		weapon, damageTaken, healthAfter,
	)
}

func hurtEvent(attacker, victim *common.Player, weapon common.EquipmentType, damageTaken, healthAfter int) events.PlayerHurt {
	return events.PlayerHurt{
		Player:            victim,
		Attacker:          attacker,
		Weapon:            &common.Equipment{Type: weapon},
		HealthDamageTaken: damageTaken,
		Health:            healthAfter,
	}
}

func hurtAtFrame(a *analyser, frame int, event events.PlayerHurt) {
	a.parser.(*matchParser).currentFrame = frame
	a.onPlayerHurt(event)
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

			a.captureScoreboardMVPs()
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

	endScoredRound(a, common.TeamTerrorists)
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

// This mirrors the issue #39 sequence: a 15 HP shot, then an attacker-less
// 5 HP World event for the same victim on the next recorded demo frame.
func TestAdjacentWorldDamageCreditsPreviousEnemy(t *testing.T) {
	a, shooter, victim := liveRound()
	a.lastHealth[victimID] = 81

	hurtAtFrame(a, 100, hurtEvent(shooter, victim, common.EqUSP, 15, 66))
	hurtAtFrame(a, 101, hurtEvent(nil, victim, common.EqWorld, 5, 61))
	endScoredRound(a, common.TeamTerrorists)

	got := a.players[shooterID]
	if got.AssistStats.DamageGiven != 20 {
		t.Errorf("damage given = %d, want 20", got.AssistStats.DamageGiven)
	}
	if got.UtilityStats.UtilityDamage != (UtilityDamageStats{}) {
		t.Errorf("utility damage = %+v, want zero for shot and fall damage", got.UtilityStats.UtilityDamage)
	}
	if got := a.sideDamage[shooterID]; got != (SideCount{Total: 20, T: 20}) {
		t.Errorf("side damage = %+v, want 20 on T side", got)
	}
	if got := a.ecoDamage[shooterID]; got != 20 {
		t.Errorf("eco damage = %v, want 20 for two even-tier damage events", got)
	}
	if got := a.tracker.damageTo[victimID][shooterID]; got != 20 {
		t.Errorf("rating assist damage = %d, want 20", got)
	}
}

func TestZeroRealDamageDoesNotEraseWorldDamageSource(t *testing.T) {
	a, shooter, victim := liveRound()
	a.lastHealth[victimID] = 81

	hurtAtFrame(a, 100, hurtEvent(shooter, victim, common.EqNova, 15, 66))
	// Another pellet reports damage against the same resulting health, so it
	// removed no additional HP and must not erase the blast's attribution.
	hurtAtFrame(a, 100, hurtEvent(shooter, victim, common.EqNova, 15, 66))
	hurtAtFrame(a, 101, hurtEvent(nil, victim, common.EqWorld, 5, 61))

	if got := damageGiven(a); got != 20 {
		t.Errorf("damage given = %d, want 20", got)
	}
}

func TestWorldDamageRequiresAdjacentEnemySource(t *testing.T) {
	tests := []struct {
		name       string
		withSource bool
		worldFrame int
		want       int
	}{
		{name: "isolated World event", worldFrame: 101, want: 0},
		{name: "source is two frames old", withSource: true, worldFrame: 102, want: 15},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a, shooter, victim := liveRound()
			a.lastHealth[victimID] = 81
			if test.withSource {
				hurtAtFrame(a, 100, hurtEvent(shooter, victim, common.EqUSP, 15, 66))
			} else {
				a.lastHealth[victimID] = 66
			}
			hurtAtFrame(a, test.worldFrame, hurtEvent(nil, victim, common.EqWorld, 5, 61))

			if got := damageGiven(a); got != test.want {
				t.Errorf("damage given = %d, want %d", got, test.want)
			}
		})
	}
}

func TestWorldFallbackWithHitGroupDoesNotCreditPreviousEnemy(t *testing.T) {
	a, shooter, victim := liveRound()
	a.lastHealth[victimID] = 81
	hurtAtFrame(a, 100, hurtEvent(shooter, victim, common.EqUSP, 15, 66))

	unresolvedHit := hurtEvent(nil, victim, common.EqWorld, 5, 61)
	unresolvedHit.HitGroup = events.HitGroupChest
	hurtAtFrame(a, 101, unresolvedHit)

	if got := damageGiven(a); got != 15 {
		t.Errorf("damage given = %d, want only the resolved 15 HP hit", got)
	}
}

func TestInterveningTeamDamageClearsWorldDamageSource(t *testing.T) {
	a, shooter, victim := liveRound()
	teammate := player(3, "teammate", common.TeamCounterTerrorists)
	a.lastHealth[victimID] = 81

	hurtAtFrame(a, 100, hurtEvent(shooter, victim, common.EqUSP, 15, 66))
	hurtAtFrame(a, 101, hurtEvent(teammate, victim, common.EqM4A4, 7, 59))
	hurtAtFrame(a, 101, hurtEvent(nil, victim, common.EqWorld, 5, 54))

	if got := damageGiven(a); got != 15 {
		t.Errorf("damage given = %d, want only the initial 15", got)
	}
}

func TestRoundStartClearsWorldDamageSource(t *testing.T) {
	a, shooter, victim := liveRound()
	a.lastHealth[victimID] = 81
	hurtAtFrame(a, 100, hurtEvent(shooter, victim, common.EqUSP, 15, 66))

	a.onRoundStart(events.RoundStart{})
	a.lastHealth[victimID] = 100
	hurtAtFrame(a, 101, hurtEvent(nil, victim, common.EqWorld, 5, 95))

	if got := damageGiven(a); got != 15 {
		t.Errorf("damage given = %d, want only the prior round's 15", got)
	}
}

func TestMatchEndWorldCleanupDoesNotCreditDamage(t *testing.T) {
	a, shooter, victim := liveRound()
	a.lastHealth[victimID] = 81
	hurtAtFrame(a, 100, hurtEvent(shooter, victim, common.EqUSP, 15, 66))

	a.parser.(*matchParser).gamePhase = common.GamePhaseGameEnded
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	hurtAtFrame(a, 101, hurtEvent(nil, victim, common.EqWorld, 5, 61))

	if got := damageGiven(a); got != 15 {
		t.Errorf("damage given = %d, want only the pre-cleanup 15", got)
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

	endScoredRound(a, common.TeamCounterTerrorists)

	if got := a.kastRounds[victimID].Total; got != 1 {
		t.Errorf("victim kast rounds = %d, want 1: nobody died all round", got)
	}
}

func TestKillHandlerSeparatesClassicAndFlashAssists(t *testing.T) {
	tests := []struct {
		name          string
		assistedFlash bool
		wantClassic   int
		wantFlashed   int
	}{
		{name: "normal assist", wantClassic: 1},
		{name: "flash assist", assistedFlash: true, wantFlashed: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			killer := player(11, "killer", common.TeamCounterTerrorists)
			victim := player(5, "victim", common.TeamTerrorists)
			assister := player(12, "assister", common.TeamCounterTerrorists)
			finisher := player(1, "finisher", common.TeamTerrorists)
			a := liveAnalyser(killer, victim, assister, finisher)
			a.onRoundStart(events.RoundStart{})

			a.onKill(events.Kill{
				Killer: killer, Victim: victim, Assister: assister,
				AssistedFlash: tt.assistedFlash, Weapon: &common.Equipment{Type: common.EqM4A1},
			})
			a.onKill(events.Kill{
				Killer: finisher, Victim: assister, Weapon: &common.Equipment{Type: common.EqAK47},
			})
			endScoredRound(a, common.TeamTerrorists)

			if got := a.kastRounds[12].Total; got != tt.wantClassic {
				t.Errorf("assister classic-KAST rounds = %d, want %d", got, tt.wantClassic)
			}
			if got := a.ecoKast[12]; got == 0 {
				t.Error("demo assist must reach the separate rating-KAST facts")
			}
			if got := a.players[12].AssistStats; got.Total != 1 || got.FlashedEnemies != tt.wantFlashed {
				t.Errorf("assist stats = %+v, want total 1 and flashed %d", got, tt.wantFlashed)
			}
		})
	}
}

func TestKillHandlerPassesServerTicksToTrades(t *testing.T) {
	const tick = time.Second / 64
	killer := player(11, "killer", common.TeamCounterTerrorists)
	victim := player(1, "victim", common.TeamTerrorists)
	trader := player(2, "trader", common.TeamTerrorists)
	a := liveAnalyser(killer, victim, trader)
	p := a.parser.(*matchParser)
	p.tickTime = tick
	a.onRoundStart(events.RoundStart{})

	p.ingameTick = 640
	a.onKill(events.Kill{Killer: killer, Victim: victim, Weapon: &common.Equipment{Type: common.EqM4A1}})
	p.ingameTick = 961
	a.onKill(events.Kill{Killer: trader, Victim: killer, Weapon: &common.Equipment{Type: common.EqAK47}})
	endScoredRound(a, common.TeamTerrorists)

	if got := a.kastRounds[victim.SteamID64].Total; got != 1 {
		t.Errorf("traded victim classic-KAST rounds = %d, want 1", got)
	}
	if got := a.players[trader.SteamID64].KillStats.TradeKills; got != 1 {
		t.Errorf("trade kills = %d, want 1", got)
	}
}

func TestKillHandlerRejectsUnavailableTickTime(t *testing.T) {
	for _, tickTime := range []time.Duration{0, -1} {
		t.Run(tickTime.String(), func(t *testing.T) {
			killer := player(11, "killer", common.TeamCounterTerrorists)
			victim := player(1, "victim", common.TeamTerrorists)
			a := liveAnalyser(killer, victim)
			a.parser.(*matchParser).tickTime = tickTime
			a.onRoundStart(events.RoundStart{})

			a.onKill(events.Kill{
				Killer: killer, Victim: victim, Weapon: &common.Equipment{Type: common.EqM4A1},
			})

			if a.parseErr == nil {
				t.Fatal("unavailable tick duration did not produce a parser error")
			}
		})
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
	markRoundScored(a)
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
	markRoundScored(a)
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
		tickTime:  time.Second / 64,
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
	endScoredRound(a, common.TeamTerrorists)

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
		tickTime:  time.Second / 64,
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

func TestUnscoredSetupRoundDiscardsRoundFactsButKeepsRawEvents(t *testing.T) {
	victim := player(1, "victim", common.TeamTerrorists)
	trader := player(2, "trader", common.TeamTerrorists)
	killer := player(11, "killer", common.TeamCounterTerrorists)
	otherCTs := []*common.Player{
		player(12, "ct-12", common.TeamCounterTerrorists),
		player(13, "ct-13", common.TeamCounterTerrorists),
		player(14, "ct-14", common.TeamCounterTerrorists),
		player(15, "ct-15", common.TeamCounterTerrorists),
	}
	a := liveAnalyser(append([]*common.Player{victim, trader, killer}, otherCTs...)...)
	a.parser.(*matchParser).gamePhase = common.GamePhaseStartGamePhase
	a.onRoundStart(events.RoundStart{})

	hurtBy(a, killer, victim, 30)
	a.parser.(*matchParser).ingameTick = 640
	a.onKill(events.Kill{Killer: killer, Victim: victim, Weapon: &common.Equipment{Type: common.EqM4A1}})
	a.parser.(*matchParser).ingameTick = 768
	a.onKill(events.Kill{Killer: trader, Victim: killer, Weapon: &common.Equipment{Type: common.EqAK47}})
	for _, ct := range otherCTs {
		a.onKill(events.Kill{Killer: trader, Victim: ct, Weapon: &common.Equipment{Type: common.EqAK47}})
	}
	a.onRoundMVP(events.RoundMVPAnnouncement{Player: trader})
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	// A restart at the same authoritative round count proves the prior
	// outcome was setup rather than a played round.
	a.onRoundStart(events.RoundStart{})

	if got := a.players[killer.SteamID64].KillStats.Total; got != 1 {
		t.Errorf("raw kills = %d, want 1", got)
	}
	if got := a.players[victim.SteamID64].Deaths; got != 1 {
		t.Errorf("raw deaths = %d, want 1", got)
	}
	if got := a.players[trader.SteamID64].KillStats.Total; got != 5 {
		t.Errorf("raw trader kills = %d, want 5", got)
	}
	if got := a.players[killer.SteamID64].AssistStats.DamageGiven; got != 30 {
		t.Errorf("raw damage = %d, want 30", got)
	}
	for _, id := range []uint64{victim.SteamID64, trader.SteamID64, killer.SteamID64} {
		if got := a.players[id].SideStats.Rounds.Total; got != 0 {
			t.Errorf("player %d rounds = %d, want 0", id, got)
		}
		if got := a.kastRounds[id].Total; got != 0 {
			t.Errorf("player %d KAST rounds = %d, want 0", id, got)
		}
		if got := a.ecoSurvival[id]; got != 0 {
			t.Errorf("player %d rating survival = %v, want 0", id, got)
		}
		if got := a.ecoKast[id]; got != 0 {
			t.Errorf("player %d rating KAST = %v, want 0", id, got)
		}
	}
	if got := a.players[trader.SteamID64].KillStats.TradeKills; got != 0 {
		t.Errorf("trade kills = %d, want 0", got)
	}
	if got := a.players[victim.SteamID64].DeathsTraded.Total; got != 0 {
		t.Errorf("traded deaths = %d, want 0", got)
	}
	if got := a.players[killer.SteamID64].OpeningDuelStats.OpeningKills.Total; got != 0 {
		t.Errorf("opening kills = %d, want 0", got)
	}
	if got := a.players[trader.SteamID64].PlayerMapStats.ClutchesWon; got != 0 {
		t.Errorf("clutches = %d, want 0", got)
	}
	if got := a.players[trader.SteamID64].PlayerMapStats.MVPs; got != 0 {
		t.Errorf("MVPs = %d, want 0", got)
	}
	if got := a.players[trader.SteamID64].PlayerMapStats.ACEs; got != 0 {
		t.Errorf("ACEs = %d, want 0", got)
	}
	if got := a.players[trader.SteamID64].PlayerMapStats.MultiKills; got != (MultiKillRounds{}) {
		t.Errorf("multi-kills = %+v, want none", got)
	}
	if len(a.roundSwing) != 0 {
		t.Errorf("round swing was committed for setup round: %v", a.roundSwing)
	}
	if len(a.ecoKills) != 0 || len(a.ecoDamage) != 0 {
		t.Errorf("rating kill/damage facts were committed for setup round: kills=%v damage=%v", a.ecoKills, a.ecoDamage)
	}
}

func TestMVPFactsWaitForAuthoritativeScore(t *testing.T) {
	eventMVP := player(1, "event-mvp", common.TeamTerrorists)
	scoreboardMVP := player(2, "scoreboard-mvp", common.TeamCounterTerrorists)
	scoreboardMVP.Entity = matchEntity{properties: map[string]st.PropertyValue{
		"m_iMVPs": {Any: int32(1)},
	}}
	a := liveAnalyser(eventMVP, scoreboardMVP)
	a.onRoundStart(events.RoundStart{})
	a.onRoundMVP(events.RoundMVPAnnouncement{Player: eventMVP})
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	// Starting again at the same score leaves both event and scoreboard facts
	// uncommitted; the next scored-round slot belongs to the decided round.
	a.onRoundStart(events.RoundStart{})
	for _, id := range []uint64{eventMVP.SteamID64, scoreboardMVP.SteamID64} {
		if got := a.players[id].PlayerMapStats.MVPs; got != 0 {
			t.Errorf("setup MVPs for player %d = %d, want 0", id, got)
		}
	}

	a.onRoundMVP(events.RoundMVPAnnouncement{Player: eventMVP})
	endScoredRound(a, common.TeamTerrorists)
	if got := a.players[eventMVP.SteamID64].PlayerMapStats.MVPs; got != 1 {
		t.Errorf("event MVPs = %d, want 1", got)
	}
	if got := a.players[scoreboardMVP.SteamID64].PlayerMapStats.MVPs; got != 1 {
		t.Errorf("scoreboard MVPs = %d, want 1", got)
	}
}

func TestMVPAnnouncementAfterOfficialEndUsesFinalizedRound(t *testing.T) {
	mvp := player(1, "mvp", common.TeamTerrorists)
	opponent := player(11, "opponent", common.TeamCounterTerrorists)
	a := liveAnalyser(mvp, opponent)
	a.onRoundStart(events.RoundStart{})
	endScoredRound(a, common.TeamTerrorists)

	a.onRoundMVP(events.RoundMVPAnnouncement{Player: mvp})

	if got := a.players[mvp.SteamID64].PlayerMapStats.MVPs; got != 1 {
		t.Errorf("late event MVPs = %d, want 1", got)
	}
}

func TestFinaliseAppliesLateScoreboardMVPUpdate(t *testing.T) {
	mvp := player(1, "mvp", common.TeamTerrorists)
	opponent := player(11, "opponent", common.TeamCounterTerrorists)
	properties := map[string]st.PropertyValue{"m_iMVPs": {Any: int32(0)}}
	mvp.Entity = matchEntity{properties: properties}
	a := liveAnalyser(mvp, opponent)
	a.onRoundStart(events.RoundStart{})
	endScoredRound(a, common.TeamTerrorists)

	properties["m_iMVPs"] = st.PropertyValue{Any: int32(1)}
	a.finalise()

	if got := a.players[mvp.SteamID64].PlayerMapStats.MVPs; got != 1 {
		t.Errorf("late scoreboard MVPs = %d, want 1", got)
	}
}

func TestFinaliseDoesNotApplyUnscoredScoreboardMVP(t *testing.T) {
	mvp := player(1, "mvp", common.TeamTerrorists)
	opponent := player(11, "opponent", common.TeamCounterTerrorists)
	mvp.Entity = matchEntity{properties: map[string]st.PropertyValue{
		"m_iMVPs": {Any: int32(1)},
	}}
	a := liveAnalyser(mvp, opponent)
	a.onRoundStart(events.RoundStart{})
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	a.finalise()

	if got := a.players[mvp.SteamID64].PlayerMapStats.MVPs; got != 0 {
		t.Errorf("unscored scoreboard MVPs = %d, want 0", got)
	}
}

func TestHalftimeRoundCommitsAfterScoreAdvance(t *testing.T) {
	shooter := player(shooterID, "shooter", common.TeamTerrorists)
	victim := player(victimID, "victim", common.TeamCounterTerrorists)
	parser := &matchParser{
		playing:      []*common.Player{shooter, victim},
		gamePhase:    common.GamePhaseStartGamePhase,
		roundsPlayed: 11,
		tickTime:     time.Second / 64,
	}
	a := newAnalyser(parser)
	a.onRoundStart(events.RoundStart{})
	a.onKill(events.Kill{Killer: shooter, Victim: victim, Weapon: &common.Equipment{Type: common.EqAK47}})

	parser.gamePhase = common.GamePhaseGameHalfEnded
	endScoredRound(a, common.TeamTerrorists)

	if got := a.players[shooterID].SideStats.Rounds; got != (SideCount{Total: 1, T: 1}) {
		t.Errorf("halftime rounds = %+v, want one T round", got)
	}
	if got := a.players[shooterID].OpeningDuelStats.OpeningKills.Total; got != 1 {
		t.Errorf("halftime opening kills = %d, want 1", got)
	}
}

func TestDelayedRoundCountDoesNotDiscardScoredRound(t *testing.T) {
	shooter := player(shooterID, "shooter", common.TeamTerrorists)
	victim := player(victimID, "victim", common.TeamCounterTerrorists)
	a := liveAnalyser(shooter, victim)
	p := a.parser.(*matchParser)
	a.onRoundStart(events.RoundStart{})
	a.onKill(events.Kill{
		Killer: shooter, Victim: victim, Weapon: &common.Equipment{Type: common.EqAK47},
	})
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	// The next round begins before m_totalRoundsPlayed reflects round one.
	a.onRoundStart(events.RoundStart{})
	if got := a.players[shooterID].SideStats.Rounds.Total; got != 0 {
		t.Fatalf("round committed before the delayed authoritative count: %d", got)
	}

	// When the property advances only once at round two's end, the oldest
	// decided outcome (round one) consumes that slot.
	p.roundsPlayed = 1
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamCounterTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})
	if got := a.players[shooterID].SideStats.Rounds.Total; got != 1 {
		t.Fatalf("rounds after delayed advance = %d, want 1", got)
	}
	if got := a.players[shooterID].OpeningDuelStats.OpeningKills.Total; got != 1 {
		t.Fatalf("the first round's opening kill was lost: %d", got)
	}

	// A later observation catches the second round up without duplicating the
	// first one's facts.
	p.roundsPlayed = 2
	a.onRoundStart(events.RoundStart{})
	if got := a.players[shooterID].SideStats.Rounds.Total; got != 2 {
		t.Errorf("rounds after catch-up = %d, want 2", got)
	}
	if got := a.players[shooterID].OpeningDuelStats.OpeningKills.Total; got != 1 {
		t.Errorf("opening kill was duplicated during catch-up: %d", got)
	}
}

func TestFinaliseFlushesLastScoredRoundWithoutOfficialEnd(t *testing.T) {
	a, shooter, victim := liveRound()
	a.onKill(events.Kill{Killer: shooter, Victim: victim, Weapon: &common.Equipment{Type: common.EqAK47}})
	markRoundScored(a)
	a.parser.(*matchParser).tScore = 1
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})

	a.finalise()

	if got := a.mapData.TotalRounds; got != 1 {
		t.Errorf("map rounds = %d, want 1", got)
	}
	if got := a.players[shooterID].SideStats.Rounds.Total; got != 1 {
		t.Errorf("player rounds = %d, want 1", got)
	}
	if got := a.players[shooterID].OpeningDuelStats.OpeningKills.Total; got != 1 {
		t.Errorf("opening kills = %d, want 1", got)
	}
}

func TestDuplicateOfficialEndDoesNotCommitRoundTwice(t *testing.T) {
	a, _, _ := liveRound()
	endScoredRound(a, common.TeamCounterTerrorists)
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	if got := a.players[shooterID].SideStats.Rounds.Total; got != 1 {
		t.Errorf("player rounds = %d, want 1", got)
	}
	if got := a.kastRounds[shooterID].Total; got != 1 {
		t.Errorf("KAST rounds = %d, want 1", got)
	}
}

func TestNextRoundFinalizesDecidedRoundWithoutOfficialEndOnce(t *testing.T) {
	a, shooter, victim := liveRound()
	a.onKill(events.Kill{
		Killer: shooter, Victim: victim, Weapon: &common.Equipment{Type: common.EqAK47},
	})
	markRoundScored(a)
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})
	a.onRoundStart(events.RoundStart{})

	if got := a.players[shooterID].SideStats.Rounds.Total; got != 1 {
		t.Fatalf("rounds after fallback finalization = %d, want 1", got)
	}
	if got := a.players[shooterID].OpeningDuelStats.OpeningKills.Total; got != 1 {
		t.Fatalf("opening kills after fallback finalization = %d, want 1", got)
	}

	endScoredRound(a, common.TeamCounterTerrorists)
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
	endScoredRound(a, common.TeamTerrorists)

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
	endScoredRound(a, winner)
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
		endScoredRound(a, common.TeamCounterTerrorists)
	}

	// Halftime, then a single round on CT with 60 damage and a survival.
	shooter.Team, victim.Team = common.TeamCounterTerrorists, common.TeamTerrorists
	a.onRoundStart(events.RoundStart{})
	hurtBy(a, shooter, victim, 60)
	endScoredRound(a, common.TeamCounterTerrorists)

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
	endScoredRound(a, common.TeamTerrorists)

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
	endScoredRound(a, common.TeamTerrorists)

	if got := a.players[shooterID].OpeningDuelStats.OpeningKills.Total; got != 0 {
		t.Errorf("shooter opening kills = %d, want 0: the bot-on-bot kill opened the round first", got)
	}
}

func TestResolveGameMode(t *testing.T) {
	// The explicit ruleset the game-mode fallback needs, as the
	// Rooster–Mindfreak HLTV demos of issue #31 record it.
	competitiveConVars := func() map[string]string {
		return map[string]string{
			"mp_maxrounds":    "24",
			"mp_halftime":     "true",
			"mp_friendlyfire": "true",
		}
	}
	withConVars := func(overrides map[string]string) map[string]string {
		conVars := competitiveConVars()
		maps.Copy(conVars, overrides)
		return conVars
	}
	withoutHalftime := competitiveConVars()
	delete(withoutHalftime, "mp_halftime")

	tests := []struct {
		name    string
		primary string
		conVars map[string]string
		want    string
	}{
		{
			name:    "primary premier wins over competitive rules",
			primary: "premier",
			conVars: competitiveConVars(),
			want:    "premier",
		},
		{
			name:    "tournament ruleset fills empty primary",
			conVars: competitiveConVars(),
			want:    "competitive",
		},
		{
			name:    "numeric boolean spellings qualify",
			conVars: withConVars(map[string]string{"mp_halftime": "1", "mp_friendlyfire": "1"}),
			want:    "competitive",
		},
		{
			name: "no metadata stays unknown",
			want: "",
		},
		{
			name:    "wingman round count stays unknown",
			conVars: withConVars(map[string]string{"mp_maxrounds": "16"}),
			want:    "",
		},
		{
			name:    "casual friendly fire stays unknown",
			conVars: withConVars(map[string]string{"mp_friendlyfire": "false"}),
			want:    "",
		},
		{
			name:    "unrecorded halftime stays unknown",
			conVars: withoutHalftime,
			want:    "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveGameMode(test.primary, test.conVars); got != test.want {
				t.Errorf("resolveGameMode(%q, %v) = %q, want %q",
					test.primary, test.conVars, got, test.want)
			}
		})
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
	endScoredRound(a, common.TeamTerrorists)

	if got := a.players[shooterID].PlayerMapStats.ClutchesWon; got != 1 {
		t.Errorf("clutches won = %d, want 1", got)
	}
}
