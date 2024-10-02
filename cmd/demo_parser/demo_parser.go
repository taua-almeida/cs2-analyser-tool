package demoparser

import (
	"fmt"
	"os"
	"slices"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs"
	common "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/common"
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

	playerDemo := make(map[uint64]*DemoPlayer)
	mapData := &MapData{}
	gameMode := ""
	roundData := &RoundData{}

	registerMap(demoParser, mapData, gameMode)
	registerPlayers(demoParser, playerDemo)
	registerKills(demoParser, playerDemo)
	registerDamage(demoParser, playerDemo)
	registerMVP(demoParser, playerDemo)
	registerRoundEvents(demoParser, playerDemo, roundData)

	demoParser.ParseToEnd()

	calculateKillsPrecision(playerDemo)

	processedDemoData := &ProcessedDemo{
		Players:  playerDemo,
		Map:      *mapData,
		GameMode: gameMode,
	}

	return processedDemoData
}

func (p DemoPlayer) String() string {
	return fmt.Sprintf("Player: %s (SteamID: %d)\nKills: %d, Deaths: %d, Headshots: %d, Precision: %.2f\n",
		p.Name, p.SteamID, p.KillStats.Total, p.Deaths, p.KillStats.HeadShots, p.KillStats.Precision)
}

func registerMap(demoParser demoinfocs.Parser, mapData *MapData, gameMode string) {
	demoParser.RegisterNetMessageHandler(func(m *msgs2.CSVCMsg_ServerInfo) {
		mapData.MapName = m.GetMapName()
		gameSessionConfig := m.GetGameSessionConfig()
		if gameSessionConfig != nil {
			gameMode = gameSessionConfig.GetGamemode()
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
				PlayerMapStats: PlayerMapStats{
					ACEs: 0,
					MVPs: 0,
				},
			}
		}
	})
}

func registerRoundEvents(demoParser demoinfocs.Parser, demoPlayer map[uint64]*DemoPlayer, roundData *RoundData) {
	demoParser.RegisterEventHandler(func(e events.RoundStart) {
		gs := demoParser.GameState()
		*roundData = RoundData{
			RoundNumber:    gs.TotalRoundsPlayed() + 1,
			KillsByPlayer:  make(map[uint64]int),
			PlayersAliveCT: len(gs.TeamCounterTerrorists().Members()),
			PlayersAliveT:  len(gs.TeamTerrorists().Members()),
		}
	})

	demoParser.RegisterEventHandler(func(e events.Kill) {
		demoParser.CurrentTime()
		if e.Victim != nil {
			if e.Killer.Team == common.TeamTerrorists {
				roundData.PlayersAliveCT--
			} else {
				roundData.PlayersAliveT--
			}
			roundData.KillsByPlayer[e.Killer.SteamID64]++
		}
	})

	demoParser.RegisterEventHandler(func(e events.RoundEnd) {
		roundData.WinningTeam = e.Winner
		checkClutchAndAce(roundData, demoPlayer)
	})
}

func checkClutchAndAce(roundData *RoundData, demoPlayer map[uint64]*DemoPlayer) {
	for playerID, kills := range roundData.KillsByPlayer {
		if kills == 5 {
			demoPlayer[playerID].PlayerMapStats.ACEs++
		}
	}

	if len(roundData.KillsByPlayer) == 1 {
		for playerID, kills := range roundData.KillsByPlayer {
			if (roundData.PlayersAliveCT == 1 && roundData.WinningTeam == common.TeamCounterTerrorists) ||
				(roundData.PlayersAliveT == 1 && roundData.WinningTeam == common.TeamTerrorists) {
				if kills > 1 {
					demoPlayer[playerID].PlayerMapStats.ClutchesWon++
					roundData.ClutchWonByPlayer = playerID
				}
			}
		}
	}
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
			player.PlayerMapStats.MVPs++
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
