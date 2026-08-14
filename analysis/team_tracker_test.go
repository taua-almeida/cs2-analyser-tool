package analysis

import (
	"reflect"
	"strings"
	"testing"

	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	events "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// teamAnalyser is a liveAnalyser whose scoreboard shows the given clan names
// for the CT and T sides.
func teamAnalyser(ctClan, tClan string, playing ...*common.Player) *analyser {
	a := liveAnalyser(playing...)
	p := a.parser.(*matchParser)
	p.ctClan, p.tClan = ctClan, tClan
	return a
}

// swapSides flips every player to the other side and swaps the scoreboard
// clan names with them, exactly as a halftime or overtime side switch does.
func swapSides(a *analyser, players ...*common.Player) {
	for _, pl := range players {
		switch pl.Team {
		case common.TeamTerrorists:
			pl.Team = common.TeamCounterTerrorists
		case common.TeamCounterTerrorists:
			pl.Team = common.TeamTerrorists
		}
	}
	p := a.parser.(*matchParser)
	p.ctClan, p.tClan = p.tClan, p.ctClan
}

func TestFirstScoredRoundSeedsTwoLogicalTeams(t *testing.T) {
	ct := player(11, "ct", common.TeamCounterTerrorists)
	tPlayer := player(1, "t", common.TeamTerrorists)
	a := teamAnalyser("Alpha", "Bravo", ct, tPlayer)

	playRound(a, common.TeamCounterTerrorists)
	a.finalise()

	want := []DemoTeam{
		{TeamID: 1, Name: "Alpha", Aliases: []string{"Alpha"}, Score: 1, Roster: []uint64{11}},
		{TeamID: 2, Name: "Bravo", Aliases: []string{"Bravo"}, Score: 0, Roster: []uint64{1}},
	}
	if !reflect.DeepEqual(a.teams, want) {
		t.Errorf("teams = %+v, want %+v", a.teams, want)
	}
	if got := a.players[11].TeamID; got != 1 {
		t.Errorf("CT player team_id = %d, want 1", got)
	}
	if got := a.players[1].TeamID; got != 2 {
		t.Errorf("T player team_id = %d, want 2", got)
	}
}

func TestHalftimeSwapKeepsLogicalTeamIdentity(t *testing.T) {
	first := player(1, "first", common.TeamTerrorists)
	second := player(11, "second", common.TeamCounterTerrorists)
	a := teamAnalyser("Alpha", "Bravo", first, second)

	playRound(a, common.TeamTerrorists)
	swapSides(a, first, second)
	playRound(a, common.TeamCounterTerrorists)
	playRound(a, common.TeamTerrorists)
	a.finalise()

	// Player 1's team won as T before the swap and as CT after it; player
	// 11's team won the last round on its new T side. Identity, aliases and
	// scores all stay with the lineups, not the sides.
	want := []DemoTeam{
		{TeamID: 1, Name: "Alpha", Aliases: []string{"Alpha"}, Score: 1, Roster: []uint64{11}},
		{TeamID: 2, Name: "Bravo", Aliases: []string{"Bravo"}, Score: 2, Roster: []uint64{1}},
	}
	if !reflect.DeepEqual(a.teams, want) {
		t.Errorf("teams after halftime = %+v, want %+v", a.teams, want)
	}
}

func TestOvertimeSwapsKeepIdentityAndScores(t *testing.T) {
	first := player(1, "first", common.TeamTerrorists)
	second := player(11, "second", common.TeamCounterTerrorists)
	a := teamAnalyser("Alpha", "Bravo", first, second)

	// Player 1's side wins every round while the teams change ends before
	// each one, as aggressively as any overtime format could.
	for range 6 {
		playRound(a, first.Team)
		swapSides(a, first, second)
	}
	a.finalise()

	want := []DemoTeam{
		{TeamID: 1, Name: "Alpha", Aliases: []string{"Alpha"}, Score: 0, Roster: []uint64{11}},
		{TeamID: 2, Name: "Bravo", Aliases: []string{"Bravo"}, Score: 6, Roster: []uint64{1}},
	}
	if !reflect.DeepEqual(a.teams, want) {
		t.Errorf("teams after overtime swaps = %+v, want %+v", a.teams, want)
	}
}

func TestSubstituteJoinsUniquelyMatchedTeam(t *testing.T) {
	starter := player(1, "starter", common.TeamTerrorists)
	teammate := player(2, "teammate", common.TeamTerrorists)
	opponent := player(11, "opponent", common.TeamCounterTerrorists)
	a := teamAnalyser("", "", starter, teammate, opponent)

	playRound(a, common.TeamTerrorists)
	substitute := player(3, "substitute", common.TeamTerrorists)
	a.parser.(*matchParser).playing = []*common.Player{starter, substitute, opponent}
	playRound(a, common.TeamTerrorists)
	a.finalise()

	if got, want := a.teams[1].Roster, []uint64{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("team 2 roster = %v, want the substitute added to %v", got, want)
	}
	if got := a.players[3].TeamID; got != 2 {
		t.Errorf("substitute team_id = %d, want 2", got)
	}
	if got := a.teams[1].Score; got != 2 {
		t.Errorf("team 2 score = %d, want 2", got)
	}
}

func TestReconnectDoesNotDuplicateParticipation(t *testing.T) {
	returning := player(1, "returning", common.TeamTerrorists)
	teammate := player(2, "teammate", common.TeamTerrorists)
	opponent := player(11, "opponent", common.TeamCounterTerrorists)
	a := teamAnalyser("", "", returning, teammate, opponent)
	p := a.parser.(*matchParser)

	playRound(a, common.TeamTerrorists)
	p.playing = []*common.Player{teammate, opponent}
	playRound(a, common.TeamTerrorists)
	p.playing = []*common.Player{returning, teammate, opponent}
	playRound(a, common.TeamTerrorists)
	a.finalise()

	if got, want := a.teams[1].Roster, []uint64{1, 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("team 2 roster after reconnect = %v, want %v", got, want)
	}
	if got := a.teams[1].Score; got != 3 {
		t.Errorf("team 2 score = %d, want all three rounds", got)
	}
}

func TestWarmupKnifeAndSetupRoundsDoNotSeedTeams(t *testing.T) {
	knifer := player(99, "knifer", common.TeamCounterTerrorists)
	starterCT := player(11, "starter-ct", common.TeamCounterTerrorists)
	extraCT := player(98, "extra-ct", common.TeamCounterTerrorists)
	starterT := player(1, "starter-t", common.TeamTerrorists)
	a := teamAnalyser("Alpha", "Bravo", knifer, starterT)
	p := a.parser.(*matchParser)

	// A pregame knife round with its own roster is gated out entirely.
	p.gamePhase = common.GamePhasePregame
	a.onRoundStart(events.RoundStart{})
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamCounterTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	// An unscored setup round restarts at the same score; its roster still
	// contains a player who leaves before the real first round.
	p.gamePhase = common.GamePhaseStartGamePhase
	p.playing = []*common.Player{starterCT, extraCT, starterT}
	a.onRoundStart(events.RoundStart{})

	p.playing = []*common.Player{starterCT, starterT}
	playRound(a, common.TeamCounterTerrorists)
	a.finalise()

	want := []DemoTeam{
		{TeamID: 1, Name: "Alpha", Aliases: []string{"Alpha"}, Score: 1, Roster: []uint64{11}},
		{TeamID: 2, Name: "Bravo", Aliases: []string{"Bravo"}, Score: 0, Roster: []uint64{1}},
	}
	if !reflect.DeepEqual(a.teams, want) {
		t.Errorf("teams = %+v, want seeding from the first scored round only: %+v", a.teams, want)
	}
	if a.players[99] != nil {
		t.Error("pregame knife-round participant became a player")
	}
	if got := a.players[98].TeamID; got != 0 {
		t.Errorf("setup-round participant team_id = %d, want 0: they never played a scored round", got)
	}
}

func TestBotsAndCoachesAreNotTeamMembers(t *testing.T) {
	competitor := player(1, "competitor", common.TeamTerrorists)
	bot := botPlayer(101, "bot", common.TeamTerrorists)
	coach := playerWithCoachingTeam(50, "coach", common.TeamCounterTerrorists, common.TeamCounterTerrorists)
	opponent := player(11, "opponent", common.TeamCounterTerrorists)
	a := teamAnalyser("", "", competitor, bot, coach, opponent)

	playRound(a, common.TeamTerrorists)
	a.finalise()

	if got, want := a.teams[0].Roster, []uint64{11}; !reflect.DeepEqual(got, want) {
		t.Errorf("team 1 roster = %v, want the coach excluded: %v", got, want)
	}
	if got, want := a.teams[1].Roster, []uint64{1}; !reflect.DeepEqual(got, want) {
		t.Errorf("team 2 roster = %v, want the bot excluded: %v", got, want)
	}
}

func TestEmptyClanNamesStayUnknown(t *testing.T) {
	ct := player(11, "ct", common.TeamCounterTerrorists)
	tPlayer := player(1, "t", common.TeamTerrorists)
	a := teamAnalyser("", "", ct, tPlayer)

	playRound(a, common.TeamCounterTerrorists)
	a.finalise()

	want := []DemoTeam{
		{TeamID: 1, Name: "", Aliases: []string{}, Score: 1, Roster: []uint64{11}},
		{TeamID: 2, Name: "", Aliases: []string{}, Score: 0, Roster: []uint64{1}},
	}
	if !reflect.DeepEqual(a.teams, want) {
		t.Errorf("teams = %+v, want unknown names with no invented alias: %+v", a.teams, want)
	}
}

func TestDuplicateClanNamesDoNotMergeTeams(t *testing.T) {
	ct := player(11, "ct", common.TeamCounterTerrorists)
	tPlayer := player(1, "t", common.TeamTerrorists)
	a := teamAnalyser("MIX", "MIX", ct, tPlayer)

	playRound(a, common.TeamCounterTerrorists)
	playRound(a, common.TeamTerrorists)
	a.finalise()

	want := []DemoTeam{
		{TeamID: 1, Name: "MIX", Aliases: []string{"MIX"}, Score: 1, Roster: []uint64{11}},
		{TeamID: 2, Name: "MIX", Aliases: []string{"MIX"}, Score: 1, Roster: []uint64{1}},
	}
	if !reflect.DeepEqual(a.teams, want) {
		t.Errorf("teams = %+v, want two same-named teams kept apart: %+v", a.teams, want)
	}
}

func TestChangingClanNamesBecomeAliasesDeterministically(t *testing.T) {
	ct := player(11, "ct", common.TeamCounterTerrorists)
	tPlayer := player(1, "t", common.TeamTerrorists)
	a := teamAnalyser("Alpha", "Bravo", ct, tPlayer)
	p := a.parser.(*matchParser)

	playRound(a, common.TeamCounterTerrorists)
	p.ctClan = "Alpha GG"
	playRound(a, common.TeamCounterTerrorists)
	playRound(a, common.TeamCounterTerrorists)
	a.finalise()

	team := a.teams[0]
	if want := []string{"Alpha", "Alpha GG"}; !reflect.DeepEqual(team.Aliases, want) {
		t.Errorf("aliases = %v, want first-observation order %v", team.Aliases, want)
	}
	if team.Name != "Alpha GG" {
		t.Errorf("name = %q, want the alias observed in the most rounds, %q", team.Name, "Alpha GG")
	}
}

func TestDisplayNameTieBreaksToFirstObservedAlias(t *testing.T) {
	tracker := newTeamTracker()
	rounds := []teamRoundFacts{
		{ctRoster: []uint64{11}, tRoster: []uint64{1}, ctClan: "Alpha", tClan: "Bravo"},
		{ctRoster: []uint64{11}, tRoster: []uint64{1}, ctClan: "Alpha GG", tClan: "Bravo"},
	}
	for _, facts := range rounds {
		if err := tracker.applyRound(facts); err != nil {
			t.Fatalf("applyRound: %v", err)
		}
	}

	if got := tracker.export()[0].Name; got != "Alpha" {
		t.Errorf("tied aliases resolved to %q, want the first observed, %q", got, "Alpha")
	}
}

func TestFullSideOfSubstitutesResolvesByElimination(t *testing.T) {
	tracker := newTeamTracker()
	seed := teamRoundFacts{ctRoster: []uint64{11}, tRoster: []uint64{1}}
	if err := tracker.applyRound(seed); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// The whole T lineup is new; the CT side still maps uniquely, so the
	// new players can only be the other team.
	replaced := teamRoundFacts{ctRoster: []uint64{11}, tRoster: []uint64{5}, winner: common.TeamTerrorists}
	if err := tracker.applyRound(replaced); err != nil {
		t.Fatalf("elimination round: %v", err)
	}

	teams := tracker.export()
	if got, want := teams[1].Roster, []uint64{1, 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("team 2 roster = %v, want %v", got, want)
	}
	if got := teams[1].Score; got != 1 {
		t.Errorf("team 2 score = %d, want the eliminated-side win counted", got)
	}
}

func TestRoundsWithoutEligiblePlayersCarryNoTeamFacts(t *testing.T) {
	tracker := newTeamTracker()

	// A bots-only round cannot seed identity.
	if err := tracker.applyRound(teamRoundFacts{winner: common.TeamTerrorists}); err != nil {
		t.Fatalf("bots-only round before seeding: %v", err)
	}
	if got := tracker.export(); len(got) != 0 {
		t.Fatalf("teams = %+v, want none before an eligible participant", got)
	}

	seed := teamRoundFacts{ctRoster: []uint64{11}, tRoster: []uint64{1}, ctClan: "Alpha", tClan: "Bravo"}
	if err := tracker.applyRound(seed); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	// After seeding, another bots-only round moves no score to either team.
	if err := tracker.applyRound(teamRoundFacts{winner: common.TeamTerrorists}); err != nil {
		t.Fatalf("bots-only round after seeding: %v", err)
	}

	teams := tracker.export()
	if teams[0].Score != 0 || teams[1].Score != 0 {
		t.Errorf("scores = %d/%d, want no win attributed without roster evidence", teams[0].Score, teams[1].Score)
	}
}

func TestUndecidedAcceptedRoundMovesNoTeamScore(t *testing.T) {
	tracker := newTeamTracker()
	facts := teamRoundFacts{ctRoster: []uint64{11}, tRoster: []uint64{1}, winner: common.TeamUnassigned}
	if err := tracker.applyRound(facts); err != nil {
		t.Fatalf("applyRound: %v", err)
	}

	teams := tracker.export()
	if teams[0].Score != 0 || teams[1].Score != 0 {
		t.Errorf("scores = %d/%d, want 0/0 for a round with no recorded winner", teams[0].Score, teams[1].Score)
	}
}

func TestAmbiguousRosterReturnsActionableError(t *testing.T) {
	first := player(1, "first", common.TeamTerrorists)
	second := player(11, "second", common.TeamCounterTerrorists)
	a := teamAnalyser("", "", first, second)
	p := a.parser.(*matchParser)

	playRound(a, common.TeamTerrorists)
	// The next scored round is played by entirely different SteamIDs, so
	// neither side can be mapped to a seeded team.
	p.playing = []*common.Player{
		player(3, "unknown-t", common.TeamTerrorists),
		player(13, "unknown-ct", common.TeamCounterTerrorists),
	}
	playRound(a, common.TeamTerrorists)

	err := a.finalise()
	if err == nil {
		t.Fatal("fully replaced rosters did not produce an ambiguity error")
	}
	msg := err.Error()
	for _, evidence := range []string{"cannot resolve logical teams", "finalized round 2", "[13]", "[3]", "team 1 members [11]", "team 2 members [1]"} {
		if !strings.Contains(msg, evidence) {
			t.Errorf("ambiguity error %q does not carry evidence %q", msg, evidence)
		}
	}
}

// A halftime side switch can land during freeze time, after RoundStart
// snapshotted the old sides. A player who disconnects inside that window
// must not be presented to team resolution on their stale pre-switch side,
// where they would sit among the other lineup and read as playing for both
// teams.
func TestPreSwapFreezeTimeLeaverDoesNotBreakIdentity(t *testing.T) {
	one := player(1, "one", common.TeamTerrorists)
	two := player(2, "two", common.TeamTerrorists)
	eleven := player(11, "eleven", common.TeamCounterTerrorists)
	twelve := player(12, "twelve", common.TeamCounterTerrorists)
	a := teamAnalyser("", "", one, two, eleven, twelve)
	p := a.parser.(*matchParser)

	playRound(a, common.TeamTerrorists)

	// Halftime: RoundStart still sees the old sides.
	a.onRoundStart(events.RoundStart{})
	// The switch lands during freeze time, and player 2 leaves before live
	// play begins.
	one.Team = common.TeamCounterTerrorists
	eleven.Team, twelve.Team = common.TeamTerrorists, common.TeamTerrorists
	p.playing = []*common.Player{one, eleven, twelve}
	a.onDisconnect(events.PlayerDisconnected{Player: two})
	a.onRoundFreezetimeEnd(events.RoundFreezetimeEnd{})
	endScoredRound(a, common.TeamCounterTerrorists)

	if err := a.finalise(); err != nil {
		t.Fatalf("freeze-time leaver broke team resolution: %v", err)
	}
	want := []DemoTeam{
		{TeamID: 1, Name: "", Aliases: []string{}, Score: 0, Roster: []uint64{11, 12}},
		{TeamID: 2, Name: "", Aliases: []string{}, Score: 2, Roster: []uint64{1, 2}},
	}
	if !reflect.DeepEqual(a.teams, want) {
		t.Errorf("teams = %+v, want identity kept through the late swap: %+v", a.teams, want)
	}
	if got := a.players[2].TeamID; got != 2 {
		t.Errorf("leaver team_id = %d, want their round-1 team 2", got)
	}
}

// A setup round can receive RoundEnd and still be restarted at the same
// authoritative score. Such a round consumes a scored slot in the player
// facts, but it must not seed logical teams, add its roster to them, or
// move a team score.
func TestDecidedSetupRoundDoesNotSeedOrScoreTeams(t *testing.T) {
	extra := player(98, "extra-ct", common.TeamCounterTerrorists)
	tPlayer := player(1, "t", common.TeamTerrorists)
	ct := player(11, "ct", common.TeamCounterTerrorists)
	a := teamAnalyser("", "", extra, tPlayer)
	p := a.parser.(*matchParser)

	a.onRoundStart(events.RoundStart{})
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamCounterTerrorists})
	a.onRoundEndOfficial(events.RoundEndOfficial{})

	// The match restarts at the same count with the real lineup.
	p.playing = []*common.Player{ct, tPlayer}
	playRound(a, common.TeamCounterTerrorists)
	playRound(a, common.TeamTerrorists)

	if err := a.finalise(); err != nil {
		t.Fatalf("decided setup round broke team identity: %v", err)
	}
	want := []DemoTeam{
		{TeamID: 1, Name: "", Aliases: []string{}, Score: 1, Roster: []uint64{11}},
		{TeamID: 2, Name: "", Aliases: []string{}, Score: 1, Roster: []uint64{1}},
	}
	if !reflect.DeepEqual(a.teams, want) {
		t.Errorf("teams = %+v, want seeding and scores from the real rounds only: %+v", a.teams, want)
	}
	if got := a.players[98].TeamID; got != 0 {
		t.Errorf("setup-round participant team_id = %d, want 0", got)
	}
}

