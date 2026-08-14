package dataexport

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/taua-almeida/cs2-analyser-tool/analysis"
)

// seriesTeamLabels labels the two series teams for tables, keyed by series
// team ID. A team the demos never named falls back to its series-local ID.
// Collisions are detected on the final label strings, after that fallback:
// two teams can share a clan name — the parser permits it, names being
// labels and never identity — and a real clan name can equally collide with
// the other team's generated "Team N". Either way the ID is appended to
// both so columns and winners stay distinguishable.
func seriesTeamLabels(series *analysis.SeriesAnalysis) map[int]string {
	labels := make(map[int]string, len(series.Teams))
	counts := make(map[string]int, len(series.Teams))
	for _, team := range series.Teams {
		label := team.Name
		if label == "" {
			label = fmt.Sprintf("Team %d", team.TeamID)
		}
		labels[team.TeamID] = label
		counts[label]++
	}
	for _, team := range series.Teams {
		if counts[labels[team.TeamID]] > 1 {
			labels[team.TeamID] = fmt.Sprintf("%s (team %d)", labels[team.TeamID], team.TeamID)
		}
	}
	return labels
}

// sortedSeriesByKills orders aggregate players the way sortedByKills orders
// map players.
func sortedSeriesByKills(players map[uint64]*analysis.SeriesPlayer) []*analysis.SeriesPlayer {
	return slices.SortedFunc(maps.Values(players), func(a, b *analysis.SeriesPlayer) int {
		return killsOrder(a.KillStats.Total, b.KillStats.Total, a.Name, b.Name, a.SteamID, b.SteamID)
	})
}

// selectedSeriesPlayers narrows the aggregate players to the selection; a
// nil selection means everyone.
func selectedSeriesPlayers(series *analysis.SeriesAnalysis, selected map[uint64]bool) map[uint64]*analysis.SeriesPlayer {
	if selected == nil {
		return series.Players
	}
	players := make(map[uint64]*analysis.SeriesPlayer, len(selected))
	for id, player := range series.Players {
		if selected[id] {
			players[id] = player
		}
	}
	return players
}

// seriesPlayerView reshapes an aggregate player into the per-map player
// struct the existing rating-free tables and CSV row builder format. It is
// a rendering adapter only: user_id stays zero and is never printed. The
// view cannot express an omitted rating, so every rating-consuming output
// reads the SeriesPlayer itself instead — printSeriesPlayersTable and
// printSeriesRatingTable render "-", and seriesCSVRecords injects blank
// rating cells.
func seriesPlayerView(player *analysis.SeriesPlayer) *analysis.DemoPlayer {
	return &analysis.DemoPlayer{
		SteamID:          player.SteamID,
		Name:             player.Name,
		TeamID:           player.TeamID,
		Deaths:           player.Deaths,
		DeathsTraded:     player.DeathsTraded,
		KillStats:        player.KillStats,
		AssistStats:      player.AssistStats,
		PlayerMapStats:   player.PlayerStats,
		OpeningDuelStats: player.OpeningDuelStats,
		SideStats:        player.SideStats,
		UtilityStats:     player.UtilityStats,
	}
}

// PrintSeriesCLITables prints the multi-demo output: the ordered map
// results with the overall map score, round score and winner, then the
// aggregate player table for the selected players (everyone when selected
// is nil).
func PrintSeriesCLITables(series *analysis.SeriesAnalysis, selected map[uint64]bool) {
	printSeriesResultTable(series, os.Stdout)
	printSeriesPlayersTable(series, selected, os.Stdout)
}

func printSeriesResultTable(series *analysis.SeriesAnalysis, output io.Writer) {
	labels := seriesTeamLabels(series)
	// BuildSeries emits the teams in series-team-ID order, 1 then 2.
	teamOne, teamTwo := series.Teams[0], series.Teams[1]
	winner, loser := teamOne, teamTwo
	if series.WinnerTeamID == teamTwo.TeamID {
		winner, loser = teamTwo, teamOne
	}

	t := table.NewWriter()
	t.SetOutputMirror(output)
	t.SetTitle("Best-of-%d map results", series.BestOf)
	t.AppendHeader(table.Row{"#", "Map", labels[teamOne.TeamID], labels[teamTwo.TeamID], "Winner"})
	for i, seriesMap := range series.Maps {
		seriesOf := make(map[int]int, len(seriesMap.TeamAssignments))
		for _, assignment := range seriesMap.TeamAssignments {
			seriesOf[assignment.MapTeamID] = assignment.SeriesTeamID
		}
		rounds := make(map[int]int, len(seriesMap.Analysis.Teams))
		for _, mapTeam := range seriesMap.Analysis.Teams {
			rounds[seriesOf[mapTeam.TeamID]] += mapTeam.Score
		}
		t.AppendRow(table.Row{
			i + 1,
			seriesMap.Analysis.Map.MapName,
			rounds[teamOne.TeamID],
			rounds[teamTwo.TeamID],
			labels[seriesMap.WinnerTeamID],
		})
	}
	t.AppendFooter(table.Row{"", "Maps", teamOne.MapsWon, teamTwo.MapsWon, labels[winner.TeamID]})
	t.AppendFooter(table.Row{"", "Rounds", teamOne.RoundsWon, teamTwo.RoundsWon, ""})
	t.SetCaption("Series winner: %s (%d:%d maps, %d:%d rounds)\n",
		labels[winner.TeamID], winner.MapsWon, loser.MapsWon, winner.RoundsWon, loser.RoundsWon)
	t.Render()
}

