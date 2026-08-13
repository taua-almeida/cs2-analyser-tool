package demoparser

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	events "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"
)

// analyser accumulates player stats while the demo is parsed. Round-scoped
// bookkeeping (clutches, aces, trades, KAST) is delegated to roundTracker.
type analyser struct {
	parser         demoinfocs.Parser
	players        map[uint64]*DemoPlayer
	tracker        *roundTracker
	kastRounds     map[uint64]SideCount // rounds with KAST, total and per side
	sideDamage     map[uint64]SideCount // damage given, total and per side
	openingWins    map[uint64]int       // rounds won after taking the opening kill
	lastHealth     map[uint64]int
	worldSource    map[uint64]worldDamageSource
	flashEnds      map[uint64]time.Duration // latest known end of each player's blind interval
	ecoKills       map[uint64]float64       // eco-adjusted kill points
	ecoDamage      map[uint64]float64       // eco-adjusted damage given
	roundSwing     map[uint64]float64       // summed round-win-probability swing
	ecoSurvival    map[uint64]float64       // eco-weighted rounds survived
	ecoKast        map[uint64]float64       // eco-weighted rounds with rating KAST credit
	roundTiers     map[uint64]equipTier     // loadout tier per player in the current round
	roundMVPs      map[uint64]int           // cumulative scoreboard MVPs captured for the current round
	roundEcoKills  []playerRatingFact       // rating kill points in event order for the current round
	roundEcoDamage []playerRatingFact       // rating damage points in event order for the current round
	pendingRounds  []*pendingRoundFacts     // finalized facts waiting for scoreboard advances
	latestRound    *pendingRoundFacts       // most recently finalized round, for late MVP events
	appliedRounds  int                      // authoritative round count already represented in player facts
	roundsStarted  bool                     // whether appliedRounds has been initialized
	eventMVPs      map[uint64]int           // MVP announcements from authoritative scored rounds
	parseErr       error                    // delayed handler error returned after parsing
	mapData        MapData
	gameMode       string
}

type pendingRoundFacts struct {
	outcome    roundOutcome
	tiers      map[uint64]equipTier
	mvps       map[uint64]int
	ecoKills   []playerRatingFact
	ecoDamage  []playerRatingFact
	scored     bool
	mvpCounted bool
}

type playerRatingFact struct {
	player uint64
	value  float64
}

type worldDamageSource struct {
	attacker *common.Player
	frame    int
}

func newAnalyser(parser demoinfocs.Parser) *analyser {
	return &analyser{
		parser:      parser,
		players:     make(map[uint64]*DemoPlayer),
		tracker:     newRoundTracker(),
		kastRounds:  make(map[uint64]SideCount),
		sideDamage:  make(map[uint64]SideCount),
		openingWins: make(map[uint64]int),
		lastHealth:  make(map[uint64]int),
		worldSource: make(map[uint64]worldDamageSource),
		flashEnds:   make(map[uint64]time.Duration),
		ecoKills:    make(map[uint64]float64),
		ecoDamage:   make(map[uint64]float64),
		roundSwing:  make(map[uint64]float64),
		ecoSurvival: make(map[uint64]float64),
		ecoKast:     make(map[uint64]float64),
		roundTiers:  make(map[uint64]equipTier),
		roundMVPs:   make(map[uint64]int),
		eventMVPs:   make(map[uint64]int),
	}
}

func ProcessDemo(demoPath string) (*ProcessedDemo, error) {
	file, err := os.Open(demoPath)
	if err != nil {
		return nil, fmt.Errorf("opening demo file: %w", err)
	}
	defer file.Close()

	a := newAnalyser(demoinfocs.NewParser(file))
	defer a.parser.Close()

	a.registerHandlers()

	if err := a.parser.ParseToEnd(); err != nil {
		return nil, fmt.Errorf("parsing demo: %w", err)
	}
	if a.parseErr != nil {
		return nil, fmt.Errorf("parsing demo: %w", a.parseErr)
	}

	a.finalise()

	return &ProcessedDemo{
		Players:  a.players,
		Map:      a.mapData,
		GameMode: a.gameMode,
	}, nil
}

