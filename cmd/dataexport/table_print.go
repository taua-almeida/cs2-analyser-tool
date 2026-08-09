package dataexport

import (
	"fmt"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	demoparser "github.com/taua-almeida/cs2-analyser-tool/cmd/demo_parser"
)

// kdRatio formats kills/deaths, treating zero deaths as one so the ratio
// stays finite.
func kdRatio(kills, deaths int) string {
	if deaths == 0 {
		deaths = 1
	}
	return fmt.Sprintf("%.3f", float64(kills)/float64(deaths))
}

func PrintCLIDataTable(playerToAnalyse map[uint64]*demoparser.DemoPlayer, mapData *demoparser.MapData, gameMode string) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Name", "Kills", "Deaths", "K/D", "HS", "Assists", "ADR", "KAST (%)", "Precision (%)", "Best Weapon"})
	for _, player := range playerToAnalyse {
		playerBestWeapon := demoparser.GetPlayerBestWeapon(player.KillStats.WeaponsKills)
		t.AppendRow(table.Row{
			player.Name,
			player.KillStats.Total,
			player.Deaths,
			kdRatio(player.KillStats.Total, player.Deaths),
			player.KillStats.HeadShots,
			player.AssistStats.Total,
			fmt.Sprintf("%.1f", player.AssistStats.ADR),
			fmt.Sprintf("%.1f", player.PlayerMapStats.KAST),
			fmt.Sprintf("%.1f", player.KillStats.Precision*100),
			playerBestWeapon,
		})
	}
	t.SortBy([]table.SortBy{{Name: "Kills", Mode: table.DscNumeric}})
	t.AppendFooter(table.Row{"Map Played", mapData.MapName})
	t.AppendFooter(table.Row{"Score CT : T", fmt.Sprintf("%d : %d", mapData.RoundsWonCT, mapData.RoundsWonT)})
	if gameMode != "" {
		t.SetCaption("This is a demo of a: %s game\n", strings.ToUpper(gameMode))
	}
	t.Render()
}
