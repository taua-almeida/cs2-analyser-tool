package demoparser

import (
	"fmt"
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
	parser      demoinfocs.Parser
	players     map[uint64]*DemoPlayer
	tracker     *roundTracker
	kastRounds  map[uint64]SideCount // rounds with KAST, total and per side
	sideDamage  map[uint64]SideCount // damage given, total and per side
	openingWins map[uint64]int       // rounds won after taking the opening kill
	lastHealth  map[uint64]int
	flashEnds   map[uint64]time.Duration // latest known end of each player's blind interval
	ecoKills    map[uint64]float64       // eco-adjusted kill points
	ecoDamage   map[uint64]float64       // eco-adjusted damage given
	roundSwing  map[uint64]float64       // summed round-win-probability swing
	ecoSurvival map[uint64]float64       // eco-weighted rounds survived
	ecoKast     map[uint64]float64       // eco-weighted rounds with rating KAST credit
	mapData     MapData
	gameMode    string
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
		flashEnds:   make(map[uint64]time.Duration),
		ecoKills:    make(map[uint64]float64),
		ecoDamage:   make(map[uint64]float64),
		roundSwing:  make(map[uint64]float64),
		ecoSurvival: make(map[uint64]float64),
		ecoKast:     make(map[uint64]float64),
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

// ensurePlayer returns the stats entry for p, creating it on first sight.
// Lazy creation covers demos where recording started after players
// connected, so no PlayerConnect event is ever seen for them.
func (a *analyser) ensurePlayer(p *common.Player) *DemoPlayer {
	if p == nil || p.IsBot || p.SteamID64 == 0 {
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

func (a *analyser) inWarmup() bool {
	return a.parser.GameState().IsWarmupPeriod()
}

// playingRoster reads the players currently on a side into the shape the
// round tracker takes, registering anyone seen for the first time and
// seeding the health their damage is measured against.
func (a *analyser) playingRoster() map[uint64]common.Team {
	roster := make(map[uint64]common.Team)
	for _, p := range a.parser.GameState().Participants().Playing() {
		if p.Team != common.TeamTerrorists && p.Team != common.TeamCounterTerrorists {
			continue
		}
		roster[trackerID(p)] = p.Team
		a.lastHealth[trackerID(p)] = p.Health()
		a.ensurePlayer(p)
	}
	return roster
}

func (a *analyser) onRoundStart(e events.RoundStart) {
	if a.inWarmup() {
		return
	}
	// Close the previous round in case its official end was never seen.
	a.applyRoundOutcome(a.tracker.finalize())
	a.tracker.startRound(a.playingRoster())
	a.captureTiers()
}

// onRoundFreezetimeEnd folds the players who picked a side during freeze
// time into the round. RoundStart snapshots the roster before that window
// opens, so without this they play a round that counts them nowhere: their
// kills, deaths and damage land on a side whose round count never moved.
func (a *analyser) onRoundFreezetimeEnd(e events.RoundFreezetimeEnd) {
	if a.inWarmup() {
		return
	}
	a.tracker.joinRound(a.playingRoster())
	a.captureTiers()
}

// captureTiers snapshots every playing player's loadout tier into the round
// tracker. It runs after the tracker opens or rejoins a round, because
// startRound clears the previous round's tiers, and again when freeze time
// ends so the tier reflects what was actually bought.
func (a *analyser) captureTiers() {
	for _, p := range a.parser.GameState().Participants().Playing() {
		if p.Team != common.TeamTerrorists && p.Team != common.TeamCounterTerrorists {
			continue
		}
		a.tracker.setTier(trackerID(p), playerTier(p.Inventory))
	}
}

// onBombPlanted feeds the round tracker the bomb state its round-swing
// win-probability model conditions on.
func (a *analyser) onBombPlanted(e events.BombPlanted) {
	if a.inWarmup() {
		return
	}
	a.tracker.plantBomb()
}

func (a *analyser) onKill(e events.Kill) {
	if a.inWarmup() || e.Victim == nil {
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

	if victim := a.ensurePlayer(e.Victim); victim != nil {
		victim.Deaths++
		victim.SideStats.Deaths.count(e.Victim.Team)
		// A World cleanup after the round is decided is not a player's
		// decision to die with utility. Combat, suicide, fall, bomb and other
		// post-round deaths still contribute.
		if !a.tracker.isPostRoundWorldCleanup(byWorld) {
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
			// which TestProcessDemoGolden pins for unused utility.
			a.ecoKills[killerID] += killPoints(playerTier(e.Killer.Inventory), playerTier(e.Victim.Inventory))
		}
	}

	var assisterID uint64
	if !teamkill && e.Assister != nil && e.Assister.Team != e.Victim.Team {
		assisterID = trackerID(e.Assister)
		if assister := a.ensurePlayer(e.Assister); assister != nil {
			assister.AssistStats.Total++
			if e.AssistedFlash {
				assister.AssistStats.FlashedEnemies++
			}
		}
	}

	isTrade := a.tracker.kill(killerID, trackerID(e.Victim), killerTeam, e.Victim.Team, assisterID, byWorld, a.parser.CurrentTime())
	if isTrade && !teamkill {
		if killer := a.ensurePlayer(e.Killer); killer != nil {
			killer.KillStats.TradeKills++
		}
	}
}

func (a *analyser) onPlayerHurt(e events.PlayerHurt) {
	if a.inWarmup() || e.Player == nil {
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

	if e.Attacker == nil {
		return
	}
	// Team damage and self damage never count towards damage given, and
	// following HLTV convention neither does bomb damage (when the game
	// credits the explosion to the planter).
	if e.Attacker.SteamID64 == e.Player.SteamID64 || e.Attacker.Team == e.Player.Team {
		return
	}
	if e.Weapon != nil && e.Weapon.Type == common.EqBomb {
		return
	}
	if attacker := a.ensurePlayer(e.Attacker); attacker != nil {
		attacker.AssistStats.DamageGiven += realDamage
		addSide(a.sideDamage, attacker.SteamID, e.Attacker.Team, realDamage)
		if e.Weapon != nil {
			attacker.UtilityStats.UtilityDamage.add(e.Weapon.Type, realDamage)
		}
		a.ecoDamage[attacker.SteamID] += float64(realDamage) *
			ecoDuelFactor(playerTier(e.Attacker.Inventory), playerTier(e.Player.Inventory))
		a.tracker.damage(trackerID(e.Attacker), victimID, realDamage)
	}
}

func (a *analyser) onPlayerFlashed(e events.PlayerFlashed) {
	if a.inWarmup() || e.Player == nil {
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
	if a.inWarmup() || e.Projectile == nil {
		return
	}
	if thrower := a.ensurePlayer(e.Projectile.Thrower); thrower != nil {
		thrower.UtilityStats.GrenadesThrown.add(projectileGrenadeType(e.Projectile))
	}
}

func (a *analyser) onRoundMVP(e events.RoundMVPAnnouncement) {
	if a.inWarmup() {
		return
	}
	if player := a.ensurePlayer(e.Player); player != nil {
		player.PlayerMapStats.MVPs++
	}
}

func (a *analyser) onDisconnect(e events.PlayerDisconnected) {
	if e.Player != nil {
		a.tracker.disconnect(trackerID(e.Player))
	}
}

func (a *analyser) onRoundEnd(e events.RoundEnd) {
	a.syncScoreboardMVPs()
	a.tracker.markEnd(e.Winner)
}

func (a *analyser) onRoundEndOfficial(e events.RoundEndOfficial) {
	a.applyRoundOutcome(a.tracker.finalize())
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

func (a *analyser) applyRoundOutcome(outcome roundOutcome) {
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
	for id, weight := range outcome.ecoSurvival {
		a.ecoSurvival[id] += weight
	}
	for id, weight := range outcome.ratingKast {
		a.ecoKast[id] += weight
	}
}

// syncScoreboardMVPs mirrors the scoreboard MVP counter into the player
// stats. CS2 demos no longer carry the round_mvp game event, so the
// RoundMVPAnnouncement handler never fires and the entity property is the
// only reliable source. Synced every round end so leavers keep theirs.
func (a *analyser) syncScoreboardMVPs() {
	for _, pl := range a.parser.GameState().Participants().Playing() {
		if dp := a.ensurePlayer(pl); dp != nil {
			dp.PlayerMapStats.MVPs = max(dp.PlayerMapStats.MVPs, pl.MVPs())
		}
	}
}

// finalise fills in everything that needs the full match: final score and
// the per-player derived stats.
func (a *analyser) finalise() {
	a.applyRoundOutcome(a.tracker.finalize())
	a.syncScoreboardMVPs()

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
			p.Rating = blendRating(ratingRound{
				killPoints: a.ecoKills[id] / rounds,
				ecoDamage:  a.ecoDamage[id] / rounds,
				survival:   a.ecoSurvival[id] / rounds,
				kast:       a.ecoKast[id] / rounds,
				multiKill:  multiKillPoints(p.PlayerMapStats.MultiKills) / rounds,
				swing:      a.roundSwing[id] / rounds,
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