func (p DemoPlayer) String() string {
	return fmt.Sprintf("Player: %s (SteamID: %d)\nKills: %d, Deaths: %d, Headshots: %d, Precision: %.1f%%\n",
		p.Name, p.SteamID, p.KillStats.Total, p.Deaths, p.KillStats.HeadShots, p.KillStats.Precision*100)
}

func (a *analyser) registerHandlers() {
	a.parser.RegisterNetMessageHandler(func(m *msg.CSVCMsg_ServerInfo) {
		a.mapData.MapName = m.GetMapName()
		if cfg := m.GetGameSessionConfig(); cfg != nil {
			a.gameMode = cfg.GetGamemode()
		}
	})
	a.parser.RegisterEventHandler(a.onRoundStart)
	a.parser.RegisterEventHandler(a.onRoundFreezetimeEnd)
	a.parser.RegisterEventHandler(a.onBombPlanted)
	a.parser.RegisterEventHandler(a.onKill)
	a.parser.RegisterEventHandler(a.onPlayerHurt)
	a.parser.RegisterEventHandler(a.onPlayerFlashed)
	a.parser.RegisterEventHandler(a.onGrenadeProjectileThrow)
	a.parser.RegisterEventHandler(a.onRoundMVP)
	a.parser.RegisterEventHandler(a.onRoundEnd)
	a.parser.RegisterEventHandler(a.onRoundEndOfficial)
	a.parser.RegisterEventHandler(a.onDisconnect)
}

// isCoachingController reports whether p is affiliated with a side as a
// coach rather than as a competitor. Such controllers still have a T/CT
// Team and a live player pawn, but m_iCoachingTeam is non-zero while they
// are coaching.
func isCoachingController(p *common.Player) bool {
	if p == nil || p.Entity == nil {
		return false
	}
	coachingTeam, ok := p.Entity.PropertyValue("m_iCoachingTeam")
	// A serializer can expose the property before its value arrives. Treat
	// that as unavailable rather than evidence that the player is a coach.
	team, set := coachingTeam.Any.(int32)
	return ok && set && common.Team(team) != common.TeamUnassigned
}

// ensurePlayer returns the stats entry for p, creating it on first sight.
// Lazy creation covers demos where recording started after players
// connected, so no PlayerConnect event is ever seen for them.
func (a *analyser) ensurePlayer(p *common.Player) *DemoPlayer {
	if p == nil || p.IsBot || p.SteamID64 == 0 || isCoachingController(p) {
		return nil
	}
	dp, ok := a.players[p.SteamID64]
	if !ok {
		dp = &DemoPlayer{
			SteamID:   p.SteamID64,
			UserID:    p.UserID,
			KillStats: KillStats{WeaponsKills: make(map[string]int)},
		}
		a.players[p.SteamID64] = dp
	}
	if p.Name != "" {
		dp.Name = p.Name
	}
	return dp
}

// trackerID returns a round-tracker key that is unique even for bots, whose
// SteamID64 is always 0. Bot user IDs are small, so the offset bit cannot
// collide with a real 64-bit SteamID.
func trackerID(p *common.Player) uint64 {
	if p == nil {
		return 0
	}
	if p.SteamID64 != 0 {
		return p.SteamID64
	}
	return uint64(p.UserID) | 1<<62
}

func (a *analyser) inWarmupOrPregame() bool {
	state := a.parser.GameState()
	// Tournament knife rounds can report match-started and non-warmup while
	// the game rules still identify them as pregame. Rejecting that phase
	// keeps the following 0:0 competitive round without relying on its order.
	return state.IsWarmupPeriod() || state.GamePhase() == common.GamePhasePregame
}

