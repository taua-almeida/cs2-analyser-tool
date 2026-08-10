package demoparser

type KillStats struct {
	Total        int            `json:"total"`
	HeadShots    int            `json:"headshots"`
	Precision    float64        `json:"precision"`
	WeaponsKills map[string]int `json:"weapons_kills"`
	TradeKills   int            `json:"trade_kills"`
	TeamKills    int            `json:"team_kills"`
}

type AssistStats struct {
	Total          int     `json:"total"`
	FlashedEnemies int     `json:"flashed_enemies"`
	DamageGiven    int     `json:"damage_given"`
	ADR            float64 `json:"adr"`
}

// MultiKillRounds counts rounds by how many enemies the player killed in
// them. The buckets are exclusive, so a 4k round is only a 4k and the four
// of them add up to the rounds with at least two enemy kills.
type MultiKillRounds struct {
	K2 int `json:"k2"`
	K3 int `json:"k3"`
	K4 int `json:"k4"`
	K5 int `json:"k5"`
}

type PlayerMapStats struct {
	MVPs        int             `json:"mvps"`
	ACEs        int             `json:"aces"`
	MultiKills  MultiKillRounds `json:"multi_kills"`
	ClutchesWon int             `json:"clutches_won"`
	KAST        float64         `json:"kast"`
}

// SideCount is a per-round counter split by the side the player was on.
type SideCount struct {
	Total int `json:"total"`
	CT    int `json:"ct"`
	T     int `json:"t"`
}

// OpeningDuelStats counts the rounds a player opened or was opened on.
// OpeningSuccessRate is a percentage rather than a count, and is named apart
// from the counts because other trackers use "opening success" for the
// opening-kill tally itself.
type OpeningDuelStats struct {
	OpeningKills       SideCount `json:"opening_kills"`
	OpeningDeaths      SideCount `json:"opening_deaths"`
	OpeningSuccessRate float64   `json:"opening_success_rate"`
}

type DemoPlayer struct {
	SteamID          uint64           `json:"steam_id"`
	Name             string           `json:"name"`
	UserID           int              `json:"user_id"`
	Deaths           int              `json:"deaths"`
	KillStats        KillStats        `json:"kill_stats"`
	AssistStats      AssistStats      `json:"assist_stats"`
	PlayerMapStats   PlayerMapStats   `json:"player_map_stats"`
	OpeningDuelStats OpeningDuelStats `json:"opening_duel_stats"`
}

type MapData struct {
	MapName     string `json:"map_name"`
	TotalRounds int    `json:"total_rounds"`
	RoundsWonCT int    `json:"rounds_won_ct"`
	RoundsWonT  int    `json:"rounds_won_t"`
}

type ProcessedDemo struct {
	Players  map[uint64]*DemoPlayer `json:"players"`
	Map      MapData                `json:"map_data"`
	GameMode string                 `json:"game_mode"`
}
