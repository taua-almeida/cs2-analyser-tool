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
	"github.com/taua-almeida/cs2-analyser-tool/analysis"
)

// killsOrder is the one player ordering every output uses: most kills
// first, ties broken by name, then by SteamID — without the final unique
// key, two players sharing a name and kill count would inherit the map's
// random iteration order and reorder between runs.
func killsOrder(aKills, bKills int, aName, bName string, aID, bID uint64) int {
	if c := cmp.Compare(bKills, aKills); c != 0 {
		return c
	}
	if c := cmp.Compare(aName, bName); c != 0 {
		return c
	}
	return cmp.Compare(aID, bID)
}

// sortedByKills orders players the way the main table does.
func sortedByKills(players map[uint64]*analysis.DemoPlayer) []*analysis.DemoPlayer {
	return slices.SortedFunc(maps.Values(players), func(a, b *analysis.DemoPlayer) int {
		return killsOrder(a.KillStats.Total, b.KillStats.Total, a.Name, b.Name, a.SteamID, b.SteamID)
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
func entryScore(opening analysis.OpeningDuelStats) string {
	return fmt.Sprintf("%d:%d", opening.OpeningKills.Total, opening.OpeningDeaths.Total)
}

func PrintCLIDataTable(playerToAnalyse map[uint64]*analysis.DemoPlayer, mapData *analysis.MapData, gameMode string) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Name", "Kills", "Deaths", "K/D", "HS", "Assists", "ADR", "KAST (%)", "Rating", "Entry", "Precision (%)", "Best Weapon"})
	for _, player := range playerToAnalyse {
		playerBestWeapon := analysis.GetPlayerBestWeapon(player.KillStats.WeaponsKills)
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
func PrintCLIDetailTables(playerToAnalyse map[uint64]*analysis.DemoPlayer) {
	players := sortedByKills(playerToAnalyse)
	printRatingTable(players, os.Stdout)
	printApproxMetricsTable(players, os.Stdout)
	printMultiKillTable(players, os.Stdout)
	printTradeTable(players, os.Stdout)
	printSideSplitTable(players, os.Stdout)
	printUtilityEffectivenessTable(players, os.Stdout)
	printGrenadesThrownTable(players, os.Stdout)
}

// ratingRow is one line of the rating breakdown. A nil rating means it was
// not computed — a series aggregate without raw facts — and renders as "-"
// cells rather than a fabricated zero rating.
type ratingRow struct {
	name   string
	rating *analysis.RatingStats
}

// printRatingRows breaks the rating into its six sub-ratings, each
// normalized so 1.00 is an average performance on that axis. The KAST and
// round-swing columns are headed as sub-ratings to keep them apart from the
// approximate percentages printed below them. It is the one rating-breakdown
// shell, shared by the single-map and series detail tables.
func printRatingRows(rows []ratingRow, output io.Writer) {
	t := table.NewWriter()
	t.SetOutputMirror(output)
	t.SetTitle("Rating breakdown (1.00 = average)")
	t.AppendHeader(table.Row{"Name", "Rating", "Kills", "Damage", "Survival", "eKAST sub-rating", "Multi-kill", "Round swing sub-rating"})
	for _, row := range rows {
		cells := table.Row{row.name, "-", "-", "-", "-", "-", "-", "-"}
		if rating := row.rating; rating != nil {
			cells = table.Row{
				row.name,
				fmt.Sprintf("%.2f", rating.Value),
				fmt.Sprintf("%.2f", rating.Kills),
				fmt.Sprintf("%.2f", rating.Damage),
				fmt.Sprintf("%.2f", rating.Survival),
				fmt.Sprintf("%.2f", rating.KAST),
				fmt.Sprintf("%.2f", rating.MultiKill),
				fmt.Sprintf("%.2f", rating.RoundSwing),
			}
		}
		t.AppendRow(cells)
	}
	t.Render()
}

func printRatingTable(players []*analysis.DemoPlayer, output io.Writer) {
	rows := make([]ratingRow, len(players))
	for i, player := range players {
		rows[i] = ratingRow{name: player.Name, rating: &player.Rating}
	}
	printRatingRows(rows, output)
}

// printApproxMetricsTable shows the interpretable pre-normalization values
// behind the eKAST and round-swing sub-ratings. Both are Rating 3.0-style
// approximations: eKAST weights qualifying rounds by economy, so it can
// exceed 100%, and swing keeps its sign instead of the sub-rating's floor.
func printApproxMetricsTable(players []*analysis.DemoPlayer, output io.Writer) {
	t := table.NewWriter()
	t.SetOutputMirror(output)
	t.SetTitle("Approximate Rating 3.0 metrics")
	t.AppendHeader(table.Row{"Name", "Approx. eKAST (%)", "Approx. swing (%)"})
	for _, player := range players {
		stats := player.PlayerMapStats
		t.AppendRow(table.Row{
			player.Name,
			fmt.Sprintf("%.1f", stats.ApproxEKASTPercent),
			fmt.Sprintf("%+.1f", stats.ApproxRoundSwingPercent),
		})
	}
	t.Render()
}

// countSplit formats a side-split count as "ct / t".
func countSplit(count analysis.SideCount) string {
	return fmt.Sprintf("%d / %d", count.CT, count.T)
}

// rateSplit formats a side-split rate as "ct / t".
func rateSplit(rate analysis.SideRate) string {
	return fmt.Sprintf("%.1f / %.1f", rate.CT, rate.T)
}

// printTradeTable puts the two sides of a trade next to each other: the
// kill that avenged a teammate and the player's own deaths that were
// avenged. One kill can trade more than one death, so the totals may differ.
func printTradeTable(players []*analysis.DemoPlayer, output io.Writer) {
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
func printSideSplitTable(players []*analysis.DemoPlayer, output io.Writer) {
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
func printMultiKillTable(players []*analysis.DemoPlayer, output io.Writer) {
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

func printUtilityEffectivenessTable(players []*analysis.DemoPlayer, output io.Writer) {
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

func printGrenadesThrownTable(players []*analysis.DemoPlayer, output io.Writer) {
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