// playingRoster reads the players currently on a side into the shape the
// round tracker takes, registering anyone seen for the first time, seeding
// the health their damage is measured against, and snapshotting the loadout
// tier the round's eco weights use. The tier read at the end of freeze time
// overwrites the round-start one, so it reflects what was actually bought.
func (a *analyser) playingRoster() map[uint64]common.Team {
	roster := make(map[uint64]common.Team)
	for _, p := range a.parser.GameState().Participants().Playing() {
		if p.Team != common.TeamTerrorists && p.Team != common.TeamCounterTerrorists {
			continue
		}
		if isCoachingController(p) {
			continue
		}
		id := trackerID(p)
		roster[id] = p.Team
		a.lastHealth[id] = p.Health()
		a.roundTiers[id] = playerTier(p.Inventory)
		a.ensurePlayer(p)
	}
	return roster
}

func (a *analyser) onRoundStart(e events.RoundStart) {
	if a.inWarmupOrPregame() {
		return
	}
	if !a.roundsStarted {
		a.appliedRounds = a.parser.GameState().TotalRoundsPlayed()
		a.roundsStarted = true
	}
	// Close the previous round in case its official end was never seen. It
	// remains queued until the authoritative round count creates room for it;
	// this tolerates a late property update without crediting a same-score
	// setup RoundStart.
	a.finalizeRoundFacts()
	a.latestRound = nil
	clear(a.roundTiers)
	clear(a.roundMVPs)
	a.roundEcoKills = a.roundEcoKills[:0]
	a.roundEcoDamage = a.roundEcoDamage[:0]
	clear(a.worldSource)
	a.tracker.startRound(a.playingRoster())
}

// onRoundFreezetimeEnd folds the players who picked a side during freeze
// time into the round. RoundStart snapshots the roster before that window
// opens, so without this they play a round that counts them nowhere: their
// kills, deaths and damage land on a side whose round count never moved.
func (a *analyser) onRoundFreezetimeEnd(e events.RoundFreezetimeEnd) {
	if a.inWarmupOrPregame() {
		return
	}
	a.tracker.joinRound(a.playingRoster())
}

// onBombPlanted feeds the round tracker the bomb state its round-swing
// win-probability model conditions on.
func (a *analyser) onBombPlanted(e events.BombPlanted) {
	if a.inWarmupOrPregame() || isCoachingController(e.Player) {
		return
	}
	a.tracker.plantBomb()
}

