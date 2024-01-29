package demoparser

import (
	"fmt"
	"os"
	"slices"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs"
	events "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/msgs2"
)

func ProcessDemo(demoPath string) *ProcessedDemo {
	file, err := os.Open(demoPath)
	if err != nil {
		fmt.Println("Error opening demo file", err)
		return nil
	}
	defer file.Close()

	demoParser := demoinfocs.NewParser(file)
	defer demoParser.Close()

	if err != nil {
		fmt.Println("Error parsing demo file", err)
		return nil
	}

	playerDemo := make(map[uint64]*DemoPlayer)
	gameData := &DemoGame{}

	getDemoNetData(demoParser, gameData)
	registerPlayers(demoParser, playerDemo)
	registerKills(demoParser, playerDemo)
	registerDamage(demoParser, playerDemo)
	registerMVP(demoParser, playerDemo)
	registerRoundData(demoParser, playerDemo)

	demoParser.ParseToEnd()

	calculateKillsPrecision(playerDemo)

	processedDemoData := &ProcessedDemo{
		Players: playerDemo,
		Game:    *gameData,
	}

	return processedDemoData
}

func (p DemoPlayer) String() string {
	return fmt.Sprintf("Player: %s (SteamID: %d)\nKills: %d, Deaths: %d, Headshots: %d, Precision: %.2f\n",
		p.Name, p.SteamID, p.KillStats.Total, p.Deaths, p.KillStats.HeadShots, p.KillStats.Precision)
}

func getDemoNetData(demoParser demoinfocs.Parser, gameData *DemoGame) {
	demoParser.RegisterNetMessageHandler(func(m *msgs2.CSVCMsg_ServerInfo) {
		gameData.MapName = m.GetMapName()
		gameSessionConfig := m.GetGameSessionConfig()
		if gameSessionConfig != nil {
			gameData.GameMode = gameSessionConfig.GetGamemode()
		}
	})
}

func registerPlayers(demoParser demoinfocs.Parser, demoPlayer map[uint64]*DemoPlayer) {
	demoParser.RegisterEventHandler(func(e events.PlayerConnect) {
		if !e.Player.IsBot {
			demoPlayer[e.Player.SteamID64] = &DemoPlayer{
				SteamID:   e.Player.SteamID64,
				Name:      e.Player.Name,
				UserID:    e.Player.UserID,
				KillStats: KillStats{WeaponsKills: make(map[string]int)},
				MapStats: MapStats{
					RoundsWon:  0,
					RoundsLost: 0,
					MVPs:       0,
				},
			}
		}
	})
}

func registerKills(demoParser demoinfocs.Parser, demoPlayer map[uint64]*DemoPlayer) {
	demoParser.RegisterEventHandler(func(e events.Kill) {
		if !e.Killer.IsBot {
			killer := demoPlayer[e.Killer.SteamID64]
			killer.KillStats.Total++
			if e.IsHeadshot {
				killer.KillStats.HeadShots++
			}
			if e.Weapon.String() != "World" {
				killer.KillStats.WeaponsKills[e.Weapon.String()]++
			}
		}
		if e.Assister != nil && !e.Assister.IsBot {
			assister := demoPlayer[e.Assister.SteamID64]
			assister.AssistStats.Total++
			if e.Victim.FlashDuration > 0 {
				assister.AssistStats.FlashedEnemies++
			}
		}
		if !e.Victim.IsBot {
			victim := demoPlayer[e.Victim.SteamID64]
			victim.Deaths++
		}
	})
}

func registerDamage(demoParser demoinfocs.Parser, demoPlayer map[uint64]*DemoPlayer) {
	demoParser.RegisterEventHandler(func(e events.PlayerHurt) {
		if e.Attacker != nil && !e.Attacker.IsBot {

			attacker := demoPlayer[e.Attacker.SteamID64]
			attacker.AssistStats.DamageGiven += e.HealthDamageTaken

		}
	})
}

func registerMVP(demoParser demoinfocs.Parser, demoPlayer map[uint64]*DemoPlayer) {
	demoParser.RegisterEventHandler(func(e events.RoundMVPAnnouncement) {
		if !e.Player.IsBot {
			player := demoPlayer[e.Player.SteamID64]
			player.MapStats.MVPs++
		}
	})
}

func registerRoundData(demoParser demoinfocs.Parser, demoPlayer map[uint64]*DemoPlayer) {
	demoParser.RegisterEventHandler(func(e events.RoundEnd) {
		if e.WinnerState != nil {
			winners := e.WinnerState.Members()
			for _, player := range winners {
				demoPlayer[player.SteamID64].MapStats.RoundsWon++
			}
		}
		if e.LoserState != nil {
			losers := e.LoserState.Members()
			for _, player := range losers {
				demoPlayer[player.SteamID64].MapStats.RoundsLost++
			}
		}
	})
}

func calculateKillsPrecision(players map[uint64]*DemoPlayer) {
	for _, player := range players {
		if player.KillStats.Total > 0 {
			player.KillStats.Precision = float64(player.KillStats.HeadShots) / float64(player.KillStats.Total)
		} else {
			player.KillStats.Precision = 0
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
	var playerNames []string
	for _, player := range players {
		playerNames = append(playerNames, player.Name)
	}
	return playerNames
}

func GetPlayersToAnalyse(players map[uint64]*DemoPlayer, playersToAnalyse []string) map[uint64]*DemoPlayer {
	var playersToAnalyseMap = make(map[uint64]*DemoPlayer)
	for _, player := range players {
		foundPlayer := slices.Index(playersToAnalyse, player.Name)
		if foundPlayer != -1 {
			playersToAnalyseMap[player.SteamID] = player
		}
	}
	return playersToAnalyseMap
}
