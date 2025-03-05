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
	CountessRun:       {area.TowerCellarLevel1, area.TowerCellarLevel2, area.TowerCellarLevel3, area.TowerCellarLevel4, area.TowerCellarLevel5},
	AndarielRun:       {area.CatacombsLevel1, area.CatacombsLevel2, area.CatacombsLevel3, area.CatacombsLevel4},
	AncientTunnelsRun: {area.AncientTunnels},
	MausoleumRun:      {area.Mausoleum},
	SummonerRun:       {area.ArcaneSanctuary},
	DurielRun:         {area.DurielsLair},
	MephistoRun:       {area.DuranceOfHateLevel3},
	TravincalRun:      {area.Travincal},
	EldritchRun:       {area.FrigidHighlands},
	PindleskinRun:     {area.NihlathaksTemple},
	NihlathakRun:      {area.HallsOfAnguish, area.HallsOfPain, area.HallsOfVaught},
	TristramRun:       {area.Tristram},
	LowerKurastRun:    {area.LowerKurast},
	StonyTombRun:      {area.StonyTombLevel1, area.StonyTombLevel2},
	PitRun:            {area.PitLevel1, area.PitLevel2},
	ArachnidLairRun:   {area.SpiderCave, area.SpiderCavern, area.SpiderForest},
	TalRashaTombsRun:  {area.TalRashasTomb1, area.TalRashasTomb2, area.TalRashasTomb3, area.TalRashasTomb4, area.TalRashasTomb5, area.TalRashasTomb6, area.TalRashasTomb7},
	BaalRun:           {area.ThroneOfDestruction, area.TheWorldstoneChamber},
	DiabloRun:         {area.ChaosSanctuary},
	CowsRun:           {area.MooMooFarm},
	DrifterCavernRun:  {area.DrifterCavern},
	SpiderCavernRun:   {area.SpiderCavern},
	EnduguRun:         {area.FlayerJungle},
}