func (a *analyser) onKill(e events.Kill) {
	if a.inWarmupOrPregame() || e.Victim == nil ||
		isCoachingController(e.Victim) || isCoachingController(e.Killer) {
		return
	}

	// Suicides and world deaths (fall damage, C4) have no killer to credit.
	// Bots all report SteamID64 0, so identity has to go through trackerID
	// (which gives each bot a distinct ID) or two different bots trading
	// kills would misread as one bot suiciding on itself.
	suicide := e.Killer != nil && trackerID(e.Killer) == trackerID(e.Victim)
	// World kills can carry the victim as their own killer in CS2 demos
	// (mid-round disconnects, match-end cleanup). The tracker uses the round
	// state to distinguish cleanup from World deaths that still count. Bomb
	// kills stay separate from both.
	byWorld := (e.Killer == nil || suicide) && (e.Weapon == nil || e.Weapon.Type == common.EqWorld)
	postRoundWorld := a.tracker.isPostRoundWorldCleanup(byWorld)
	gamePhase := a.parser.GameState().GamePhase()
	matchEndWorldCleanup := postRoundWorld &&
		gamePhase == common.GamePhaseGameEnded
	postRoundBombDeath := a.tracker.isPostRound() &&
		gamePhase == common.GamePhaseGameHalfEnded &&
		e.Weapon != nil && e.Weapon.Type == common.EqBomb

	if victim := a.ensurePlayer(e.Victim); victim != nil {
		// HLTV omits C4 deaths after the round and game phase have both
		// closed. Bomb deaths emitted just after a round-ending explosion
		// during the active phase still count. Only match-end World events
		// are cleanup; ordinary post-round falls and World suicides count.
		if !postRoundBombDeath && !matchEndWorldCleanup {
			victim.Deaths++
			victim.SideStats.Deaths.count(e.Victim.Team)
		}
		if !postRoundWorld {
			victim.UtilityStats.UnusedUtilityValue += unusedUtilityValue(e.Victim.Inventory)
		}
	}

	var killerID uint64
	var killerTeam common.Team
	if e.Killer != nil && !suicide {
		killerID = trackerID(e.Killer)
		killerTeam = e.Killer.Team
	}
	teamkill := killerID != 0 && killerTeam == e.Victim.Team

	if killer := a.ensurePlayer(e.Killer); killer != nil && !suicide {
		if teamkill {
			killer.KillStats.TeamKills++
		} else {
			killer.KillStats.Total++
			killer.SideStats.Kills.count(killerTeam)
			if e.IsHeadshot {
				killer.KillStats.HeadShots++
			}
			if e.Weapon != nil && e.Weapon.Type != common.EqWorld {
				killer.KillStats.WeaponsKills[e.Weapon.String()]++
			}
			// The duel is priced loadout against loadout at kill time; the
			// victim's inventory is still their pre-death one during Kill,
			// which TestProcessDemoGolden pins for unused utility. Keyed by
			// SteamID like ecoDamage, matching how derive reads both back.
			a.roundEcoKills = append(a.roundEcoKills, playerRatingFact{
				player: killer.SteamID,
				value:  killPoints(playerTier(e.Killer.Inventory), playerTier(e.Victim.Inventory)),
			})
		}
	}

	var assisterID uint64
	if !teamkill && e.Assister != nil &&
		e.Assister.Team != e.Victim.Team && !isCoachingController(e.Assister) {
		assisterID = trackerID(e.Assister)
		if assister := a.ensurePlayer(e.Assister); assister != nil {
			assister.AssistStats.Total++
			if e.AssistedFlash {
				assister.AssistStats.FlashedEnemies++
			}
		}
	}

	tickTime := a.parser.TickTime()
	if tickTime <= 0 && a.parseErr == nil {
		a.parseErr = fmt.Errorf("demo tick duration is %s; cannot calculate trades", tickTime)
	}
	a.tracker.kill(killerID, trackerID(e.Victim), killerTeam, e.Victim.Team,
		assisterID, e.AssistedFlash, byWorld, a.parser.GameState().IngameTick(), tickTime)
}

func (a *analyser) onPlayerHurt(e events.PlayerHurt) {
	if a.inWarmupOrPregame() || e.Player == nil ||
		isCoachingController(e.Player) || isCoachingController(e.Attacker) {
		return
	}

	// HealthDamageTaken is capped by the victim's health at the start of
	// the tick, so simultaneous shotgun pellets each report overlapping
	// damage. Capping every event at the victim's actual health drop
	// removes the overlap without crediting engine rounding leftovers.
	victimID := trackerID(e.Player)
	before, seen := a.lastHealth[victimID]
	if !seen {
		before = 100
	}
	realDamage := min(e.HealthDamageTaken, max(0, before-e.Health))
	a.lastHealth[victimID] = e.Health

	// In the #39 demo, a landing appears as an attacker-less World event one
	// demo frame after an enemy hit, and HLTV includes its 5 HP in that
	// attacker's ADR. demoinfocs also uses EqWorld as a fallback for unresolved
	// empty-weapon events, so attribution additionally requires the landing's
	// generic hit group, lack of armor damage, adjacent frame and same victim.
	// Isolated World damage remains uncredited.
	frame := a.parser.CurrentFrame()
	source, hasSource := a.worldSource[victimID]
	// A later pellet from the same blast can report no new health loss. Keep
	// the blast as the source, but let every other intervening hurt invalidate it.
	sameSourceOverlap := realDamage == 0 && e.Attacker != nil && hasSource &&
		frame == source.frame && trackerID(e.Attacker) == trackerID(source.attacker)
	if hasSource && !sameSourceOverlap {
		delete(a.worldSource, victimID)
	}
	damageAttacker := e.Attacker
	unattributedWorldDamage := realDamage > 0 && damageAttacker == nil &&
		e.Weapon != nil && e.Weapon.Type == common.EqWorld &&
		e.HitGroup == events.HitGroupGeneric && e.ArmorDamageTaken == 0
	matchEndWorldCleanup := a.tracker.isPostRoundWorldCleanup(unattributedWorldDamage) &&
		a.parser.GameState().GamePhase() == common.GamePhaseGameEnded
	if unattributedWorldDamage && hasSource && !matchEndWorldCleanup &&
		frame >= source.frame && frame-source.frame <= 1 {
		damageAttacker = source.attacker
	}

	if damageAttacker == nil {
		return
	}
	// Team damage and self damage never count towards damage given, and
	// following HLTV convention neither does bomb damage (when the game
	// credits the explosion to the planter).
	if damageAttacker.SteamID64 == e.Player.SteamID64 || damageAttacker.Team == e.Player.Team {
		return
	}
	if e.Weapon != nil && e.Weapon.Type == common.EqBomb {
		return
	}
	attackerStats := a.ensurePlayer(damageAttacker)
	if attackerStats == nil {
		return
	}
	if e.Attacker != nil && realDamage > 0 {
		a.worldSource[victimID] = worldDamageSource{attacker: damageAttacker, frame: frame}
	}
	attackerStats.AssistStats.DamageGiven += realDamage
	addSide(a.sideDamage, attackerStats.SteamID, damageAttacker.Team, realDamage)
	if e.Weapon != nil {
		attackerStats.UtilityStats.UtilityDamage.add(e.Weapon.Type, realDamage)
	}
	// Zero-damage events are common (overlapping shotgun pellets, fully
	// absorbed hits) and cannot change either total, so skip pricing
	// their duel.
	if realDamage > 0 {
		a.roundEcoDamage = append(a.roundEcoDamage, playerRatingFact{
			player: attackerStats.SteamID,
			value: float64(realDamage) *
				ecoDuelFactor(playerTier(damageAttacker.Inventory), playerTier(e.Player.Inventory)),
		})
		a.tracker.damage(trackerID(damageAttacker), victimID, realDamage)
	}
}

