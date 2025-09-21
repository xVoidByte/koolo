package action

import (
	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/item"
)

func getMercenaryMetaItemScore(it data.Item) (float64, bool) {
	name := getItemNameForScore(it)

	if score, found := MercenaryMetaHelmetScore[name]; found {
		return score, found
	}

	if score, found := MercenaryMetaArmorScore[name]; found {
		return score, found
	}

	if score, found := MercenaryMetaWeaponScore[name]; found {
		return score, found
	}

	return 0.0, false
}

func getItemNameForScore(it data.Item) item.Name {
	if it.IsRuneword {
		return item.Name(it.RunewordName)
	}

	if it.IdentifiedName != "" {
		return item.Name(it.IdentifiedName)
	}

	return it.Name
}

var (
	MercenaryMetaHelmetScore = map[item.Name]float64{
		item.Name(item.AndarielsVisage):        2000.0,
		item.Name(item.Stealskull):             1900.0,
		item.Name(item.CrownofThieves):         1800.0,
		item.Name(item.TalRashasHoradricCrest): 1700.0,
		item.Name(item.Vampiregaze):            1600.0,
		item.Name(item.RunewordBulwark):        1500.0,
		item.Name(item.UndeadCrown):            1400.0,
		item.Name(item.Wormskull):              1300.0,
	}

	MercenaryMetaArmorScore = map[item.Name]float64{
		item.Name(item.RunewordTreachery): 2000.0,
		item.Name(item.RunewordHustle):    1900.0,
		item.Name(item.RunewordSmoke):     1800.0,
		item.Name(item.DurielsShell):      1700.0,
	}

	MercenaryMetaWeaponScore = map[item.Name]float64{
		item.Name(item.RunewordInsight): 2000.0,
	}
)
