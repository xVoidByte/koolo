package step

import (
	"fmt"
	"math"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/skill"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/utils"
)

const (
	DistanceToFinishMoving = 4
	idleThreshold          = 1500 * time.Millisecond
	maxMovementTimeout     = 30 * time.Second
	sleepInterval          = 100 * time.Millisecond
)

type MoveOpts struct {
	distanceOverride *int
}

type MoveOption func(*MoveOpts)

// WithDistanceToFinish overrides the default DistanceToFinishMoving
func WithDistanceToFinish(distance int) MoveOption {
	return func(opts *MoveOpts) {
		opts.distanceOverride = &distance
	}
}

func MoveTo(dest data.Position, options ...MoveOption) error {
	ctx := context.Get()
	ctx.SetLastStep("MoveTo")

	opts := &MoveOpts{}
	for _, o := range options {
		o(opts)
	}

	minDistanceToFinish := DistanceToFinishMoving
	if opts.distanceOverride != nil {
		minDistanceToFinish = *opts.distanceOverride
	}

	startedAt := time.Now()
	lastRun := time.Time{}
	previousPosition := data.Position{}
	previousDistance := 0
	idleStartTime := time.Time{}
	var currentDistance int

	for {
		ctx.PauseIfNotPriority()
		ctx.RefreshGameData()

		// Timeout check
		if time.Since(startedAt) > maxMovementTimeout {
			return nil
		}

		currentDistance = ctx.PathFinder.DistanceFromMe(dest)
		if currentDistance <= minDistanceToFinish {
			return nil
		}

		// Position tracking
		playerPos := ctx.Data.PlayerUnit.Position
		if playerPos == previousPosition {
			if idleStartTime.IsZero() {
				idleStartTime = time.Now()
			} else if time.Since(idleStartTime) > idleThreshold {
				handleIdle(ctx)
				idleStartTime = time.Time{}
				previousPosition = playerPos
				continue
			}
		} else {
			idleStartTime = time.Time{}
			previousPosition = playerPos
		}

		// Skill management
		manageMovementSkills(ctx)

		// Path calculation
		path, distance, found := ctx.PathFinder.GetPath(dest)
		if !found {
			if currentDistance < minDistanceToFinish+5 {
				return nil
			}
			return pathfindingError(ctx, dest)
		}

		// Dynamic distance adjustment (only when no override set)
		if opts.distanceOverride == nil {
			if distance < 20 && math.Abs(float64(previousDistance-distance)) < DistanceToFinishMoving {
				minDistanceToFinish += DistanceToFinishMoving
			} else {
				minDistanceToFinish = DistanceToFinishMoving
			}
		}

		// Movement execution
		if !executeMovement(ctx, dest, &lastRun, path, distance, &minDistanceToFinish, startedAt, &previousPosition, &idleStartTime) {
			return nil
		}
		previousDistance = distance
	}
}

// handleIdle triggers random movement when player is stuck
func handleIdle(ctx *context.Status) {
	ctx.Logger.Debug("Anti-idle triggered")
	ctx.PathFinder.RandomMovement()
	time.Sleep(150 * time.Millisecond)
}

// manageMovementSkills sets appropriate movement skills (Teleport/Vigor)
func manageMovementSkills(ctx *context.Status) {
	if ctx.Data.CanTeleport() {
		if ctx.Data.PlayerUnit.RightSkill != skill.Teleport {
			ctx.HID.PressKeyBinding(ctx.Data.KeyBindings.MustKBForSkill(skill.Teleport))
		}
		return
	}

	if kb, found := ctx.Data.KeyBindings.KeyBindingForSkill(skill.Vigor); found && ctx.Data.PlayerUnit.RightSkill != skill.Vigor {
		ctx.HID.PressKeyBinding(kb)
	}
}

// pathfindingError returns formatted pathfinding failure error
func pathfindingError(ctx *context.Status, dest data.Position) error {
	return fmt.Errorf("pathfinding failed in %s to %d,%d",
		ctx.Data.PlayerUnit.Area.Area().Name,
		dest.X,
		dest.Y,
	)
}

// executeMovement handles the actual movement execution with proper cooldown management
func executeMovement(
	ctx *context.Status,
	dest data.Position,
	lastRun *time.Time,
	path []data.Position,
	distance int,
	minDistance *int,
	startedAt time.Time,
	previousPosition *data.Position,
	idleStartTime *time.Time,
) bool {
	walkDuration := utils.RandomDurationMs(600, 1200)

	// Handle walking cooldown
	if !ctx.Data.CanTeleport() && time.Since(*lastRun) < walkDuration {
		return handleWalkCooldown(ctx, dest, lastRun, walkDuration, minDistance, startedAt, previousPosition, idleStartTime)
	}

	// Handle teleport cooldown
	if ctx.Data.CanTeleport() {
		// Use the actual calculated cast duration without artificial division
		remainingWait := ctx.Data.PlayerCastDuration() - time.Since(*lastRun)

		if remainingWait > 0 {
			return handleTeleportCooldown(ctx, dest, remainingWait, minDistance, startedAt, previousPosition, idleStartTime)
		}
	}

	// Execute the actual movement
	ctx.PathFinder.MoveThroughPath(path, walkDuration)
	*lastRun = time.Now()
	return true
}

// handleWalkCooldown manages walking cooldown with periodic checks
func handleWalkCooldown(
	ctx *context.Status,
	dest data.Position,
	lastRun *time.Time,
	walkDuration time.Duration,
	minDistance *int,
	startedAt time.Time,
	previousPosition *data.Position,
	idleStartTime *time.Time,
) bool {
	remainingSleep := walkDuration - time.Since(*lastRun)
	for remainingSleep > 0 {
		sleepTime := minDuration(sleepInterval, remainingSleep)
		time.Sleep(sleepTime)
		remainingSleep -= sleepTime

		if checkEarlyExit(ctx, dest, minDistance, startedAt, previousPosition, idleStartTime) {
			return false
		}
	}
	return true
}

// handleTeleportCooldown manages teleport cooldown with periodic checks
func handleTeleportCooldown(
	ctx *context.Status,
	dest data.Position,
	remainingWait time.Duration,
	minDistance *int,
	startedAt time.Time,
	previousPosition *data.Position,
	idleStartTime *time.Time,
) bool {
	for remainingWait > 0 {
		sleepTime := minDuration(sleepInterval, remainingWait)
		time.Sleep(sleepTime)
		remainingWait -= sleepTime

		if checkEarlyExit(ctx, dest, minDistance, startedAt, previousPosition, idleStartTime) {
			return false
		}
	}
	return true
}

// checkEarlyExit verifies exit conditions during cooldown periods
func checkEarlyExit(
	ctx *context.Status,
	dest data.Position,
	minDistance *int,
	startedAt time.Time,
	previousPosition *data.Position,
	idleStartTime *time.Time,
) bool {

	// Timeout check
	if time.Since(startedAt) > maxMovementTimeout {
		return true
	}

	// Distance check
	currentDistance := ctx.PathFinder.DistanceFromMe(dest)
	if currentDistance <= *minDistance {
		return true
	}

	// Idle detection
	currentPos := ctx.Data.PlayerUnit.Position
	if currentPos == *previousPosition {
		if idleStartTime.IsZero() {
			*idleStartTime = time.Now()
		} else if time.Since(*idleStartTime) > idleThreshold {
			handleIdle(ctx)
			*idleStartTime = time.Time{}
			*previousPosition = currentPos
		}
	} else {
		*previousPosition = currentPos
		*idleStartTime = time.Time{}
	}

	return false
}

// minDuration returns the smaller of two durations
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
