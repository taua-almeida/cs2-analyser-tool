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

type PlayerMapStats struct {
	MVPs        int     `json:"mvps"`
	ACEs        int     `json:"aces"`
	ClutchesWon int     `json:"clutches_won"`
	KAST        float64 `json:"kast"`
}

// SideCount is a per-round counter split by the side the player was on.
type SideCount struct {
	Total int `json:"total"`
	CT    int `json:"ct"`
	T     int `json:"t"`
}

type OpeningDuelStats struct {
	OpeningKills   SideCount `json:"opening_kills"`
	OpeningDeaths  SideCount `json:"opening_deaths"`
	OpeningSuccess float64   `json:"opening_success"`
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
