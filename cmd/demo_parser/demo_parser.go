package demoparser

import (
	"fmt"
	"os"
	"slices"
	"strings"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	events "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"
)

// analyser accumulates player stats while the demo is parsed. Round-scoped
// bookkeeping (clutches, aces, trades, KAST) is delegated to roundTracker.
type analyser struct {
	parser       demoinfocs.Parser
	players      map[uint64]*DemoPlayer
	tracker      *roundTracker
	roundsPlayed map[uint64]int
	kastRounds   map[uint64]int
	mapData      MapData
	gameMode     string
}

func ProcessDemo(demoPath string) (*ProcessedDemo, error) {
	file, err := os.Open(demoPath)
	if err != nil {
		return nil, fmt.Errorf("opening demo file: %w", err)
	}
	defer file.Close()

	a := &analyser{
		parser:       demoinfocs.NewParser(file),
		players:      make(map[uint64]*DemoPlayer),
		tracker:      newRoundTracker(),
		roundsPlayed: make(map[uint64]int),
		kastRounds:   make(map[uint64]int),
	}
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
	a.parser.RegisterEventHandler(a.onKill)
	a.parser.RegisterEventHandler(a.onPlayerHurt)
	a.parser.RegisterEventHandler(a.onRoundMVP)
	a.parser.RegisterEventHandler(a.onRoundEnd)
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

func (a *analyser) onRoundStart(e events.RoundStart) {
	if a.inWarmup() {
		return
	}
	alive := make(map[uint64]common.Team)
	for _, p := range a.parser.GameState().Participants().Playing() {
		if p.Team != common.TeamTerrorists && p.Team != common.TeamCounterTerrorists {
			continue
		}
		alive[trackerID(p)] = p.Team
		a.ensurePlayer(p)
	}
	a.tracker.startRound(alive)
}

func (a *analyser) onKill(e events.Kill) {
	if a.inWarmup() || e.Victim == nil {
		return
	}

	if victim := a.ensurePlayer(e.Victim); victim != nil {
		victim.Deaths++
	}

	// Suicides and world deaths (fall damage, C4) have no killer to credit.
	suicide := e.Killer != nil && e.Killer.SteamID64 == e.Victim.SteamID64
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
			if e.IsHeadshot {
				killer.KillStats.HeadShots++
			}
			if e.Weapon != nil && e.Weapon.Type != common.EqWorld {
				killer.KillStats.WeaponsKills[e.Weapon.String()]++
			}
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

	isTrade := a.tracker.kill(killerID, trackerID(e.Victim), killerTeam, e.Victim.Team, assisterID, a.parser.CurrentTime())
	if isTrade && !teamkill {
		if killer := a.ensurePlayer(e.Killer); killer != nil {
			killer.KillStats.TradeKills++
		}
	}
}

func (a *analyser) onPlayerHurt(e events.PlayerHurt) {
	if a.inWarmup() || e.Attacker == nil || e.Player == nil {
		return
	}
	// Team damage and self damage never count towards damage given.
	if e.Attacker.SteamID64 == e.Player.SteamID64 || e.Attacker.Team == e.Player.Team {
		return
	}
	if attacker := a.ensurePlayer(e.Attacker); attacker != nil {
		attacker.AssistStats.DamageGiven += e.HealthDamageTaken
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
		a.tracker.remove(trackerID(e.Player))
	}
}

func (a *analyser) onRoundEnd(e events.RoundEnd) {
	outcome := a.tracker.endRound(e.Winner)
	if !outcome.played {
		return
	}
	for _, id := range outcome.aces {
		if p := a.players[id]; p != nil {
			p.PlayerMapStats.ACEs++
		}
	}
	if p := a.players[outcome.clutcher]; p != nil {
		p.PlayerMapStats.ClutchesWon++
	}
	for id := range outcome.participants {
		if _, isHuman := a.players[id]; !isHuman {
			continue
		}
		a.roundsPlayed[id]++
		if outcome.kast[id] {
			a.kastRounds[id]++
		}
	}
}

// finalise fills in everything that needs the full match: final score and
// the per-player derived stats (precision, ADR, KAST).
func (a *analyser) finalise() {
	gs := a.parser.GameState()
	a.mapData.TotalRounds = gs.TotalRoundsPlayed()
	a.mapData.RoundsWonCT = gs.TeamCounterTerrorists().Score()
	a.mapData.RoundsWonT = gs.TeamTerrorists().Score()

	for id, p := range a.players {
		if p.KillStats.Total > 0 {
			p.KillStats.Precision = float64(p.KillStats.HeadShots) / float64(p.KillStats.Total)
		}
		if a.mapData.TotalRounds > 0 {
			p.AssistStats.ADR = float64(p.AssistStats.DamageGiven) / float64(a.mapData.TotalRounds)
		}
		if rounds := a.roundsPlayed[id]; rounds > 0 {
			p.PlayerMapStats.KAST = 100 * float64(a.kastRounds[id]) / float64(rounds)
		}
	}
}

func GetPlayerBestWeapon(weaponsKills map[string]int) string {
	var bestWeapon string
	var bestWeaponKills int
	for weapon, kills := range weaponsKills {
		if kills > bestWeaponKills {
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