func (a *analyser) onPlayerFlashed(e events.PlayerFlashed) {
	if a.inWarmupOrPregame() || e.Player == nil ||
		isCoachingController(e.Player) || isCoachingController(e.Attacker) {
		return
	}
	addedBlindTime := a.addedFlashTime(e.Player, e.FlashDuration())
	if e.Attacker == nil {
		return
	}
	if trackerID(e.Player) == trackerID(e.Attacker) {
		return
	}

	attacker := a.ensurePlayer(e.Attacker)
	if attacker == nil {
		return
	}
	if e.Player.Team == e.Attacker.Team {
		attacker.UtilityStats.FriendsFlashed++
		return
	}
	attacker.UtilityStats.EnemiesFlashed++
	attacker.UtilityStats.EnemyFlashTimeSeconds += addedBlindTime.Seconds()
}

func (a *analyser) onGrenadeProjectileThrow(e events.GrenadeProjectileThrow) {
	if a.inWarmupOrPregame() || e.Projectile == nil {
		return
	}
	if thrower := a.ensurePlayer(e.Projectile.Thrower); thrower != nil {
		thrower.UtilityStats.GrenadesThrown.add(projectileGrenadeType(e.Projectile))
	}
}

func (a *analyser) onRoundMVP(e events.RoundMVPAnnouncement) {
	if a.inWarmupOrPregame() {
		return
	}
	if player := a.ensurePlayer(e.Player); player != nil {
		if a.tracker.live {
			a.tracker.markMVP(player.SteamID)
			return
		}
		// Some demos announce the MVP after RoundEndOfficial finalized the
		// tracker. Attach it to that round until the next RoundStart instead of
		// silently dropping it.
		if a.latestRound != nil && a.latestRound.outcome.mvp == 0 {
			a.latestRound.outcome.mvp = player.SteamID
			a.applyEventMVP(a.latestRound)
		}
	}
}

func (a *analyser) onDisconnect(e events.PlayerDisconnected) {
	if e.Player != nil {
		a.tracker.disconnect(trackerID(e.Player))
	}
}

func (a *analyser) onRoundEnd(e events.RoundEnd) {
	if a.inWarmupOrPregame() {
		return
	}
	a.captureScoreboardMVPs()
	a.tracker.markEnd(e.Winner)
}

