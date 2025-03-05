package game

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/memory"
	"github.com/hectorgimenez/d2go/pkg/utils"
	"github.com/hectorgimenez/koolo/internal/config"
	"github.com/hectorgimenez/koolo/internal/game/map_client"
	"github.com/lxn/win"
	//"golang.org/x/sync/errgroup"
)

type MemoryReader struct {
	cfg *config.CharacterCfg
	*memory.GameReader
	mapSeed        uint
	HWND           win.HWND
	WindowLeftX    int
	WindowTopY     int
	GameAreaSizeX  int
	GameAreaSizeY  int
	supervisorName string
	cachedMapData  map[area.ID]AreaData
	logger         *slog.Logger
}

func NewGameReader(cfg *config.CharacterCfg, supervisorName string, pid uint32, window win.HWND, logger *slog.Logger) (*MemoryReader, error) {
	process, err := memory.NewProcessForPID(pid)
	if err != nil {
		return nil, err
	}

	gr := &MemoryReader{
		GameReader:     memory.NewGameReader(process),
		HWND:           window,
		supervisorName: supervisorName,
		cfg:            cfg,
		logger:         logger,
	}

	gr.updateWindowPositionData()

	return gr, nil
}

func (gd *MemoryReader) MapSeed() uint {
	return gd.mapSeed
}

func (gd *MemoryReader) getRequiredAreas() map[area.ID]bool {
	required := make(map[area.ID]bool)

	// Always include all town areas
	required[area.RogueEncampment] = true
	required[area.LutGholein] = true
	required[area.KurastDocks] = true
	required[area.ThePandemoniumFortress] = true
	required[area.Harrogath] = true

	if gd.cfg == nil {
		return required
	}

	// Add areas for each configured run
	for _, run := range gd.cfg.Game.Runs {
		// Handle terror zone areas separately
		if run == config.TerrorZoneRun {
			for _, a := range gd.cfg.Game.TerrorZone.Areas {
				required[area.ID(a)] = true
			}
			continue
		}

		// Get areas from centralized configuration
		if areas, exists := config.RunAreas[run]; exists {
			for _, a := range areas {
				required[a] = true
			}
		}
	}

	gd.logger.Debug("Computed required areas", slog.Any("areas", required))
	return required
}

func (gd *MemoryReader) FetchMapData() error {
	d := gd.GameReader.GetData()
	currentAreaID := d.PlayerUnit.Area
	gd.mapSeed, _ = gd.getMapSeed(d.PlayerUnit.Address)
	t := time.Now()
	gd.logger.Debug("Fetching map data...", slog.Uint64("seed", uint64(gd.mapSeed)), slog.String("difficulty", string(config.Characters[gd.supervisorName].Game.Difficulty)))

	mapData, err := map_client.GetMapData(strconv.Itoa(int(gd.mapSeed)), config.Characters[gd.supervisorName].Game.Difficulty)
	if err != nil {
		return fmt.Errorf("error fetching map data: %w", err)
	}

	requiredAreas := gd.getRequiredAreas()
	areas := make(map[area.ID]AreaData)
	var mu sync.Mutex
	var wg sync.WaitGroup

	gd.logger.Debug("Processing map data",
		slog.Int("total_areas", len(mapData)),
		slog.Int("required_areas", len(requiredAreas)),
		slog.Any("required", requiredAreas),
	)

	for _, lvl := range mapData {
		lvl := lvl // Capture loop variable
		areaID := area.ID(lvl.ID)
		if !requiredAreas[areaID] {
			gd.logger.Debug("Skipping unrequired area",
				slog.String("name", lvl.Name),
				slog.Int("id", lvl.ID),
			)
			continue // Skip non-required areas
		}

		if areaID == currentAreaID {
			// Process current area synchronously
			processLevel(lvl, areas, &mu)
		} else {
			// Process other areas in goroutines
			wg.Add(1)
			go func() {
				defer wg.Done()
				processLevel(lvl, areas, &mu)
			}()
		}
	}

	wg.Wait() // Wait for all background goroutines to finish

	gd.cachedMapData = areas
	gd.logger.Debug("Fetch completed", slog.Int64("ms", time.Since(t).Milliseconds()))
	return nil
}

