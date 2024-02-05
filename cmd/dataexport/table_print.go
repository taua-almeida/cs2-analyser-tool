package dataexport

import (
	"fmt"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	demoparser "github.com/taua-almeida/cs2-analyser-tool/cmd/demo_parser"
)

func PrintCLIDataTable(playerToAnalyse map[uint64]*demoparser.DemoPlayer, mapData *demoparser.MapData, gameMode string) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Name", "Kills", "Deaths", "K/D", "HS", "Assists", "Damage Given", "Precision (%)", "Best Weapon"})
	for _, player := range playerToAnalyse {
		playerBestWeapon := demoparser.GetPlayerBestWeapon(player.KillStats.WeaponsKills)
		kd := fmt.Sprintf("%.3f", float32(player.KillStats.Total)/float32(player.Deaths))
		t.AppendRow(table.Row{
			player.Name,
			player.KillStats.Total,
			player.Deaths,
			kd,
			player.KillStats.HeadShots,
			player.AssistStats.Total,
			player.AssistStats.DamageGiven,
			int(player.KillStats.Precision * 100),
			playerBestWeapon,
		})
	}
	t.SortBy([]table.SortBy{{Name: "Kills", Mode: table.DscNumeric}})
	t.AppendFooter(table.Row{"Map Played", mapData.MapName})
	t.SetCaption(fmt.Sprintf("This is a demo of a: %s, game\n", strings.ToUpper(gameMode)))
	t.Render()
}
