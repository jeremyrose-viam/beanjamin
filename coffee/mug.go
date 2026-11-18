package coffee

import (
	"context"
	"fmt"
)

// stagedMugFrameName is the World-parented obstacle frame for a mug set down at
// the hot water spout while the arm is away operating the hot water switch.
const stagedMugFrameName = "staged-mug"

// slideMugToWater carries the full mug from under the group head to the hot
// water spout (mug_ready_for_water, 32 mm to the arm's right) along a straight
// line, slowly because the mug is full.
func (s *beanjaminCoffee) slideMugToWater(ctx, cancelCtx context.Context) error {
	step := Step{PoseName: clawPoseMugReadyForWater, PoseSwitch: s.clawsSw, LinearConstraint: defaultApproachConstraint,
		MoveOptions: s.slowMoveOptions(), AllowedCollisions: s.scene().heldItemSurfaceCollisions(heldItemMachineCollisions)}
	if err := s.executeStep(ctx, cancelCtx, step); err != nil {
		return fmt.Errorf("slide_mug_to_water: %w", err)
	}
	return nil
}

// setMugAtWater sets the held mug down at the spout and backs the arm away,
// leaving the mug as a static world obstacle (staged-mug) so the hot water
// moves route around it. The reverse is regrabMugAtWater.
func (s *beanjaminCoffee) setMugAtWater(ctx, cancelCtx context.Context) error {
	if s.gripper == nil {
		return fmt.Errorf("set_mug_at_water: no gripper configured")
	}
	if err := s.openGripper(ctx); err != nil {
		return fmt.Errorf("set_mug_at_water: %w", err)
	}
	// The mug is set down; keep it as a static world obstacle so the hot water
	// moves route around it.
	if err := s.stageMugAsObstacle(ctx); err != nil {
		return fmt.Errorf("set_mug_at_water: %w", err)
	}
	// The gripper is empty, so the arm's default speed.
	exitStep := Step{PoseName: clawPoseMugWaterApproach, PoseSwitch: s.clawsSw, LinearConstraint: defaultApproachConstraint}
	if err := s.executeStep(ctx, cancelCtx, exitStep); err != nil {
		return fmt.Errorf("set_mug_at_water: %w", err)
	}
	if err := s.closeGripper(ctx); err != nil {
		return fmt.Errorf("set_mug_at_water: %w", err)
	}
	return nil
}

// hotWaterRoutineWired says whether dispenseHotWater is the real routine. While
// it is false, a hot_water order serves an empty mug and an americano is just a
// lungo, both reported as success, so can_serve_mug_drinks must stay off for
// customers. Flip it when the real routine replaces the stub.
const hotWaterRoutineWired = false

// dispenseHotWater is the hot water routine: a stub until the real one is wired.
//
// The contract for the real routine: it keeps the arm for the whole pour (it
// turns the water on, waits a set time, and turns it off); it receives the drink
// id so it can tell a top-off (americano, the mug already holds espresso) from a
// full mug (hot water alone); and it must start and end with the arm clear of
// the mug, since the mug stands staged at the spout throughout.
func (s *beanjaminCoffee) dispenseHotWater(ctx, cancelCtx context.Context, drink string) error {
	s.activeOrderLogger().Warnf("dispense_hot_water: no hot water routine wired yet (drink %s); skipping", drink)
	return nil
}

// regrabMugAtWater picks the staged mug back up and carries it clear of the
// spout: approach, open, slow linear descent onto the mug, grab, then a slow
// linear retreat with the mug in hand. The reverse of setMugAtWater.
func (s *beanjaminCoffee) regrabMugAtWater(ctx, cancelCtx context.Context) error {
	if s.gripper == nil {
		return fmt.Errorf("regrab_mug_at_water: no gripper configured")
	}
	approachStep := Step{PoseName: clawPoseMugWaterApproach, PoseSwitch: s.clawsSw}
	if err := s.executeStep(ctx, cancelCtx, approachStep); err != nil {
		return fmt.Errorf("regrab_mug_at_water: %w", err)
	}
	if err := s.openGripper(ctx); err != nil {
		return fmt.Errorf("regrab_mug_at_water: %w", err)
	}
	// Descend onto the mug; it stays a world obstacle, but the jaws may touch it
	// for this step. Slow, to settle onto the handle-side grasp gently.
	grabStep := Step{PoseName: clawPoseMugReadyForWater, PoseSwitch: s.clawsSw, LinearConstraint: defaultApproachConstraint, Pause: shortPause,
		MoveOptions: s.slowMoveOptions(), AllowedCollisions: s.scene().stagedGrabCollisions(stagedMug)}
	if err := s.executeStep(ctx, cancelCtx, grabStep); err != nil {
		return fmt.Errorf("regrab_mug_at_water: %w", err)
	}
	if err := s.grabAndVerifyHolding(ctx, "mug at water"); err != nil {
		return fmt.Errorf("regrab_mug_at_water: grab gripper: %w", err)
	}
	// The mug is back in the gripper: move it from the world obstacle to a held item.
	if err := s.regrabStagedMug(ctx); err != nil {
		s.activeOrderLogger().Warnf("regrab_mug_at_water: reattach mug geometry failed, continuing untracked: %v", err)
	}
	retreatStep := Step{PoseName: clawPoseMugWaterApproach, PoseSwitch: s.clawsSw, LinearConstraint: defaultApproachConstraint,
		MoveOptions: s.slowMoveOptions(), AllowedCollisions: s.scene().heldItemSurfaceCollisions(heldItemMachineCollisions)}
	if err := s.executeStep(ctx, cancelCtx, retreatStep); err != nil {
		return fmt.Errorf("regrab_mug_at_water: %w", err)
	}
	return nil
}

// placeMugAtWater is the hot-water-alone path: vision-grab an empty mug off the
// shelf and set it straight down at the spout.
func (s *beanjaminCoffee) placeMugAtWater(ctx, cancelCtx context.Context) error {
	if err := s.pickMugDynamic(ctx, cancelCtx); err != nil {
		return err
	}
	approachStep := Step{PoseName: clawPoseMugWaterApproach, PoseSwitch: s.clawsSw}
	if err := s.executeStep(ctx, cancelCtx, approachStep); err != nil {
		return fmt.Errorf("place_mug_at_water: %w", err)
	}
	// The mug is empty, so the arm's default speed.
	readyStep := Step{PoseName: clawPoseMugReadyForWater, PoseSwitch: s.clawsSw, LinearConstraint: defaultApproachConstraint, Pause: shortPause,
		AllowedCollisions: s.scene().heldItemSurfaceCollisions(heldItemMachineCollisions)}
	if err := s.executeStep(ctx, cancelCtx, readyStep); err != nil {
		return fmt.Errorf("place_mug_at_water: %w", err)
	}
	return s.setMugAtWater(ctx, cancelCtx)
}

// stageMugAsObstacle is setMugAtWater's release path: it turns the held mug into
// the static staged-mug obstacle. No-op when nothing is attached.
func (s *beanjaminCoffee) stageMugAsObstacle(ctx context.Context) error {
	return s.stageAsObstacle(ctx, stagedMug)
}

// regrabStagedMug moves the staged mug from the world back into the gripper once
// the jaws have closed on it, modeling the grasp from where the mug stands.
func (s *beanjaminCoffee) regrabStagedMug(ctx context.Context) error {
	return s.regrabStaged(ctx, stagedMug)
}
