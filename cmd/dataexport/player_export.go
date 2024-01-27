package dataexport

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"time"

	demoparser "github.com/taua-almeida/cs2-analyser-tool/cmd/demo_parser"
)

func WritePlayersToFile(players map[uint64]*demoparser.DemoPlayer, saveType string) (string, error) {
	fileName := fmt.Sprintf("%d_data.%s", time.Now().Unix(), saveType)

	if saveType == "csv" {
		csvFile, err := os.Create(fileName)
		if err != nil {
			return "", err
		}
		defer csvFile.Close()
		w := csv.NewWriter(csvFile)
		csvRecords := [][]string{
			{"Name", "Kills", "Deaths", "K/D", "HS", "Assists", "Flash Assist", "Damage Given", "Precision (%)", "Best Weapon"},
		}
		for _, player := range players {
			playerBestWeapon := demoparser.GetPlayerBestWeapon(player.KillStats.WeaponsKills)
			kd := fmt.Sprintf("%.3f", float32(player.KillStats.Total)/float32(player.Deaths))
			csvRecords = append(csvRecords, []string{
				player.Name,
				fmt.Sprintf("%d", player.KillStats.Total),
				fmt.Sprintf("%d", player.Deaths),
				kd,
				fmt.Sprintf("%d", player.KillStats.HeadShots),
				fmt.Sprintf("%d", player.AssistStats.Total),
				fmt.Sprintf("%d", player.AssistStats.FlashedEnemies),
				fmt.Sprintf("%d", player.AssistStats.DamageGiven),
				fmt.Sprintf("%.2f", player.KillStats.Precision),
				playerBestWeapon,
			})
		}
		w.WriteAll(csvRecords)
		if err := w.Error(); err != nil {
			return "", err
		}
		w.Flush()
		return fileName, nil
	}
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
