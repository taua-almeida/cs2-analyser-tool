package dataexport

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	demoparser "github.com/taua-almeida/cs2-analyser-tool/cmd/demo_parser"
)

// sortedByKills orders players the way the main table does, most kills
// first, so every output lists them in the same order. Ties break by name
// to keep that order stable across runs.
func sortedByKills(players map[uint64]*demoparser.DemoPlayer) []*demoparser.DemoPlayer {
	return slices.SortedFunc(maps.Values(players), func(a, b *demoparser.DemoPlayer) int {
		if c := cmp.Compare(b.KillStats.Total, a.KillStats.Total); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
}

// kdRatio formats kills/deaths, treating zero deaths as one so the ratio
// stays finite.
func kdRatio(kills, deaths int) string {
	if deaths == 0 {
		deaths = 1
	}
	return fmt.Sprintf("%.3f", float64(kills)/float64(deaths))
}

// entryScore formats opening duels as kills:deaths, e.g. "5:3".
func entryScore(opening demoparser.OpeningDuelStats) string {
	return fmt.Sprintf("%d:%d", opening.OpeningKills.Total, opening.OpeningDeaths.Total)
}

func PrintCLIDataTable(playerToAnalyse map[uint64]*demoparser.DemoPlayer, mapData *demoparser.MapData, gameMode string) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Name", "Kills", "Deaths", "K/D", "HS", "Assists", "ADR", "KAST (%)", "Entry", "Precision (%)", "Best Weapon"})
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
			entryScore(player.OpeningDuelStats),
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

// PrintCLIDetailTables prints the stats that do not fit the main table,
// each in its own narrow table. Only shown with --details.
func PrintCLIDetailTables(playerToAnalyse map[uint64]*demoparser.DemoPlayer) {
	printMultiKillTable(playerToAnalyse)
}

// printMultiKillTable lists the multi-kill rounds per player. The buckets
// are exclusive, so each round shows up in exactly one column.
func printMultiKillTable(playerToAnalyse map[uint64]*demoparser.DemoPlayer) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetTitle("Multi-kill rounds")
	t.AppendHeader(table.Row{"Name", "2K", "3K", "4K", "5K"})
	for _, player := range sortedByKills(playerToAnalyse) {
		multiKills := player.PlayerMapStats.MultiKills
		t.AppendRow(table.Row{player.Name, multiKills.K2, multiKills.K3, multiKills.K4, multiKills.K5})
	}
	t.Render()
}