// Helper function to process a single level's data
func processLevel(lvl map_client.ServerLevel, areas map[area.ID]AreaData, mu *sync.Mutex) {
	cg := lvl.CollisionGrid()
	resultGrid := make([][]CollisionType, lvl.Size.Height)
	for i := range resultGrid {
		resultGrid[i] = make([]CollisionType, lvl.Size.Width)
	}

	for y := 0; y < lvl.Size.Height; y++ {
		for x := 0; x < lvl.Size.Width; x++ {
			if cg[y][x] {
				resultGrid[y][x] = CollisionTypeWalkable
			} else {
				resultGrid[y][x] = CollisionTypeNonWalkable
			}
		}
	}

	npcs, exits, objects, rooms := lvl.NPCsExitsAndObjects()
	grid := NewGrid(resultGrid, lvl.Offset.X, lvl.Offset.Y)

	mu.Lock()
	areas[area.ID(lvl.ID)] = AreaData{
		Area:           area.ID(lvl.ID),
		Name:           lvl.Name,
		NPCs:           npcs,
		AdjacentLevels: exits,
		Objects:        objects,
		Rooms:          rooms,
		Grid:           grid,
	}
	mu.Unlock()
}

func (gd *MemoryReader) updateWindowPositionData() {
	pos := win.WINDOWPLACEMENT{}
	point := win.POINT{}
	win.ClientToScreen(gd.HWND, &point)
	win.GetWindowPlacement(gd.HWND, &pos)

	gd.WindowLeftX = int(point.X)
	gd.WindowTopY = int(point.Y)
	gd.GameAreaSizeX = int(pos.RcNormalPosition.Right) - gd.WindowLeftX - 9
	gd.GameAreaSizeY = int(pos.RcNormalPosition.Bottom) - gd.WindowTopY - 9
}

func (gd *MemoryReader) GetData() Data {
	d := gd.GameReader.GetData()
	currentArea, ok := gd.cachedMapData[d.PlayerUnit.Area]
	if ok {
		// This hacky thing is because sometimes if the objects are far away we can not fetch them, basically WP.
		memObjects := gd.Objects(d.PlayerUnit.Position, d.HoverData)
		for _, clientObject := range currentArea.Objects {
			found := false
			for _, obj := range memObjects {
				// Only consider it a duplicate if same name AND same position
				if obj.Name == clientObject.Name && obj.Position.X == clientObject.Position.X && obj.Position.Y == clientObject.Position.Y {
					found = true
					break
				}
			}
			if !found {
				memObjects = append(memObjects, clientObject)
			}
		}

		d.AreaOrigin = data.Position{X: currentArea.OffsetX, Y: currentArea.OffsetY}
		d.NPCs = currentArea.NPCs
		d.AdjacentLevels = currentArea.AdjacentLevels
		d.Rooms = currentArea.Rooms
		d.Objects = memObjects
	}

	var cfgCopy config.CharacterCfg
	if gd.cfg != nil {
		cfgCopy = *gd.cfg
	}

	return Data{
		Data:         d,
		CharacterCfg: cfgCopy,
		AreaData:     currentArea,
		Areas:        gd.cachedMapData,
	}
}

func (gd *MemoryReader) getMapSeed(playerUnit uintptr) (uint, error) {
	actPtr := uintptr(gd.Process.ReadUInt(playerUnit+0x20, memory.Uint64))
	actMiscPtr := uintptr(gd.Process.ReadUInt(actPtr+0x78, memory.Uint64))

	dwInitSeedHash1 := gd.Process.ReadUInt(actMiscPtr+0x840, memory.Uint32)
	//dwInitSeedHash2 := uintptr(gd.Process.ReadUInt(actMiscPtr+0x844, memory.Uint32))
	dwEndSeedHash1 := gd.Process.ReadUInt(actMiscPtr+0x868, memory.Uint32)

	mapSeed, found := utils.GetMapSeed(dwInitSeedHash1, dwEndSeedHash1)
	if !found {
		return 0, errors.New("error calculating map seed")
	}

	return mapSeed, nil
}
