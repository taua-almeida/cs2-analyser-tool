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

		csvRecords := [][]string{
			{"Name", "Kills", "Deaths", "K/D", "HS", "Assists", "Flash Assists", "Damage Given", "ADR", "KAST (%)", "Precision (%)", "Trade Kills", "Opening Kills", "Opening Deaths", "Opening Success (%)", "MVPs", "ACEs", "2K", "3K", "4K", "5K", "Clutches Won", "Best Weapon"},
		}
		for _, player := range sortedByKills(players) {
			csvRecords = append(csvRecords, []string{
				player.Name,
				fmt.Sprintf("%d", player.KillStats.Total),
				fmt.Sprintf("%d", player.Deaths),
				kdRatio(player.KillStats.Total, player.Deaths),
				fmt.Sprintf("%d", player.KillStats.HeadShots),
				fmt.Sprintf("%d", player.AssistStats.Total),
				fmt.Sprintf("%d", player.AssistStats.FlashedEnemies),
				fmt.Sprintf("%d", player.AssistStats.DamageGiven),
				fmt.Sprintf("%.1f", player.AssistStats.ADR),
				fmt.Sprintf("%.1f", player.PlayerMapStats.KAST),
				fmt.Sprintf("%.1f", player.KillStats.Precision*100),
				fmt.Sprintf("%d", player.KillStats.TradeKills),
				fmt.Sprintf("%d", player.OpeningDuelStats.OpeningKills.Total),
				fmt.Sprintf("%d", player.OpeningDuelStats.OpeningDeaths.Total),
				fmt.Sprintf("%.1f", player.OpeningDuelStats.OpeningSuccessRate),
				fmt.Sprintf("%d", player.PlayerMapStats.MVPs),
				fmt.Sprintf("%d", player.PlayerMapStats.ACEs),
				fmt.Sprintf("%d", player.PlayerMapStats.MultiKills.K2),
				fmt.Sprintf("%d", player.PlayerMapStats.MultiKills.K3),
				fmt.Sprintf("%d", player.PlayerMapStats.MultiKills.K4),
				fmt.Sprintf("%d", player.PlayerMapStats.MultiKills.K5),
				fmt.Sprintf("%d", player.PlayerMapStats.ClutchesWon),
				demoparser.GetPlayerBestWeapon(player.KillStats.WeaponsKills),
			})
		}

		w := csv.NewWriter(csvFile)
		if err := w.WriteAll(csvRecords); err != nil {
			return "", err
		}
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
