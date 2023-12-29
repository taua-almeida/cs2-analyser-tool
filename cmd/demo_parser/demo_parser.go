package demoparser

import (
	"fmt"
	"os"
	"sync"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs"
	events "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/events"
)

type KillStats struct {
	Total        int
	HeadShots    int
	Precision    float64
	WeaponsKills map[string]int
}

type AssistStats struct {
	Total          int
	FlashedEnemies int
	DamageGiven    int
}

type DemoPlayer struct {
	SteamID     uint64
	OldName     string
	Name        string
	UserID      int
	Deaths      int
	KillStats   KillStats
	AssistStats AssistStats
}

func ProcessDemo(demoPath string) map[uint64]*DemoPlayer {
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
	var wg sync.WaitGroup

	wg.Add(1)
	go registerPlayers(demoParser, playerDemo, &wg)

	wg.Add(1)
	go registerKills(demoParser, playerDemo, &wg)

	wg.Add(1)
	go registerDamage(demoParser, playerDemo, &wg)

	wg.Wait()
	demoParser.ParseToEnd()

	calculateKillsPrecision(playerDemo)

	return playerDemo
}

func registerPlayers(demoParser demoinfocs.Parser, demoPlayer map[uint64]*DemoPlayer, wg *sync.WaitGroup) {
	defer wg.Done()
	demoParser.RegisterEventHandler(func(e events.PlayerConnect) {
		if !e.Player.IsBot {
			demoPlayer[e.Player.SteamID64] = &DemoPlayer{SteamID: e.Player.SteamID64, Name: e.Player.Name, UserID: e.Player.UserID}
		}
	})
}

func registerKills(demoParser demoinfocs.Parser, demoPlayer map[uint64]*DemoPlayer, wg *sync.WaitGroup) {
	defer wg.Done()
	demoParser.RegisterEventHandler(func(e events.Kill) {
		if !e.Killer.IsBot {
			killer := demoPlayer[e.Killer.SteamID64]
			killer.KillStats.Total++
			if e.IsHeadshot {
				killer.KillStats.HeadShots++
			}
			if killer.KillStats.WeaponsKills == nil {
				killer.KillStats.WeaponsKills = make(map[string]int)
			}
			killer.KillStats.WeaponsKills[e.Weapon.String()]++
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

func registerDamage(demoParser demoinfocs.Parser, demoPlayer map[uint64]*DemoPlayer, wg *sync.WaitGroup) {
	defer wg.Done()
	demoParser.RegisterEventHandler(func(e events.PlayerHurt) {
		if e.Attacker != nil && !e.Attacker.IsBot {
			attacker := demoPlayer[e.Attacker.SteamID64]
			attacker.AssistStats.DamageGiven += e.HealthDamageTaken
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

func (p DemoPlayer) String() string {
	return fmt.Sprintf("Player: %s (SteamID: %d)\nKills: %d, Deaths: %d, Headshots: %d, Precision: %.2f\n",
		p.Name, p.SteamID, p.KillStats.Total, p.Deaths, p.KillStats.HeadShots, p.KillStats.Precision)
}

func GetPlayersName(players map[uint64]*DemoPlayer) []string {
	var playerNames []string
	for _, player := range players {
		playerNames = append(playerNames, player.Name)
	}
	return playerNames
}
