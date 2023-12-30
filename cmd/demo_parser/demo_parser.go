package demoparser

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs"
	events "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/events"
)

type KillStats struct {
	Total        int            `json:"total"`
	HeadShots    int            `json:"headshots"`
	Precision    float64        `json:"precision"`
	WeaponsKills map[string]int `json:"weapons_kills"`
}

type AssistStats struct {
	Total          int `json:"total"`
	FlashedEnemies int `json:"flashed_enemies"`
	DamageGiven    int `json:"damage_given"`
}

type DemoPlayer struct {
	SteamID     uint64      `json:"steam_id"`
	Name        string      `json:"name"`
	UserID      int         `json:"user_id"`
	Deaths      int         `json:"deaths"`
	KillStats   KillStats   `json:"kill_stats"`
	AssistStats AssistStats `json:"assist_stats"`
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
	var mu sync.Mutex // Mutex for safe map access

	// Array of functions for concurrent execution
	functions := []func(){
		func() { registerPlayers(demoParser, playerDemo, &wg, &mu) },
		func() { registerKills(demoParser, playerDemo, &wg, &mu) },
		func() { registerDamage(demoParser, playerDemo, &wg, &mu) },
	}

	for _, f := range functions {
		wg.Add(1)
		go f()
	}

	wg.Wait()
	demoParser.ParseToEnd()

	calculateKillsPrecision(playerDemo)

	return playerDemo
}

func registerPlayers(demoParser demoinfocs.Parser, demoPlayer map[uint64]*DemoPlayer, wg *sync.WaitGroup, mu *sync.Mutex) {
	defer wg.Done()
	demoParser.RegisterEventHandler(func(e events.PlayerConnect) {
		if !e.Player.IsBot {
			mu.Lock() // Lock the mutex before modifying the map
			demoPlayer[e.Player.SteamID64] = &DemoPlayer{
				SteamID:   e.Player.SteamID64,
				Name:      e.Player.Name,
				UserID:    e.Player.UserID,
				KillStats: KillStats{WeaponsKills: make(map[string]int)},
			}
			mu.Unlock() // Unlock the mutex after the map is modified
		}
	})
}

func registerKills(demoParser demoinfocs.Parser, demoPlayer map[uint64]*DemoPlayer, wg *sync.WaitGroup, mu *sync.Mutex) {
	defer wg.Done()
	demoParser.RegisterEventHandler(func(e events.Kill) {
		mu.Lock() // Lock before accessing the map
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
		mu.Unlock() // Unlock after modifying the map
	})
}

func registerDamage(demoParser demoinfocs.Parser, demoPlayer map[uint64]*DemoPlayer, wg *sync.WaitGroup, mu *sync.Mutex) {
	defer wg.Done()
	demoParser.RegisterEventHandler(func(e events.PlayerHurt) {
		if e.Attacker != nil && !e.Attacker.IsBot {
			mu.Lock()
			attacker := demoPlayer[e.Attacker.SteamID64]
			attacker.AssistStats.DamageGiven += e.HealthDamageTaken
			mu.Unlock()
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

func WritePlayersToFile(players map[uint64]*DemoPlayer) (string, error) {
	fileName := fmt.Sprintf("%d_data.json", time.Now().Unix()) // Save in the current working directory

	jsonData, err := json.MarshalIndent(players, "", " ")
	if err != nil {
		return "", err
	}

	err = os.WriteFile(fileName, jsonData, 0644)
	if err != nil {
		return "", err
	}

	return fileName, nil
}
