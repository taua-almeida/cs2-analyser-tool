package dataexport

import (
	"cmp"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
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

		// Sort rows by kills so the CSV matches the table output.
		sortedPlayers := slices.SortedFunc(maps.Values(players), func(a, b *demoparser.DemoPlayer) int {
			return cmp.Compare(b.KillStats.Total, a.KillStats.Total)
		})

		csvRecords := [][]string{
			{"Name", "Kills", "Deaths", "K/D", "HS", "Assists", "Flash Assists", "Damage Given", "ADR", "KAST (%)", "Precision (%)", "Trade Kills", "Opening Kills", "Opening Deaths", "Opening Success (%)", "MVPs", "ACEs", "Clutches Won", "Best Weapon"},
		}
		for _, player := range sortedPlayers {
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
				fmt.Sprintf("%.1f", player.OpeningDuelStats.OpeningSuccess),
				fmt.Sprintf("%d", player.PlayerMapStats.MVPs),
				fmt.Sprintf("%d", player.PlayerMapStats.ACEs),
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
