package config

import (
	"github.com/hectorgimenez/d2go/pkg/data/area"
)

type Run string

const (
	CountessRun         Run = "countess"
	AndarielRun         Run = "andariel"
	AncientTunnelsRun   Run = "ancient_tunnels"
	MausoleumRun        Run = "mausoleum"
	SummonerRun         Run = "summoner"
	DurielRun           Run = "duriel"
	MephistoRun         Run = "mephisto"
	TravincalRun        Run = "travincal"
	EldritchRun         Run = "eldritch"
	PindleskinRun       Run = "pindleskin"
	NihlathakRun        Run = "nihlathak"
	TristramRun         Run = "tristram"
	LowerKurastRun      Run = "lower_kurast"
	LowerKurastChestRun Run = "lower_kurast_chest"
	StonyTombRun        Run = "stony_tomb"
	PitRun              Run = "pit"
	ArachnidLairRun     Run = "arachnid_lair"
	TalRashaTombsRun    Run = "tal_rasha_tombs"
	BaalRun             Run = "baal"
	DiabloRun           Run = "diablo"
	CowsRun             Run = "cows"
	LevelingRun         Run = "leveling"
	QuestsRun           Run = "quests"
	TerrorZoneRun       Run = "terror_zone"
	ThreshsocketRun     Run = "threshsocket"
	DrifterCavernRun    Run = "drifter_cavern"
	SpiderCavernRun     Run = "spider_cavern"
	EnduguRun           Run = "endugu"
)

var AvailableRuns = map[Run]interface{}{
	CountessRun:         nil,
	AndarielRun:         nil,
	AncientTunnelsRun:   nil,
	MausoleumRun:        nil,
	SummonerRun:         nil,
	DurielRun:           nil,
	MephistoRun:         nil,
	TravincalRun:        nil,
	EldritchRun:         nil,
	PindleskinRun:       nil,
	NihlathakRun:        nil,
	TristramRun:         nil,
	LowerKurastRun:      nil,
	LowerKurastChestRun: nil,
	StonyTombRun:        nil,
	PitRun:              nil,
	ArachnidLairRun:     nil,
	TalRashaTombsRun:    nil,
	BaalRun:             nil,
	DiabloRun:           nil,
	CowsRun:             nil,
	LevelingRun:         nil,
	QuestsRun:           nil,
	TerrorZoneRun:       nil,
	ThreshsocketRun:     nil,
	DrifterCavernRun:    nil,
	SpiderCavernRun:     nil,
	EnduguRun:           nil,
}

var RunAreas = map[Run][]area.ID{
	AncientTunnelsRun:   {area.AncientTunnels},
	ArachnidLairRun:     {area.SpiderForest, area.SpiderCave, area.SpiderCavern},
	BaalRun:             {area.ThroneOfDestruction, area.TheWorldstoneChamber},
	CountessRun:         {area.TowerCellarLevel1, area.TowerCellarLevel2, area.TowerCellarLevel3, area.TowerCellarLevel4, area.TowerCellarLevel5},
	CowsRun:             {area.MooMooFarm},
	DiabloRun:           {area.ChaosSanctuary},
	DrifterCavernRun:    {area.DrifterCavern},
	DurielRun:           {area.DurielsLair},
	EldritchRun:         {area.FrigidHighlands},
	EnduguRun:           {area.FlayerJungle},
	LevelingRun:         {}, // Special case - handled through leveling logic
	LowerKurastRun:      {area.LowerKurast},
	LowerKurastChestRun: {area.LowerKurast}, // Same area but different handling
	MausoleumRun:        {area.Mausoleum},
	MephistoRun:         {area.DuranceOfHateLevel3},
	NihlathakRun:        {area.HallsOfAnguish, area.HallsOfPain, area.HallsOfVaught},
	PindleskinRun:       {area.NihlathaksTemple},
	PitRun:              {area.PitLevel1, area.PitLevel2},
	QuestsRun:           {}, // Special case - handled through quest tracking
	SpiderCavernRun:     {area.SpiderCavern},
	StonyTombRun:        {area.StonyTombLevel1, area.StonyTombLevel2},
	SummonerRun:         {area.ArcaneSanctuary},
	TalRashaTombsRun:    {area.TalRashasTomb1, area.TalRashasTomb2, area.TalRashasTomb3, area.TalRashasTomb4, area.TalRashasTomb5, area.TalRashasTomb6, area.TalRashasTomb7},
	TerrorZoneRun:       {},                 // Handled dynamically through TerrorZone config
	ThreshsocketRun:     {area.DisusedFane},
	TravincalRun:        {area.Travincal},
	TristramRun:         {area.Tristram},
	AndarielRun:         {area.CatacombsLevel1, area.CatacombsLevel2, area.CatacombsLevel3, area.CatacombsLevel4},
}