func (a *analyser) onRoundEndOfficial(e events.RoundEndOfficial) {
	a.finalizeRoundFacts()
}

// add credits n to the side the player was on. Anything but CT and T only
// reaches the total, which cannot happen for the events that call this: a
// player has to be on a side to kill, die or deal damage.
func (s *SideCount) add(side common.Team, n int) {
	s.Total += n
	switch side {
	case common.TeamCounterTerrorists:
		s.CT += n
	case common.TeamTerrorists:
		s.T += n
	}
}

// count records one round on the side the player was playing.
func (s *SideCount) count(side common.Team) {
	s.add(side, 1)
}

// addSide credits n to the counter kept under id, which the maps hold by
// value so a player that has never been counted reads as an empty one.
func addSide(counters map[uint64]SideCount, id uint64, side common.Team, n int) {
	counter := counters[id]
	counter.add(side, n)
	counters[id] = counter
}

// add credits one round with the given number of enemy kills to its bucket.
// The top bucket takes everything from aceKills up, so it always holds the
// same rounds as ACEs, and rounds below multiKillMin fall through uncounted.
func (m *MultiKillRounds) add(kills int) {
	switch {
	case kills >= aceKills:
		m.K5++
	case kills == 4:
		m.K4++
	case kills == 3:
		m.K3++
	case kills == 2:
		m.K2++
	}
}

func (a *analyser) applyRoundOutcomeWithTiers(outcome roundOutcome, tiers map[uint64]equipTier) {
	if !outcome.played {
		return
	}
	for _, id := range outcome.aces {
		if p := a.players[id]; p != nil {
			p.PlayerMapStats.ACEs++
		}
	}
	for id, kills := range outcome.multiKills {
		if p := a.players[id]; p != nil {
			p.PlayerMapStats.MultiKills.add(kills)
		}
	}
	for id, kills := range outcome.tradeKills {
		if p := a.players[id]; p != nil {
			p.KillStats.TradeKills += kills
		}
	}
	if p := a.players[outcome.clutcher]; p != nil {
		p.PlayerMapStats.ClutchesWon++
	}
	if p := a.players[outcome.opening.killer]; p != nil {
		p.OpeningDuelStats.OpeningKills.count(outcome.opening.killerTeam)
		if outcome.openingWon {
			a.openingWins[outcome.opening.killer]++
		}
	}
	if p := a.players[outcome.opening.victim]; p != nil {
		p.OpeningDuelStats.OpeningDeaths.count(outcome.opening.victimTeam)
	}
	// The side a player counts on is the one they started the round on, so
	// that a halftime swap moves them without touching what they did before
	// it. Everything else here comes from the same round.
	for id, side := range outcome.participants {
		p := a.players[id]
		if p == nil {
			continue
		}
		p.SideStats.Rounds.count(side)
		if outcome.deathsTraded[id] {
			p.DeathsTraded.count(side)
		}
		if outcome.kast[id] {
			addSide(a.kastRounds, id, side, 1)
		}
	}
	for id, delta := range outcome.swing {
		a.roundSwing[id] += delta
	}
	// The tracker reports round facts; how much a survived or KAST round is
	// worth on the player's buy that round is the rating model's business.
	// A player with no tier snapshot weighs the neutral 1.
	for id := range outcome.survived {
		a.ecoSurvival[id] += ecoRoundWeight(tiers[id])
	}
	for id := range outcome.ratingKast {
		a.ecoKast[id] += ecoRoundWeight(tiers[id])
	}
}

// finalizeRoundFacts closes the current round and queues its derived facts.
// The queue is reconciled against the authoritative round count so a late
// property update cannot lose a genuine round and an unended setup round
// cannot consume a scored-round slot. Raw event totals deliberately bypass it.
func (a *analyser) finalizeRoundFacts() {
	outcome := a.tracker.finalize()
	if outcome.played {
		pending := &pendingRoundFacts{
			outcome:   outcome,
			tiers:     maps.Clone(a.roundTiers),
			mvps:      maps.Clone(a.roundMVPs),
			ecoKills:  slices.Clone(a.roundEcoKills),
			ecoDamage: slices.Clone(a.roundEcoDamage),
		}
		a.pendingRounds = append(a.pendingRounds, pending)
		a.latestRound = pending
	}
	a.applyScoredRoundFacts()
}

