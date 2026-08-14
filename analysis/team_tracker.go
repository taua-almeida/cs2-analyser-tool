package analysis

import (
	"fmt"
	"slices"

	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// teamRoundFacts is what one accepted scored round contributes to logical
// team identity: the eligible SteamID64s that played each side, the clan
// names the scoreboard showed for those sides, and the side that won (or
// TeamUnassigned when the round's decision was never seen).
type teamRoundFacts struct {
	round             int // 1-based position among the match's finalized rounds, for errors
	ctRoster, tRoster []uint64
	ctClan, tClan     string
	winner            common.Team
}

// nameCount is one observed label spelling and how often it was seen.
type nameCount struct {
	name  string
	count int
}

// nameTally is the one implementation of the label rule shared by map teams,
// series teams and series players: nonempty labels are collected
// deduplicated in first-observation order, and the display name is the most
// observed one with ties broken by first observation. For a map team an
// observation is an accepted round, so a mid-match rename only takes over
// once it has been on the scoreboard longer than the old name; series teams
// and players observe once per map. Something never named has no display
// name — one is never invented.
type nameTally struct {
	counts []nameCount
}

func (nt *nameTally) observe(name string) {
	if name == "" {
		return
	}
	for i := range nt.counts {
		if nt.counts[i].name == name {
			nt.counts[i].count++
			return
		}
	}
	nt.counts = append(nt.counts, nameCount{name: name, count: 1})
}

func (nt *nameTally) mostObserved() string {
	name, best := "", 0
	for _, count := range nt.counts {
		if count.count > best {
			name, best = count.name, count.count
		}
	}
	return name
}

// names returns the observed labels in first-observation order, non-nil even
// when empty so they serialize as [] rather than null.
func (nt *nameTally) names() []string {
	names := make([]string, len(nt.counts))
	for i, count := range nt.counts {
		names[i] = count.name
	}
	return names
}

// logicalTeam accumulates one map-local team: the members that have played
// an accepted round for it, every clan-name alias observed while they held a
// side, and the accepted rounds they won.
type logicalTeam struct {
	id      int
	members map[uint64]bool
	aliases nameTally
	score   int
}

func newLogicalTeam(id int) *logicalTeam {
	return &logicalTeam{id: id, members: make(map[uint64]bool)}
}

// recordRound folds one accepted round into the team. New roster members are
// substitutes joining the team; a reconnecting member is already in the set
// and cannot be duplicated. An empty clan name stays unknown rather than
// becoming a fake alias.
func (lt *logicalTeam) recordRound(roster []uint64, clan string, won bool) {
	for _, id := range roster {
		lt.members[id] = true
	}
	lt.aliases.observe(clan)
	if won {
		lt.score++
	}
}

func (lt *logicalTeam) export() DemoTeam {
	return DemoTeam{
		TeamID:  lt.id,
		Name:    lt.aliases.mostObserved(),
		Aliases: lt.aliases.names(),
		Score:   lt.score,
		Roster:  lt.memberIDs(),
	}
}

// memberIDs returns the sorted member SteamIDs, non-nil even when empty so
// an empty roster serializes as [] rather than null.
func (lt *logicalTeam) memberIDs() []uint64 {
	ids := make([]uint64, 0, len(lt.members))
	for id := range lt.members {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// teamTracker resolves the two logical teams of one map. CT and T are
// temporary sides that swap at halftime and with every overtime half, so
// identity is carried by the eligible SteamID64 rosters alone: the tracker
// is only fed rounds the authoritative scored-round gate accepted, seeds the
// teams from the first of those rounds, and maps every later round's sides
// back to the seeded teams through shared members. Clan names are labels
// that never carry identity; the full contract is documented on DemoTeam.
type teamTracker struct {
	teams [2]*logicalTeam
}

func newTeamTracker() *teamTracker {
	return &teamTracker{}
}

func (tt *teamTracker) seeded() bool {
	return tt.teams[0] != nil
}

// applyRound folds one accepted round into the two teams. The first round
// with any eligible participant seeds them: team 1 is the seeding round's CT
// side and team 2 its T side, a deterministic map-local numbering that
// claims nothing about the sides or organizations of any other demo. A
// round with no eligible participant carries no identity evidence and
// contributes nothing.
func (tt *teamTracker) applyRound(facts teamRoundFacts) error {
	if len(facts.ctRoster)+len(facts.tRoster) == 0 {
		return nil
	}
	var ctTeam, tTeam *logicalTeam
	if !tt.seeded() {
		ctTeam, tTeam = newLogicalTeam(1), newLogicalTeam(2)
		tt.teams[0], tt.teams[1] = ctTeam, tTeam
	} else {
		var err error
		ctTeam, tTeam, err = tt.resolve(facts)
		if err != nil {
			return err
		}
	}
	ctTeam.recordRound(facts.ctRoster, facts.ctClan, facts.winner == common.TeamCounterTerrorists)
	tTeam.recordRound(facts.tRoster, facts.tClan, facts.winner == common.TeamTerrorists)
	return nil
}

// resolve maps the round's two side rosters onto the seeded teams. A mapping
// is confident when it has roster evidence (a shared eligible SteamID) and
// no conflict (no player it would place on both teams); the straight and
// crossed mappings cannot both qualify, because any evidence for one is a
// conflict of the other. Substitutes resolve through their teammates, and a
// side of entirely new players still resolves by elimination when the other
// side maps uniquely. Anything less returns an error carrying the competing
// evidence instead of guessing.
func (tt *teamTracker) resolve(facts teamRoundFacts) (ctTeam, tTeam *logicalTeam, err error) {
	first, second := tt.teams[0], tt.teams[1]
	straight := overlapCount(facts.ctRoster, first) + overlapCount(facts.tRoster, second)
	crossed := overlapCount(facts.ctRoster, second) + overlapCount(facts.tRoster, first)

	if straight > 0 && crossed == 0 {
		return first, second, nil
	}
	if crossed > 0 && straight == 0 {
		return second, first, nil
	}
	evidence := fmt.Sprintf(
		"CT roster %v shares %v with team 1 and %v with team 2; T roster %v shares %v with team 1 and %v with team 2",
		sortedIDs(facts.ctRoster), overlap(facts.ctRoster, first), overlap(facts.ctRoster, second),
		sortedIDs(facts.tRoster), overlap(facts.tRoster, first), overlap(facts.tRoster, second))
	if straight+crossed == 0 {
		return nil, nil, fmt.Errorf(
			"finalized round %d: cannot resolve logical teams: neither side shares an eligible SteamID with a seeded team: %s; team 1 members %v, team 2 members %v",
			facts.round, evidence, first.memberIDs(), second.memberIDs())
	}
	return nil, nil, fmt.Errorf(
		"finalized round %d: eligible SteamIDs appear to participate for both logical teams: %s",
		facts.round, evidence)
}

// export returns the two teams in team-ID order, or no teams when no
// accepted round ever had an eligible participant.
func (tt *teamTracker) export() []DemoTeam {
	if !tt.seeded() {
		return []DemoTeam{}
	}
	return []DemoTeam{tt.teams[0].export(), tt.teams[1].export()}
}

// overlapCount reports how many of the roster IDs are members of the team.
func overlapCount(roster []uint64, team *logicalTeam) int {
	count := 0
	for _, id := range roster {
		if team.members[id] {
			count++
		}
	}
	return count
}

// overlap returns the roster IDs that are members of the team, sorted. It
// only feeds error evidence; resolve decides on counts alone.
func overlap(roster []uint64, team *logicalTeam) []uint64 {
	var shared []uint64
	for _, id := range roster {
		if team.members[id] {
			shared = append(shared, id)
		}
	}
	slices.Sort(shared)
	return shared
}

func sortedIDs(roster []uint64) []uint64 {
	sorted := slices.Clone(roster)
	slices.Sort(sorted)
	return sorted
}