func TestTeamConflictInFinaliseFlushedRoundIsReported(t *testing.T) {
	first := player(1, "first", common.TeamTerrorists)
	second := player(11, "second", common.TeamCounterTerrorists)
	a := teamAnalyser("", "", first, second)
	p := a.parser.(*matchParser)

	playRound(a, common.TeamTerrorists)
	// The final round is played by unknown SteamIDs and its official end
	// never arrives, so only the finalise flush can apply it.
	p.playing = []*common.Player{
		player(3, "unknown-t", common.TeamTerrorists),
		player(13, "unknown-ct", common.TeamCounterTerrorists),
	}
	a.onRoundStart(events.RoundStart{})
	markRoundScored(a)
	a.onRoundEnd(events.RoundEnd{Winner: common.TeamTerrorists})

	if err := a.finalise(); err == nil {
		t.Fatal("team conflict in the finalise-flushed round was swallowed")
	}
}

func TestPlayerOnBothTeamsFailsExplicitly(t *testing.T) {
	switcher := player(1, "switcher", common.TeamTerrorists)
	teammate := player(2, "teammate", common.TeamTerrorists)
	opponentA := player(11, "opponent-a", common.TeamCounterTerrorists)
	opponentB := player(12, "opponent-b", common.TeamCounterTerrorists)
	a := teamAnalyser("", "", switcher, teammate, opponentA, opponentB)

	playRound(a, common.TeamTerrorists)
	// Player 1 reappears on the other lineup's side.
	switcher.Team = common.TeamCounterTerrorists
	playRound(a, common.TeamTerrorists)

	err := a.finalise()
	if err == nil {
		t.Fatal("a player on both lineups did not produce an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "participate for both logical teams") {
		t.Errorf("error %q does not name the dual participation", msg)
	}
	if !strings.Contains(msg, "[1]") {
		t.Errorf("error %q does not identify the conflicting SteamID", msg)
	}
}
