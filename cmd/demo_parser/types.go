package demoparser

type KillStats struct {
	Total        int            `json:"total"`
	HeadShots    int            `json:"headshots"`
	Precision    float64        `json:"precision"`
	WeaponsKills map[string]int `json:"weapons_kills"`
}

type AssistStats struct {
	Total          int `json:"total"`
	FlashedEnemies int `json:"flashed_enemies"`
	DamageGiven    int `json:"damage_given"`
}

type DemoPlayer struct {
	SteamID     uint64      `json:"steam_id"`
	Name        string      `json:"name"`
	UserID      int         `json:"user_id"`
	Deaths      int         `json:"deaths"`
	KillStats   KillStats   `json:"kill_stats"`
	AssistStats AssistStats `json:"assist_stats"`
}