// printSeriesPlayersTable is the aggregate counterpart of the main
// single-map table, with a Maps column instead of the map footer. Rates are
// series-wide recomputations; a player whose rating could not be recomputed
// from raw facts shows "-".
func printSeriesPlayersTable(series *analysis.SeriesAnalysis, selected map[uint64]bool, output io.Writer) {
	t := table.NewWriter()
	t.SetOutputMirror(output)
	t.SetTitle("Series player aggregates")
	t.AppendHeader(table.Row{"Name", "Maps", "Kills", "Deaths", "K/D", "HS", "Assists", "ADR", "KAST (%)", "Rating", "Entry", "Precision (%)", "Best Weapon"})
	for _, player := range sortedSeriesByKills(selectedSeriesPlayers(series, selected)) {
		rating := "-"
		if player.Rating != nil {
			rating = fmt.Sprintf("%.2f", player.Rating.Value)
		}
		t.AppendRow(table.Row{
			player.Name,
			player.MapsPlayed,
			player.KillStats.Total,
			player.Deaths,
			kdRatio(player.KillStats.Total, player.Deaths),
			player.KillStats.HeadShots,
			player.AssistStats.Total,
			fmt.Sprintf("%.1f", player.AssistStats.ADR),
			fmt.Sprintf("%.1f", player.PlayerStats.KAST),
			rating,
			entryScore(player.OpeningDuelStats),
			fmt.Sprintf("%.1f", player.KillStats.Precision*100),
			analysis.GetPlayerBestWeapon(player.KillStats.WeaponsKills),
		})
	}
	t.Render()
}

// PrintSeriesDetailTables prints the aggregate versions of the --details
// tables for the selected players (everyone when selected is nil). The
// rating breakdown goes through the aggregate players themselves so an
// omitted rating stays visibly omitted; the per-map detail values remain
// available through the saved series JSON.
func PrintSeriesDetailTables(series *analysis.SeriesAnalysis, selected map[uint64]bool) {
	sorted := sortedSeriesByKills(selectedSeriesPlayers(series, selected))
	printSeriesRatingTable(sorted, os.Stdout)
	views := make([]*analysis.DemoPlayer, len(sorted))
	for i, player := range sorted {
		views[i] = seriesPlayerView(player)
	}
	printApproxMetricsTable(views, os.Stdout)
	printMultiKillTable(views, os.Stdout)
	printTradeTable(views, os.Stdout)
	printSideSplitTable(views, os.Stdout)
	printUtilityEffectivenessTable(views, os.Stdout)
	printGrenadesThrownTable(views, os.Stdout)
}

// printSeriesRatingTable feeds aggregate players to the shared rating
// breakdown; a nil rating renders as "-" cells there.
func printSeriesRatingTable(players []*analysis.SeriesPlayer, output io.Writer) {
	rows := make([]ratingRow, len(players))
	for i, player := range players {
		rows[i] = ratingRow{name: player.Name, rating: player.Rating}
	}
	printRatingRows(rows, output)
}

// WriteSeriesToFile writes the series as JSON — the complete envelope with
// the ordered map analyses — or as CSV. The CSV deliberately stays the
// existing flat aggregate-player table with the exact single-map header,
// order and formatting: no series, team, hash or map columns leak into it,
// so full per-map series data requires JSON. The one series-specific cell
// rule is that an omitted rating leaves its seven columns empty instead of
// printing as 0.00.
func WriteSeriesToFile(series *analysis.SeriesAnalysis, saveType string) (string, error) {
	fileName := fmt.Sprintf("%d_data.%s", time.Now().Unix(), saveType)

	if saveType == "csv" {
		csvFile, err := os.Create(fileName)
		if err != nil {
			return "", err
		}
		defer csvFile.Close()

		w := csv.NewWriter(csvFile)
		if err := w.WriteAll(seriesCSVRecords(series.Players)); err != nil {
			return "", err
		}
		return fileName, nil
	}

	jsonData, err := json.MarshalIndent(series, "", " ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(fileName, jsonData, 0644); err != nil {
		return "", err
	}
	return fileName, nil
}

// seriesCSVRecords builds the aggregate-player rows under the single-map
// header contract, blanking the rating columns of players whose series
// rating was not recomputed.
func seriesCSVRecords(players map[uint64]*analysis.SeriesPlayer) [][]string {
	records := [][]string{playerCSVHeader()}
	for _, player := range sortedSeriesByKills(players) {
		ratingCells := omittedRatingCSVCells()
		if player.Rating != nil {
			ratingCells = ratingCSVCells(*player.Rating)
		}
		records = append(records, playerCSVRow(seriesPlayerView(player), ratingCells))
	}
	return records
}
