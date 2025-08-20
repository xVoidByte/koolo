package run

import (


	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/config"
	"github.com/hectorgimenez/koolo/internal/context"

)

type ClearJail struct {
	ctx *context.Status
}

func NewClearJail() *ClearJail {
	return &ClearJail{
		ctx: context.Get(),
	}
}

func (t ClearJail) Name() string {
	return string(config.TristramRun)
}

func (t ClearJail) Run() error {

	// Use waypoint to JailLevel1
	err := action.WayPoint(area.JailLevel1)
	if err != nil {
		return err
	}
	
	return action.ClearCurrentLevel(false, data.MonsterAnyFilter())
	//return action.ClearCurrentLevel(false, data.MonsterEliteFilter())
	
	//return action.ClearAreaAroundPlayer(250, data.MonsterEliteFilter())

}
