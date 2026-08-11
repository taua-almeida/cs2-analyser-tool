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

// SideRate is a per-side average or percentage. It carries no total: the
// match-wide value keeps its own field, AssistStats.ADR or
// PlayerMapStats.KAST.
type SideRate struct {
	CT float64 `json:"ct"`
	T  float64 `json:"t"`
}

// SideStats splits the core stats by the side the player was on. Sides swap
// at halftime, so every split is attributed round by round and never from
// the team the player finished the match on.
//
// ADR and KAST are divided by Rounds.CT and Rounds.T, the rounds the player
// was actually in on that side, which is the only denominator that exists
// per player. The match-wide ADR and KAST instead divide by the rounds of
// the whole match, so the two only reconcile for a player who was there for
// all of them.
type SideStats struct {
	Rounds SideCount `json:"rounds"`
	Kills  SideCount `json:"kills"`
	Deaths SideCount `json:"deaths"`
	ADR    SideRate  `json:"adr"`
	KAST   SideRate  `json:"kast"`
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

// UtilityDamageStats counts real enemy health removed by standard damaging
// grenades. Fire combines molotov and incendiary damage because the originating
// grenade cannot be recovered reliably from inferno hurt events.
type UtilityDamageStats struct {
	Total int `json:"total"`
	HE    int `json:"he"`
	Fire  int `json:"fire"`
}

// GrenadesThrownStats counts one use per standard grenade projectile created.
// Total is maintained with the six mutually exclusive type counters.
type GrenadesThrownStats struct {
	Total      int `json:"total"`
	Flash      int `json:"flash"`
	Smoke      int `json:"smoke"`
	HE         int `json:"he"`
	Molotov    int `json:"molotov"`
	Incendiary int `json:"incendiary"`
	Decoy      int `json:"decoy"`
}

// UtilityStats holds match-wide flash, damage, usage, and unused-inventory
// measurements. Flash counts are events rather than unique affected players.
type UtilityStats struct {
	EnemiesFlashed               int                 `json:"enemies_flashed"`
	FriendsFlashed               int                 `json:"friends_flashed"`
	EnemyFlashTimeSeconds        float64             `json:"enemy_flash_time_seconds"`
	AverageEnemyFlashTimeSeconds float64             `json:"average_enemy_flash_time_seconds"`
	UtilityDamage                UtilityDamageStats  `json:"utility_damage"`
	GrenadesThrown               GrenadesThrownStats `json:"grenades_thrown"`
	UnusedUtilityValue           int                 `json:"unused_utility_value"`
}

type DemoPlayer struct {
	SteamID          uint64           `json:"steam_id"`
	Name             string           `json:"name"`
	UserID           int              `json:"user_id"`
	Deaths           int              `json:"deaths"`
	DeathsTraded     SideCount        `json:"deaths_traded"`
	KillStats        KillStats        `json:"kill_stats"`
	AssistStats      AssistStats      `json:"assist_stats"`
	PlayerMapStats   PlayerMapStats   `json:"player_map_stats"`
	OpeningDuelStats OpeningDuelStats `json:"opening_duel_stats"`
	SideStats        SideStats        `json:"side_stats"`
	UtilityStats     UtilityStats     `json:"utility_stats"`
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
