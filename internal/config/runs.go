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

// In case of future area development, each area must be specified with it's corresponding area as Waypoint -> Destination -> Final Point
// Example: If character is configured to run Mausoleum:
// Heads to area.ColdPlains (Waypoint) first -> teleports/walks to BurialGrounds (Destination) -> enters Mausoleum (Final Point)
var RunAreas = map[Run][]area.ID{
	AncientTunnelsRun:   {area.AncientTunnels, area.LostCity},
	ArachnidLairRun:     {area.SpiderForest, area.SpiderCave, area.SpiderCavern},
	BaalRun:             {area.ThroneOfDestruction, area.TheWorldStoneKeepLevel1, area.TheWorldStoneKeepLevel2, area.TheWorldStoneKeepLevel3, area.TheWorldstoneChamber},
	CountessRun:         {area.BlackMarsh, area.ForgottenTower, area.TowerCellarLevel1, area.TowerCellarLevel2, area.TowerCellarLevel3, area.TowerCellarLevel4, area.TowerCellarLevel5},
	CowsRun:             {area.StonyField, area.Tristram, area.MooMooFarm},
	DiabloRun:           {area.CityOfTheDamned, area.RiverOfFlame, area.ChaosSanctuary},
	DrifterCavernRun:    {area.GlacialTrail, area.DrifterCavern},
	DurielRun:           {area.CanyonOfTheMagi, area.TalRashasTomb1, area.TalRashasTomb2, area.TalRashasTomb3, area.TalRashasTomb4, area.TalRashasTomb5, area.TalRashasTomb6, area.TalRashasTomb7, area.DurielsLair},
	EldritchRun:         {area.FrigidHighlands, area.BloodyFoothills}, // Kill Shenk option does NOT work PROBLEMATIC
	EnduguRun:           {area.FlayerJungle, area.FlayerDungeonLevel1, area.FlayerDungeonLevel2, area.FlayerDungeonLevel3},
	LevelingRun:         {}, // Special case - handled through leveling logic
	LowerKurastRun:      {area.LowerKurast},
	LowerKurastChestRun: {area.LowerKurast},                                    // Same area but different handling
	MausoleumRun:        {area.ColdPlains, area.BurialGrounds, area.Mausoleum}, // PROBLEMATIC
	MephistoRun:         {area.DuranceOfHateLevel2, area.DuranceOfHateLevel3},
	NihlathakRun:        {area.HallsOfPain, area.HallsOfVaught},
	PindleskinRun:       {area.NihlathaksTemple},
	PitRun:              {area.BlackMarsh, area.TamoeHighland, area.OuterCloister, area.PitLevel1, area.PitLevel2}, // PROBLEMATIC
	QuestsRun:           {},                                                                                        // Special case - handled through quest tracking
	SpiderCavernRun:     {area.SpiderForest, area.SpiderCavern},
	StonyTombRun:        {area.DryHills, area.RockyWaste, area.StonyTombLevel1, area.StonyTombLevel2}, // PROBLEMATIC
	SummonerRun:         {area.ArcaneSanctuary},
	TalRashaTombsRun:    {area.CanyonOfTheMagi, area.TalRashasTomb1, area.TalRashasTomb2, area.TalRashasTomb3, area.TalRashasTomb4, area.TalRashasTomb5, area.TalRashasTomb6, area.TalRashasTomb7},
	TerrorZoneRun:       {},                                                              // Handled dynamically through TerrorZone config
	ThreshsocketRun:     {area.ArreatPlateau, area.DisusedFane, area.CrystallinePassage}, // Verify exact area
	TravincalRun:        {area.Travincal},
	TristramRun:         {area.ColdPlains, area.StonyField, area.Tristram}, // Cold Plains is added if char misses waypoint and teleports to Tristram
	AndarielRun:         {area.CatacombsLevel1, area.CatacombsLevel2, area.CatacombsLevel3, area.CatacombsLevel4},
}
