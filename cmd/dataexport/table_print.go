package dataexport

import (
	"cmp"
	"fmt"
	"io"
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
	t.AppendHeader(table.Row{"Name", "Kills", "Deaths", "K/D", "HS", "Assists", "ADR", "KAST (%)", "Rating", "Entry", "Precision (%)", "Best Weapon"})
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
			fmt.Sprintf("%.2f", player.Rating.Value),
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
	players := sortedByKills(playerToAnalyse)
	printRatingTable(players, os.Stdout)
	printMultiKillTable(players, os.Stdout)
	printTradeTable(players, os.Stdout)
	printSideSplitTable(players, os.Stdout)
	printUtilityEffectivenessTable(players, os.Stdout)
	printGrenadesThrownTable(players, os.Stdout)
}

// printRatingTable breaks the rating into its six sub-ratings, each
// normalized so 1.00 is an average performance on that axis.
func printRatingTable(players []*demoparser.DemoPlayer, output io.Writer) {
	t := table.NewWriter()
	t.SetOutputMirror(output)
	t.SetTitle("Rating breakdown (1.00 = average)")
	t.AppendHeader(table.Row{"Name", "Rating", "Kills", "Damage", "Survival", "KAST", "Multi-kill", "Round swing"})
	for _, player := range players {
		rating := player.Rating
		t.AppendRow(table.Row{
			player.Name,
			fmt.Sprintf("%.2f", rating.Value),
			fmt.Sprintf("%.2f", rating.Kills),
			fmt.Sprintf("%.2f", rating.Damage),
			fmt.Sprintf("%.2f", rating.Survival),
			fmt.Sprintf("%.2f", rating.KAST),
			fmt.Sprintf("%.2f", rating.MultiKill),
			fmt.Sprintf("%.2f", rating.RoundSwing),
		})
	}
	t.Render()
}

// countSplit formats a side-split count as "ct / t".
func countSplit(count demoparser.SideCount) string {
	return fmt.Sprintf("%d / %d", count.CT, count.T)
}

// rateSplit formats a side-split rate as "ct / t".
func rateSplit(rate demoparser.SideRate) string {
	return fmt.Sprintf("%.1f / %.1f", rate.CT, rate.T)
}

// printTradeTable puts the two sides of a trade next to each other: the
// kill that avenged a teammate and the player's own deaths that were
// avenged. One kill can trade more than one death, so the totals may differ.
func printTradeTable(players []*demoparser.DemoPlayer, output io.Writer) {
	t := table.NewWriter()
	t.SetOutputMirror(output)
	t.SetTitle("Trade stats")
	t.AppendHeader(table.Row{"Name", "Trade kills", "Deaths traded", "Deaths traded (CT / T)"})
	for _, player := range players {
		t.AppendRow(table.Row{
			player.Name,
			player.KillStats.TradeKills,
			player.DeathsTraded.Total,
			countSplit(player.DeathsTraded),
		})
	}
	t.Render()
}

// printSideSplitTable lists the core stats split by side. Rounds are in
// there because per-side ADR and KAST are divided by them, which makes a
// lopsided split worth seeing next to the rates it produced.
func printSideSplitTable(players []*demoparser.DemoPlayer, output io.Writer) {
	t := table.NewWriter()
	t.SetOutputMirror(output)
	t.SetTitle("Side splits (CT / T)")
	t.AppendHeader(table.Row{"Name", "Rounds", "Kills", "Deaths", "ADR", "KAST (%)"})
	for _, player := range players {
		side := player.SideStats
		t.AppendRow(table.Row{
			player.Name,
			countSplit(side.Rounds),
			countSplit(side.Kills),
			countSplit(side.Deaths),
			rateSplit(side.ADR),
			rateSplit(side.KAST),
		})
	}
	t.Render()
}

// printMultiKillTable lists the multi-kill rounds per player. The buckets
// are exclusive, so each round shows up in exactly one column.
func printMultiKillTable(players []*demoparser.DemoPlayer, output io.Writer) {
	t := table.NewWriter()
	t.SetOutputMirror(output)
	t.SetTitle("Multi-kill rounds")
	t.AppendHeader(table.Row{"Name", "2K", "3K", "4K", "5K"})
	for _, player := range players {
		multiKills := player.PlayerMapStats.MultiKills
		t.AppendRow(table.Row{player.Name, multiKills.K2, multiKills.K3, multiKills.K4, multiKills.K5})
	}
	t.Render()
}

func printUtilityEffectivenessTable(players []*demoparser.DemoPlayer, output io.Writer) {
	t := table.NewWriter()
	t.SetOutputMirror(output)
	t.SetTitle("Utility effectiveness")
	t.AppendHeader(table.Row{"Name", "Flash assists", "Enemies flashed", "Friends flashed", "Enemy time (s)", "Avg enemy time (s)", "Damage", "HE", "Fire", "Unused value"})
	for _, player := range players {
		utility := player.UtilityStats
		t.AppendRow(table.Row{
			player.Name,
			player.AssistStats.FlashedEnemies,
			utility.EnemiesFlashed,
			utility.FriendsFlashed,
			fmt.Sprintf("%.2f", utility.EnemyFlashTimeSeconds),
			fmt.Sprintf("%.2f", utility.AverageEnemyFlashTimeSeconds),
			utility.UtilityDamage.Total,
			utility.UtilityDamage.HE,
			utility.UtilityDamage.Fire,
			utility.UnusedUtilityValue,
		})
	}
	t.Render()
}

func printGrenadesThrownTable(players []*demoparser.DemoPlayer, output io.Writer) {
	t := table.NewWriter()
	t.SetOutputMirror(output)
	t.SetTitle("Grenades thrown")
	t.AppendHeader(table.Row{"Name", "Total", "Flash", "Smoke", "HE", "Molotov", "Incendiary", "Decoy"})
	for _, player := range players {
		grenades := player.UtilityStats.GrenadesThrown
		t.AppendRow(table.Row{
			player.Name,
			grenades.Total,
			grenades.Flash,
			grenades.Smoke,
			grenades.HE,
			grenades.Molotov,
			grenades.Incendiary,
			grenades.Decoy,
		})
	}
	t.Render()
}