func (a *analyser) applyScoredRoundFacts() {
	if !a.roundsStarted {
		return
	}
	available := a.parser.GameState().TotalRoundsPlayed() - a.appliedRounds
	for available > 0 && len(a.pendingRounds) > 0 {
		index := a.nextScoredRound()
		pending := a.pendingRounds[index]
		a.pendingRounds = slices.Delete(a.pendingRounds, index, index+1)
		pending.scored = true
		a.applyRoundOutcomeWithTiers(pending.outcome, pending.tiers)
		for _, fact := range pending.ecoKills {
			a.ecoKills[fact.player] += fact.value
		}
		for _, fact := range pending.ecoDamage {
			a.ecoDamage[fact.player] += fact.value
		}
		a.applyScoreboardMVPs(pending.mvps)
		a.applyEventMVP(pending)
		a.appliedRounds++
		available--
	}
}

// nextScoredRound prefers rounds that received RoundEnd. If only unended
// candidates remain, the newest one wins: this is the parse-corruption
// fallback that skips an earlier same-score setup RoundStart.
func (a *analyser) nextScoredRound() int {
	for i, pending := range a.pendingRounds {
		if pending.outcome.decided {
			return i
		}
	}
	return len(a.pendingRounds) - 1
}

func (a *analyser) applyEventMVP(pending *pendingRoundFacts) {
	if !pending.scored || pending.mvpCounted || pending.outcome.mvp == 0 {
		return
	}
	pending.mvpCounted = true
	id := pending.outcome.mvp
	a.eventMVPs[id]++
	if player := a.players[id]; player != nil {
		player.PlayerMapStats.MVPs = max(player.PlayerMapStats.MVPs, a.eventMVPs[id])
	}
}

// captureScoreboardMVPs snapshots the cumulative scoreboard counter while
// leavers are still present. It is applied only if this round later proves to
// be scored; CS2 demos often omit RoundMVPAnnouncement entirely.
func (a *analyser) captureScoreboardMVPs() {
	for _, pl := range a.parser.GameState().Participants().Playing() {
		if dp := a.ensurePlayer(pl); dp != nil {
			a.roundMVPs[dp.SteamID] = max(a.roundMVPs[dp.SteamID], pl.MVPs())
		}
	}
}

func (a *analyser) applyScoreboardMVPs(mvps map[uint64]int) {
	for id, count := range mvps {
		if player := a.players[id]; player != nil {
			player.PlayerMapStats.MVPs = max(player.PlayerMapStats.MVPs, count)
		}
	}
}

// finalise fills in everything that needs the full match: final score and
// the per-player derived stats.
func (a *analyser) finalise() {
	a.captureScoreboardMVPs()
	a.finalizeRoundFacts()
	// The final scoreboard is authoritative and catches MVP entity updates
	// that arrived after RoundEndOfficial. Per-round snapshots above still
	// preserve MVPs for players who disconnected before parse end. Keep this
	// safety net behind the same scored-round gate as every other MVP fact.
	if a.latestRound != nil && a.latestRound.scored {
		a.applyScoreboardMVPs(a.roundMVPs)
	}
	a.pendingRounds = nil
	a.latestRound = nil

	gs := a.parser.GameState()
	a.mapData.TotalRounds = gs.TotalRoundsPlayed()
	a.mapData.RoundsWonCT = gs.TeamCounterTerrorists().Score()
	a.mapData.RoundsWonT = gs.TeamTerrorists().Score()

	a.derive(a.mapData.TotalRounds)
}

// perRound divides by the rounds the value was accumulated over, reporting
// 0 for a player who never played any of them.
func perRound(value, rounds int) float64 {
	if rounds == 0 {
		return 0
	}
	return float64(value) / float64(rounds)
}

