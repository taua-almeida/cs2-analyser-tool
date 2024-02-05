package demoparser

import common "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/common"

type KillStats struct {
	Total        int            `json:"total"`
	HeadShots    int            `json:"headshots"`
	Precision    float64        `json:"precision"`
	WeaponsKills map[string]int `json:"weapons_kills"`
	TradeKills   int            `json:"trade_kills"`
}

type AssistStats struct {
	Total          int `json:"total"`
	FlashedEnemies int `json:"flashed_enemies"`
	DamageGiven    int `json:"damage_given"`
}

type PlayerMapStats struct {
	MVPs        int `json:"mvps"`
	ACEs        int `json:"aces"`
	ClutchesWon int `json:"clutches_won"`
}

type RoundData struct {
	RoundNumber       int
	KillsByPlayer     map[uint64]int // Key: Player's SteamID64, Value: Number of kills in the round
	PlayersAliveCT    int
	PlayersAliveT     int
	WinningTeam       common.Team
	ClutchWonByPlayer uint64 // SteamID of the player who won the clutch, if any
}

type DemoPlayer struct {
	SteamID        uint64         `json:"steam_id"`
	Name           string         `json:"name"`
	UserID         int            `json:"user_id"`
	Deaths         int            `json:"deaths"`
	KillStats      KillStats      `json:"kill_stats"`
	AssistStats    AssistStats    `json:"assist_stats"`
	PlayerMapStats PlayerMapStats `json:"player_map_stats"`
	RoundData      []RoundData    `json:"round_data"`
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
