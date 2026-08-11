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
			{"Name", "Kills", "Deaths", "K/D", "HS", "Assists", "Flash Assists", "Damage Given", "ADR", "KAST (%)", "Precision (%)", "Trade Kills", "Deaths Traded", "Opening Kills", "Opening Deaths", "Opening Success (%)", "MVPs", "ACEs", "2K", "3K", "4K", "5K", "Clutches Won", "Rounds CT", "Rounds T", "Kills CT", "Kills T", "Deaths CT", "Deaths T", "Deaths Traded CT", "Deaths Traded T", "ADR CT", "ADR T", "KAST CT (%)", "KAST T (%)", "Best Weapon", "Enemies Flashed", "Friends Flashed", "Enemy Flash Time (s)", "Average Enemy Flash Time (s)", "Utility Damage Total", "HE Utility Damage", "Fire Utility Damage", "Grenades Thrown Total", "Flashbangs Thrown", "Smokes Thrown", "HE Grenades Thrown", "Molotovs Thrown", "Incendiaries Thrown", "Decoys Thrown", "Unused Utility Value", "Rating", "Rating Kills", "Rating Damage", "Rating Survival", "Rating KAST", "Rating Multi-kill", "Rating Round Swing"},
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
				fmt.Sprintf("%d", player.DeathsTraded.Total),
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
				fmt.Sprintf("%d", player.SideStats.Rounds.CT),
				fmt.Sprintf("%d", player.SideStats.Rounds.T),
				fmt.Sprintf("%d", player.SideStats.Kills.CT),
				fmt.Sprintf("%d", player.SideStats.Kills.T),
				fmt.Sprintf("%d", player.SideStats.Deaths.CT),
				fmt.Sprintf("%d", player.SideStats.Deaths.T),
				fmt.Sprintf("%d", player.DeathsTraded.CT),
				fmt.Sprintf("%d", player.DeathsTraded.T),
				fmt.Sprintf("%.1f", player.SideStats.ADR.CT),
				fmt.Sprintf("%.1f", player.SideStats.ADR.T),
				fmt.Sprintf("%.1f", player.SideStats.KAST.CT),
				fmt.Sprintf("%.1f", player.SideStats.KAST.T),
				demoparser.GetPlayerBestWeapon(player.KillStats.WeaponsKills),
				fmt.Sprintf("%d", player.UtilityStats.EnemiesFlashed),
				fmt.Sprintf("%d", player.UtilityStats.FriendsFlashed),
				fmt.Sprintf("%.1f", player.UtilityStats.EnemyFlashTimeSeconds),
				fmt.Sprintf("%.1f", player.UtilityStats.AverageEnemyFlashTimeSeconds),
				fmt.Sprintf("%d", player.UtilityStats.UtilityDamage.Total),
				fmt.Sprintf("%d", player.UtilityStats.UtilityDamage.HE),
				fmt.Sprintf("%d", player.UtilityStats.UtilityDamage.Fire),
				fmt.Sprintf("%d", player.UtilityStats.GrenadesThrown.Total),
				fmt.Sprintf("%d", player.UtilityStats.GrenadesThrown.Flash),
				fmt.Sprintf("%d", player.UtilityStats.GrenadesThrown.Smoke),
				fmt.Sprintf("%d", player.UtilityStats.GrenadesThrown.HE),
				fmt.Sprintf("%d", player.UtilityStats.GrenadesThrown.Molotov),
				fmt.Sprintf("%d", player.UtilityStats.GrenadesThrown.Incendiary),
				fmt.Sprintf("%d", player.UtilityStats.GrenadesThrown.Decoy),
				fmt.Sprintf("%d", player.UtilityStats.UnusedUtilityValue),
				fmt.Sprintf("%.2f", player.Rating.Value),
				fmt.Sprintf("%.2f", player.Rating.Kills),
				fmt.Sprintf("%.2f", player.Rating.Damage),
				fmt.Sprintf("%.2f", player.Rating.Survival),
				fmt.Sprintf("%.2f", player.Rating.KAST),
				fmt.Sprintf("%.2f", player.Rating.MultiKill),
				fmt.Sprintf("%.2f", player.Rating.RoundSwing),
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