// derive computes the per-player stats that need the finished match:
// precision, ADR, KAST, average enemy flash time and the side splits of ADR
// and KAST. It is separate from finalise so tests can drive it with a round
// count instead of a demo.
func (a *analyser) derive(totalRounds int) {
	for id, p := range a.players {
		if p.KillStats.Total > 0 {
			p.KillStats.Precision = float64(p.KillStats.HeadShots) / float64(p.KillStats.Total)
		}
		// The match-wide ADR and KAST both use the match's round count,
		// following the convention of HLTV-style stat sites. The rating's
		// per-round averages share that denominator, so a late joiner is
		// diluted here exactly as their ADR is.
		if totalRounds > 0 {
			p.AssistStats.ADR = perRound(p.AssistStats.DamageGiven, totalRounds)
			p.PlayerMapStats.KAST = 100 * perRound(a.kastRounds[id].Total, totalRounds)
			rounds := float64(totalRounds)
			// The approximate percentages publish the same per-round
			// values the rating normalizes, before the KAST baseline
			// division and before the swing floor can hide a negative.
			ecoKastPerRound := a.ecoKast[id] / rounds
			swingPerRound := a.roundSwing[id] / rounds
			p.PlayerMapStats.ApproxEKASTPercent = 100 * ecoKastPerRound
			p.PlayerMapStats.ApproxRoundSwingPercent = 100 * swingPerRound
			p.Rating = blendRating(ratingRound{
				killPoints: a.ecoKills[id] / rounds,
				ecoDamage:  a.ecoDamage[id] / rounds,
				survival:   a.ecoSurvival[id] / rounds,
				kast:       ecoKastPerRound,
				multiKill:  multiKillPoints(p.PlayerMapStats.MultiKills) / rounds,
				swing:      swingPerRound,
			})
		}
		// Their side splits cannot: sides swap at halftime, so there is no
		// match-wide CT or T round count that holds for every player. Each
		// side is divided by the rounds that player spent on it, which for
		// anyone who played the whole match adds back up to the same total.
		rounds := p.SideStats.Rounds
		damage, kast := a.sideDamage[id], a.kastRounds[id]
		p.SideStats.ADR = SideRate{
			CT: perRound(damage.CT, rounds.CT),
			T:  perRound(damage.T, rounds.T),
		}
		p.SideStats.KAST = SideRate{
			CT: 100 * perRound(kast.CT, rounds.CT),
			T:  100 * perRound(kast.T, rounds.T),
		}
		if kills := p.OpeningDuelStats.OpeningKills.Total; kills > 0 {
			p.OpeningDuelStats.OpeningSuccessRate = 100 * float64(a.openingWins[id]) / float64(kills)
		}
		if flashes := p.UtilityStats.EnemiesFlashed; flashes > 0 {
			p.UtilityStats.AverageEnemyFlashTimeSeconds = p.UtilityStats.EnemyFlashTimeSeconds / float64(flashes)
		}
	}
}

func GetPlayerBestWeapon(weaponsKills map[string]int) string {
	var bestWeapon string
	var bestWeaponKills int
	for weapon, kills := range weaponsKills {
		// Ties break alphabetically so the result is deterministic.
		if kills > bestWeaponKills || (kills == bestWeaponKills && bestWeapon != "" && weapon < bestWeapon) {
			bestWeapon = weapon
			bestWeaponKills = kills
		}
	}
	return bestWeapon
}

func GetPlayersName(players map[uint64]*DemoPlayer) []string {
	playerNames := make([]string, 0, len(players))
	for _, player := range players {
		playerNames = append(playerNames, player.Name)
	}
	slices.Sort(playerNames)
	return playerNames
}

func GetPlayersToAnalyse(players map[uint64]*DemoPlayer, playersToAnalyse []string) map[uint64]*DemoPlayer {
	playersToAnalyseMap := make(map[uint64]*DemoPlayer)
	for _, player := range players {
		match := slices.ContainsFunc(playersToAnalyse, func(name string) bool {
			return strings.EqualFold(name, player.Name)
		})
		if match {
			playersToAnalyseMap[player.SteamID] = player
		}
	}
	return playersToAnalyseMap
}
