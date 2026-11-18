# Mug Drinks (Americano and Hot Water) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The coffee service picks a ceramic mug off its own shelf by vision and serves an Americano (lungo into the mug, then topped off at the hot water dispenser) or plain hot water, with an empty hook for the hot water routine.

**Architecture:** The mug is a third vision-picked container beside the cup and glass (`pickupTarget`). Its collision model is two boxes (body + handle) carried through the existing held-item and staged-obstacle machinery, which changes from one geometry to a list. The cup's place-under-spout and retrieve steps take a small `vessel` profile so cup and mug share them; new steps in `coffee/mug.go` move the full mug to the dispenser, set it down, call the hook, and re-grab it. Two new drink ids are gated by `can_serve_mug_drinks`.

**Tech Stack:** Go, Viam RDK v1.15 (`referenceframe`, `spatialmath`, motion), the repo's `make test` / `make lint`.

**Spec:** `docs/superpowers/specs/2026-11-18-mug-drinks-design.md` (shared copy: https://claude.ai/artifact/LnwkVjRV2vTYThCmA6nPid)

## Global Constraints

- Cup and iced drinks must behave exactly as today. Every existing test passes unchanged; where a signature changes, cup and glass callers pass a one-item list.
- Every move with liquid in the mug is slow (`s.slowMoveOptions()`) and linear or level-carried.
- New config fields are `omitempty`; all mug fields except the flag and the two serving overrides are required when `can_serve_mug_drinks` is set.
- New drink ids are exactly `americano` and `hot_water`. `Supports` reason when off: `can_serve_mug_drinks=false`.
- New usage counter key: `mugs_used` (+1 per mug drink, at placement). Mug drinks never increment `espresso_cups_used`.
- Item label: `pickupLabelMug = "mug"`.
- The hot water spout is about 32 mm right of the portafilter (world Y −32); the slide is that translation.
- Claws poses: `mug_under_machine_approach`, `mug_ready_for_coffee`, `mug_water_approach`, `mug_ready_for_water`. Observe home pose: `mug_observe`. Shield obstacle frame: `clean-mug-area-shield`. Staged obstacle frame: `staged-mug`.
- Handle box = mug's full height (`mug_dimensions.height_mm`), `depth_mm` out from the body, `width_mm` across; flush with the body on the arm's right: 90° clockwise (seen from above) from the grab yaw. The right jaw passes through the handle's loop.
- `dispenseHotWater(ctx, cancelCtx context.Context, drink string) error` is a stub that logs and returns nil. The real routine keeps the arm for the whole pour (on, wait, off) and must start and end with the arm clear of the mug.
- Keep each `setStep(...)` label in sync with its `logger.Infof("step N/M: ...")` line (CLAUDE.md).
- Update README for every new config field, action and counter (CLAUDE.md). Run `make lint` before each commit.

## Review Focus

1. **A cup order right after a mug order.** Held-geometry caches are per label; a stale mug list must never be reattached to a cup. Expect the cup path to model exactly one box. (Test in Task 2.)
2. **Cancel or fault with the mug standing at the dispenser.** `strandedState` must name it, and `resetFrameSystem` must clear the `staged-mug` obstacle so the next order doesn't plan around a ghost. (Test in Task 6.)
3. **`can_serve_mug_drinks` off but a mug drink ordered** through `prepare_order` or an old client. Must be rejected at intake with the flag reason, never reach `prepareDrink`. (Test in Task 8.)
4. **Serving a mug with no mug serving overrides set.** Falls back to the shared serving offsets, but the drop height still uses the mug's grip height above its base, not its half-height, so a mug gripped off-centre isn't released in the air or driven into the shelf. (Test in Task 7.)
5. **Mug ready poses at different yaws.** The slide is meant to be a pure translation; startup logs a warning naming both poses and the angle when they differ by more than 5°. (Test in Task 3.)

---

### Task 1: Mug geometry in `geom`

**Files:**
- Modify: `coffee/geom/pickup.go`
- Test: `coffee/geom/pickup_test.go`

**Interfaces:**
- Produces: `func MugGeometries(centroid, bodyDims r3.Vector, handleDepthMm, handleWidthMm, yawRad float64, label string) ([]spatialmath.Geometry, error)` — returns `[body, handle]`. Body = `ContainerBox(centroid, bodyDims, yawRad, label)`. Handle box (**revised**: handle on the arm's right): dims `{X: handleWidthMm, Y: handleDepthMm, Z: bodyDims.Z}` in the body's frame (width along the grab heading, depth out to the right), same orientation as the body, centre at `centroid + R(yaw)·(0, −(bodyDims.Y/2 + handleDepthMm/2), 0)` — i.e. 90° clockwise from the grab heading, seen from above. Label `label + "-handle"`.

> **Revision note:** Task 1 was first built with the handle on the far side (`+heading`, commit 283c8ba). Task 1b re-points it to the right; Task 1b's brief is this task text.

- [ ] **Step 1: Write the failing test** `TestMugGeometriesHandleOnFarSide`: for yaw in {0, π/2, π, −π/4}, with centroid (100, 200, 50), body (80, 80, 97), handle depth 39 width 15: two geometries; geos[0] centre == centroid; geos[1] centre == centroid + (cos yaw, sin yaw, 0)·(40+19.5) within 1e-6; handle box dims (39, 15, 97) via `ToProtobuf().GetBox().DimsMm`; handle Z centre == 50 (full height, same as body).

  `GraspYaw` returns the heading the tool +Z points along when approaching (yaw 0 approaches along +X). The arm's right, seen from above with +Z up, is that heading turned −90°: for yaw 0 the handle is at −Y, for yaw π/2 at +X. Assert both.

- [ ] **Step 2: Run** `go test ./coffee/geom -run TestMugGeometries -v` — expect FAIL (undefined).
- [ ] **Step 3: Implement `MugGeometries`** in `coffee/geom/pickup.go`, beside `ContainerBox`.
- [ ] **Step 4: Run** the test — expect PASS. Run `go test ./coffee/geom/...` — PASS.
- [ ] **Step 5: Commit** `feat(geom): model a mug as body plus handle boxes`

### Task 1b: Re-point the handle to the arm's right

Re-implement Task 1's revised handle placement on top of commit 283c8ba: update `MugGeometries` and `TestMugGeometriesHandleOnFarSide` (rename it `TestMugGeometriesHandleOnRight`) to the revised Task 1 text. Commit `fix(geom): put the mug handle on the arm's right`.

### Task 2: Held items carry a list of geometries

**Files:**
- Modify: `coffee/held_geometry.go`, `coffee/module.go` (cache field), `coffee/cup_pickup.go` (label const only)
- Test: `coffee/held_geometry_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `const pickupLabelMug = "mug"` (in `cup_pickup.go` beside the others).
  - `func (s *beanjaminCoffee) attachDetectedGeometries(ctx context.Context, label string, geomsWorld []spatialmath.Geometry) error` — today's `attachDetectedGeometry` body, generalized; transforms all, caches the list. `attachDetectedGeometry(ctx, label, g)` stays as a one-line wrapper (nil g → no-op) so cup and glass callers don't change.
  - `addHeldItemFrame(gripperLocal []spatialmath.Geometry) error` — uses `referenceframe.NewStaticFrameWithGeometries`; frame pose from `heldItemFramePose(gripperLocal[0])`.
  - Cache: `cachedHeldGeometry(label) []spatialmath.Geometry`, `cacheHeldGeometry(label, []spatialmath.Geometry)`; new slot `heldMugGeom []spatialmath.Geometry` on the service (module.go), routed by `pickupLabelMug`, cleared in `clearHeldGeometry`. `default` stays the cup.
  - Body first: every list's element 0 is the container body. `heldItemHalfHeightMm` keeps reading `geos[0]`.
  - `func (s *beanjaminCoffee) heldItemLabel() string` — label of `geos[0]` of the held-item frame, "" when none.
  - Staging generalized: `stageHeldItem(fsInputs, frameName string) error` seats on the surface using the **body** (`geom.SeatOnSurface` on geos[0]) and shifts every geometry by the same dz, then adds one World-parented static frame `frameName` with all geometries. `stageHeldGlass(fsInputs)` becomes a wrapper (`stagedGlassFrameName`, sets `stagedGlassPlaced`). Its read-back for the re-grab (`stagedGlassGeometry`) returns the list.

- [ ] **Step 1: Write failing tests**
  - `TestAttachKeepsEveryGeometry`: attach two boxes labelled "mug"/"mug-handle"; the held-item frame's `Geometries` has 2; `cachedHeldGeometry(pickupLabelMug)` has 2; `heldItemLabel()` == "mug".
  - `TestCupAfterMugUsesCupCache` (Review Focus 1): attach a mug list, then `cacheHeldGeometry(pickupLabelCup, oneBox)`, `detachHeldGeometry()`, `reattachGeometry(pickupLabelCup)` → held-item frame has exactly 1 geometry labelled "cup".
  - `TestStageHeldItemShiftsHandleWithBody`: held mug list released 10 mm above a modeled surface; after staging, both geometries moved down by the same dz and the frame carries 2.
  - Existing tests in this file must keep passing after updating only their calls to the new list types (`TestReattachUsesCache`, `TestCacheRoutingByLabel`, swap-filter tests).
- [ ] **Step 2: Run** `go test ./coffee -run 'TestAttach|TestCupAfterMug|TestStageHeldItem|TestReattach|TestCacheRouting|TestSwapFilter|TestHeldItem' -v` — new ones FAIL.
- [ ] **Step 3: Implement** the interfaces above. `swapFilterForGlass` keeps a `[]spatialmath.Geometry` copy of the glass cache.
- [ ] **Step 4: Run** the same command, then `make test` — PASS.
- [ ] **Step 5: Commit** `refactor(coffee): held and staged items carry a geometry list`

### Task 3: Mug config, wiring and required poses

**Files:**
- Modify: `coffee/config.go`, `coffee/module.go`, `coffee/poses.go`, `README.md` (coffee model config table + new "Mug drinks" subsection)
- Test: `coffee/config_test.go`, `coffee/poses_test.go`

**Interfaces:**
- Produces (config, all `omitempty`):
  - `CanServeMugDrinks bool` `can_serve_mug_drinks`
  - `MugVisionServiceName string` `mug_vision_service_name`
  - `MugObservePoseSwitcherName string` `mug_observe_pose_switcher_name`
  - `MugApproachRelativePose, MugGrabRelativePose *RelativePose` `mug_approach_relative_pose`, `mug_grab_relative_pose`
  - `MugDimensions *ContainerDimensions` `mug_dimensions`
  - `MugHandleDimensions *HandleDimensions` `mug_handle_dimensions`, with `type HandleDimensions struct { DepthMm float64 \`json:"depth_mm"\`; WidthMm float64 \`json:"width_mm"\` }` and `func (h *HandleDimensions) validate(path, field string) error` (present, both > 0)
  - `MugServingGrabRelativePose, MugServingApproachRelativePose *RelativePose` `mug_serving_grab_relative_pose`, `mug_serving_approach_relative_pose` (optional)
- Validate, when the flag is set: `requireFields` for vision, switcher, approach, grab; `MugDimensions.validate`, `MugHandleDimensions.validate`; append `vision.Named(MugVisionServiceName).String()` and the switcher to `reqDeps`. Mirrors the `CanServeIced` block.
- module.go: `mugVision vision.Service`, `mugObserveSw toggleswitch.Switch`, filled by `visionPickup(deps, logger, "mug", ...)` only when the flag is set.
- poses.go: constants `clawPoseMugUnderMachineApproach = "mug_under_machine_approach"`, `clawPoseMugReadyForCoffee = "mug_ready_for_coffee"`, `clawPoseMugWaterApproach = "mug_water_approach"`, `clawPoseMugReadyForWater = "mug_ready_for_water"`, `mugPoseObserve = "mug_observe"`; `requiredPoses` adds the four claws poses and `{s.mugObserveSw, mugPoseObserve}` when the flag is set.
- poses.go: `func mugReadyYawMismatchDeg(coffee, water spatialmath.Pose) float64` (absolute difference of `geom.GraspYaw` in degrees, wrapped to [0,180]); `validateConfiguredPoses` logs a Warn naming both poses and the angle when > 5 (`mugReadyYawToleranceDeg = 5.0`).

- [ ] **Step 1: Write failing tests**
  - `TestValidateMugDrinksRequiresFields`: flag on with each required field missing in turn → error naming that field; all present → no error and deps include the mug vision service and switcher; flag off with no mug fields → no error.
  - `TestHandleDimensionsValidate`: nil, zero depth, zero width → error; (39, 15) → nil.
  - In `TestRequiredPosesConditional`: base config does not require `mug_observe` or any mug claws pose; `CanServeMugDrinks: true` requires all five.
  - `TestMugReadyYawMismatchDeg` (Review Focus 5): same orientation → 0; yaws 10° and 20° → 10; 175° and −175° → 10.
- [ ] **Step 2: Run** `go test ./coffee -run 'TestValidateMug|TestHandleDimensions|TestRequiredPoses|TestMugReadyYaw' -v` — FAIL.
- [ ] **Step 3: Implement** the interfaces; add README rows (from the spec's Config table) and the required-poses note. The `mug_grab_relative_pose` row carries the grip-height arithmetic, so operators can set z without reading code: the offset is added to the container's centre, which the pickup seats at `1 mm + height_mm / 2` above the shelf; grip height above the shelf = `1 + height_mm / 2 + z`. Worked example in the row: the mug is gripped at its middle so the right jaw goes through the handle's loop, so z = 0 → 1 + 48.5 + 0 = 49.5 mm above the shelf (a cup: 1 + 30 + 6 = 37 mm). The `mug_approach_relative_pose` row says to use the same z, so the jaw enters the loop level.
- [ ] **Step 4: Run** the tests, then `make test` — PASS.
- [ ] **Step 5: Commit** `feat(coffee): mug drink config, wiring and required poses`

### Task 4: Mug pickup

**Files:**
- Modify: `coffee/cup_pickup.go`, `coffee/actions.go`
- Test: `coffee/cup_pickup_test.go`

**Interfaces:**
- Consumes: `geom.MugGeometries` (Task 1), `attachDetectedGeometries` and `pickupLabelMug` (Task 2), config and `s.mugVision`/`s.mugObserveSw` (Task 3).
- Produces:
  - `pickupTarget.handle *HandleDimensions` (nil for cup and glass).
  - `const cleanMugAreaShieldFrameName = "clean-mug-area-shield"`.
  - `func (s *beanjaminCoffee) mugPickupTarget() *pickupTarget` — label `pickupLabelMug`, vision `s.mugVision`, camera `s.cupCameraName`, observe `s.mugObserveSw`, home `mugPoseObserve`, offsets `MugApproachRelativePose`/`MugGrabRelativePose`, `maxAttempts: pickupMaxAttempts(s.cfg.CupPickupMaxAttempts)`, dims `MugDimensions`, handle `MugHandleDimensions`, shield `cleanMugAreaShieldFrameName`, retreat speed nil. Prompts, exactly:
    - `noItemSpeak`: "I don't see a mug yet — please place one on the mug shelf. Trying again in 15 seconds."
    - `unreachableSpeak`: "I can see a mug but I'm having trouble grabbing it — could you nudge it a little? Trying again in 15 seconds."
  - `func (s *beanjaminCoffee) pickMugDynamic(ctx, cancelCtx context.Context) error`.
  - `func (t *pickupTarget) heldGeometries(cand geom.Candidate) ([]spatialmath.Geometry, error)` — `[cand.Geom]` when `t.handle == nil`; otherwise `geom.MugGeometries(cand.Geom.Pose().Point(), t.dims.boxDims(), t.handle.DepthMm, t.handle.WidthMm, containerYaw(t.grabRel), t.label)`. `tryGrab` attaches its result with `attachDetectedGeometries`.
  - `execute_action` name `pick_mug` → `pickMugDynamic`.

- [ ] **Step 1: Write failing tests**
  - `TestHeldGeometriesCupIsOneBox`: cup target → 1 geometry, the candidate's own.
  - `TestHeldGeometriesMugAddsHandle`: mug target with handle (39, 15), dims (80, 97), grab yaw 0 → 2 geometries; handle centre Y = candidate Y − 59.5, X = candidate X.
  - `TestMugPickupTargetFields`: label, home pose, shield, prompts equal the strings above.
- [ ] **Step 2: Run** `go test ./coffee -run 'TestHeldGeometries|TestMugPickupTarget' -v` — FAIL.
- [ ] **Step 3: Implement**.
- [ ] **Step 4: Run** the tests, then `make test` — PASS.
- [ ] **Step 5: Commit** `feat(coffee): vision pickup for mugs with a modeled handle`

### Task 5: The `vessel` profile for placing and retrieving under the spout

**Files:**
- Modify: `coffee/serving.go` (`setCupForCoffee`), `coffee/iced.go` (`approachBrewedCup`, `graspBrewedCup`, `grabBrewedCupFromMachine`), `coffee/actions.go`, `coffee/held_geometry.go` (mug configured-geometry fallback)
- Create: `coffee/vessel.go`
- Test: `coffee/vessel_test.go`

**Interfaces:**
- Consumes: `pickMugDynamic` (Task 4), mug pose constants (Task 3).
- Produces (`coffee/vessel.go`):
  ```go
  type vessel struct {
      label          string                                        // pickupLabelCup / pickupLabelMug
      pick           func(ctx, cancelCtx context.Context) error   // pickCupDynamic / pickMugDynamic
      underApproach  string                                        // claws pose in front of the group head
      readyForCoffee string                                        // claws pose under the group head
      attachConfigured func(ctx context.Context) error             // fallback when nothing cached
  }
  func (s *beanjaminCoffee) cupVessel() vessel
  func (s *beanjaminCoffee) mugVessel() vessel
  ```
  - `attachConfiguredMugGeometry(ctx)` in held_geometry.go: like `attachConfiguredGeometry` but attaches `geom.MugGeometries` around the recovered centroid.
  - `setVesselForCoffee(ctx, cancelCtx, v vessel) error`; `setCupForCoffee` = `setVesselForCoffee(…, s.cupVessel())`.
  - `approachBrewedVessel(ctx, cancelCtx, v vessel) error`; `approachBrewedCup` wraps it with the cup.
  - `graspBrewedInPlace(ctx, cancelCtx, v vessel) error` — the linear slow move to `v.readyForCoffee`, `grabAndVerifyHolding(ctx, "brewed "+v.label)`, reattach by `v.label`, fallback `v.attachConfigured`; **no retreat**.
  - `graspBrewedCup` = `graspBrewedInPlace(cup)` + today's retreat step (unchanged behaviour).
  - Error prefixes stay as today for the cup (`set_cup_for_coffee:`, `approach_brewed_cup:`, `grasp_brewed_cup:`); mug uses `set_mug_for_coffee:` etc.
  - `execute_action`: `set_mug_for_coffee`, `grab_brewed_mug` (grasp in place + retreat to `mug_under_machine_approach`).

- [ ] **Step 1: Write failing tests**
  - `TestCupVesselMatchesToday`: `cupVessel()` has label "cup", poses `cup_under_machine_approach` / `cup_ready_for_coffee`.
  - `TestMugVesselPoses`: label "mug", poses `mug_under_machine_approach` / `mug_ready_for_coffee`.
  - `TestConfiguredMugGeometryHasHandle`: with a static frame system (copy the setup from `TestConfiguredContainerBox`), the mug fallback attaches 2 geometries.
- [ ] **Step 2: Run** `go test ./coffee -run 'TestCupVessel|TestMugVessel|TestConfiguredMug' -v` — FAIL.
- [ ] **Step 3: Implement**.
- [ ] **Step 4: Run** the tests, then `make test` — PASS (all existing cup tests untouched).
- [ ] **Step 5: Commit** `refactor(coffee): place and retrieve under the spout by vessel`

### Task 6: The hot water station

**Files:**
- Create: `coffee/mug.go`
- Modify: `coffee/module.go` (`stagedMugPlaced bool`), `coffee/prepare.go` (`strandedState`), `coffee/motion.go` (reset clears the flag, and the clean-world check includes it), `coffee/actions.go`, `README.md` (actions list)
- Test: `coffee/mug_test.go`, `coffee/prepare_test.go`

**Interfaces:**
- Consumes: `stageHeldItem` (Task 2), `mugVessel`, `graspBrewedInPlace` (Task 5), `pickMugDynamic` (Task 4).
- Produces (`coffee/mug.go`):
  - `const stagedMugFrameName = "staged-mug"`
  - `slideMugToWater(ctx, cancelCtx) error` — one Step to `mug_ready_for_water`, `LinearConstraint: defaultApproachConstraint`, `MoveOptions: s.slowMoveOptions()`, `AllowedCollisions: s.heldItemSurfaceCollisions(heldItemMachineCollisions)`.
  - `setMugAtWater(ctx, cancelCtx) error` — `openGripper`, `settle`, `stageHeldItem(…, stagedMugFrameName)` and set `stagedMugPlaced`, linear retreat to `mug_water_approach`, `closeGripper`.
  - `dispenseHotWater(ctx, cancelCtx context.Context, drink string) error` — stub: `Warnf("dispense_hot_water: no hot water routine wired yet (drink %s); skipping", drink)`, return nil. Doc comment states the contract from Global Constraints.
  - `regrabMugAtWater(ctx, cancelCtx) error` — mirrors `grabStagedGlass`: approach, open, linear slow descent allowing the jaws to touch `staged-mug` (`stagedMugGrabCollisions()`, like `stagedGlassGrabCollisions`), `grabAndVerifyHolding(ctx, "mug at water")`, remove the obstacle and reattach (`regrabStagedMug`, like `regrabStagedGlass`), slow linear retreat with the held mug.
  - `placeMugAtWater(ctx, cancelCtx) error` — `pickMugDynamic`, step to `mug_water_approach`, linear to `mug_ready_for_water` (default speed, the mug is empty), `setMugAtWater`.
  - `strandedState` adds `"mug at the hot water dispenser"` when `stagedMugPlaced`; reset clears it with the glass.
  - `execute_action`: `slide_mug_to_water`, `set_mug_at_water`, `dispense_hot_water` (drink "americano"), `regrab_mug_at_water`, `place_mug_at_water`.

- [ ] **Step 1: Write failing tests**
  - In `TestStrandedStateNamesEachFlag`: `stagedMugPlaced` → names "mug at the hot water dispenser" (Review Focus 2).
  - `TestResetClearsStagedMug`: after staging a mug obstacle in a static frame system and calling the reset path, `cachedFS.Frame(stagedMugFrameName)` is nil and the flag is false.
  - `TestStagedMugGrabCollisions`: none when not placed; jaws vs `staged-mug` when placed.
  - `TestDispenseHotWaterStubIsNoop`: returns nil.
- [ ] **Step 2: Run** `go test ./coffee -run 'TestStrandedState|TestResetClearsStagedMug|TestStagedMug|TestDispenseHotWater' -v` — FAIL.
- [ ] **Step 3: Implement**.
- [ ] **Step 4: Run** the tests, then `make test` — PASS.
- [ ] **Step 5: Commit** `feat(coffee): set the mug down at the hot water dispenser and re-grab it`

### Task 7: Serving a mug

**Files:**
- Modify: `coffee/serving.go` (`tryDropCupInSlot`), `coffee/serving_slots.go` (`servingAreaDropZOffset`)
- Test: `coffee/serving_slots_test.go` (create if absent; else the existing serving test file)

**Interfaces:**
- Consumes: `heldItemLabel()` (Task 2), mug config (Task 3).
- Produces:
  - `func (s *beanjaminCoffee) servingOffsets() (grab, approach *RelativePose)` — when `heldItemLabel() == pickupLabelMug`, the mug overrides each where set, else the shared `ServingGrabRelativePose` / `ServingApproachRelativePose`. `tryDropCupInSlot` uses it.
  - `func mugGripAboveBaseMm(dims *ContainerDimensions, grab *RelativePose) float64` = `dims.HeightMm/2 + grab.Z`.
  - `servingAreaDropZOffset` returns `mugGripAboveBaseMm(...)` when the held item is a mug; otherwise unchanged.

- [ ] **Step 1: Write failing tests**
  - `TestServingOffsetsFallBackForMug` (Review Focus 4): mug held, no overrides → shared offsets; one override set → that one replaced, the other shared.
  - `TestServingOffsetsCupIgnoresMugOverrides`: cup held with mug overrides set → shared offsets.
  - `TestMugGripAboveBase`: dims height 97, grab z 0 → 48.5; grab z +5 → 53.5.
  - `TestDropZOffsetForMug`: mug held with grab z +5 → 53.5, not the half-height 48.5.
- [ ] **Step 2: Run** `go test ./coffee -run 'TestServingOffsets|TestMugGripAboveBase|TestDropZOffset' -v` — FAIL.
- [ ] **Step 3: Implement**.
- [ ] **Step 4: Run** the tests, then `make test` — PASS.
- [ ] **Step 5: Commit** `feat(coffee): serve mugs with their own offsets and grip height`

### Task 8: Drink ids and menu

**Files:**
- Modify: `coffee/order/drinks.go`, `coffee/order_intake.go` (`menu()`), `coffee/brew_steps.go` only if `drinkBrewTime` needs a case (it shouldn't: `IsLungo` covers americano)
- Test: `coffee/order/drinks_test.go`, `coffee/order_intake_test.go`

**Interfaces:**
- Produces (`package order`):
  - `func IsMug(drink string) bool` — true for `americano`, `hot_water`.
  - `func BrewsCoffee(drink string) bool` — false only for `hot_water`.
  - `IsLungo` returns true for `americano` too.
  - `Menu.MugDrinks bool`; `Supports("americano"|"hot_water")` → `(m.MugDrinks, "can_serve_mug_drinks=false")`.
  - `menu()` sets `MugDrinks: s.cfg.CanServeMugDrinks`.
  - `DrinkLabel("hot_water", "")` == "hot water" (already true; pin it).
  - No web-app change: `drinkLabel` in `web-app/app/order/drinks.ts` title-cases unknown ids.

- [ ] **Step 1: Write failing tests**
  - `TestMugDrinkPredicates`: `IsMug`, `BrewsCoffee`, `IsLungo` for americano, hot_water, lungo, espresso, iced_coffee.
  - `TestSupportsMugDrinks`: off → false with the reason; on → true.
  - `TestEnqueueRejectsMugDrinkWhenOff` (Review Focus 3): `enqueueOrder` with `{"drink":"americano"}` and the flag off returns an error containing `can_serve_mug_drinks=false`; with it on, accepted.
- [ ] **Step 2: Run** `go test ./coffee/order -run 'TestMugDrink|TestSupportsMug' -v` and `go test ./coffee -run TestEnqueueRejectsMugDrink -v` — FAIL.
- [ ] **Step 3: Implement**.
- [ ] **Step 4: Run** the tests, then `make test` — PASS.
- [ ] **Step 5: Commit** `feat(order): americano and hot water drinks behind can_serve_mug_drinks`

### Task 9: Brewing the mug drinks

**Files:**
- Modify: `coffee/prepare.go`, `coffee/steps.go` (labels), `coffee/sensor_usage.go`, `coffee/actions.go`, `README.md` (usage sensor list, actions, mug drinks section)
- Create: `coffee/prepare_mug.go` (the hot water flow and the Americano serve, keeping `prepareDrink` readable)
- Test: `coffee/sensor_usage_test.go`, `coffee/prepare_mug_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces:
  - `stepAddingHotWater = "Adding hot water"`, `stepPlacingMug = "Placing mug"` in steps.go.
  - `func cupCounter(drink string) (label, key string)` in sensor_usage.go → `("mugs", "mugs_used")` for `order.IsMug`, else `("espresso cups", "espresso_cups_used")`. `prepareDrink` uses it at the existing increment.
  - `waterDelta("hot_water")` returns `hotWaterWaterDelta = 2.0` (a full mug; the counter is relative), `waterDelta("americano")` 1.5 (lungo).
  - `prepareDrink` branches at the top: `if !order.BrewsCoffee(drink) { return s.prepareHotWater(ctx, cancelCtx, o, runPhase) }` — `runPhase` and the defers stay shared (pass the closure, or lift it to a method; builder's call).
  - Americano inside `prepareDrink`: step 5 uses `setVesselForCoffee(mugVessel)` with label `stepPlacingMug`; the brew prep (`hotPrep`) is `approachBrewedVessel(mugVessel)`; serve = `serveAmericano(ctx, cancelCtx, placeDrink)`:
    (`approachBrewedVessel(mug)` first when the arm did not line up during the pour, i.e. `cupApproached` is false) → `graspBrewedInPlace(mug)` → `slideMugToWater` → `setMugAtWater` → `setStep(stepAddingHotWater)` + `logger.Info("step 6c/9: adding hot water")` + `dispenseHotWater(…, "americano")` → `setStep(stepServing)` + `regrabMugAtWater` → `placeHeldInServingArea(…, heldFilled)`. Delivery: a mug drink never goes to the cart (`toCart` is false when `order.IsMug(drink)`).
  - `prepareHotWater` phases and progress lines, exactly:
    1. `stepLockingPortafilter` / `step 1/7: locking empty portafilter` → `lockPortaFilter`
    2. `stepReleasingFilter` / `step 2/7: releasing filter` → `releaseFilter`
    3. `stepPlacingMug` / `step 3/7: placing mug at the hot water` → `placeMugAtWater`, then increment `mugs_used`
    4. `stepAddingHotWater` / `step 4/7: adding hot water` → `dispenseHotWater(…, "hot_water")`, then water usage
    5. `stepServing` / `step 5/7: serving mug` → `regrabMugAtWater` + `placeHeldInServingArea(heldFilled)`; then the drink-ready speech as today
    6. `stepGrabbingFilter` / `step 6/7: grabbing filter` → `grabFilter`
    7. `stepUnlockingPortafilter` / `step 7/7: unlocking portafilter` → `unlockPortaFilter`, then `stepFinishingUp` and the home move as today.
    No grinder, drip-tray or cleaner counters.
  - Express the phase list as data so it is testable: `func hotWaterPhases() []phaseSpec` with `type phaseSpec struct{ step, span, progress string }`; `prepareHotWater` iterates it, binding each to its function by `span`.
  - `execute_action`: `serve_americano` (from a lungo already brewed into a mug under the spout: grasp → … → serve) and `serve_hot_water` (`placeMugAtWater` → hook → regrab → serve; assumes the portafilter is already out of the gripper, like `serve_iced_coffee`).

- [ ] **Step 1: Write failing tests**
  - `TestCupCounter`: americano and hot_water → mugs_used; lungo, iced_coffee → espresso_cups_used.
  - Extend `TestWaterDelta`: hot_water 2.0, americano 1.5.
  - `TestHotWaterPhases`: 7 phases; steps in order Locking portafilter, Releasing filter, Placing mug, Adding hot water, Serving, Grabbing filter, Unlocking portafilter; progress lines numbered `1/7`…`7/7`; none is Grinding, Tamping, Brewing or Cleaning.
- [ ] **Step 2: Run** `go test ./coffee -run 'TestCupCounter|TestWaterDelta|TestHotWaterPhases' -v` — FAIL.
- [ ] **Step 3: Implement**. README: add `mugs_used` to the usage-sensor list, the new actions, and a "Mug drinks" section (flag, flows, `dispenseHotWater` contract, the Cappuccina calibration checklist and the refill-display/monitor entries from the spec).
- [ ] **Step 4: Run** the tests, then `make lint && make test` — PASS, lint clean.
- [ ] **Step 5: Commit** `feat(coffee): brew americano and serve hot water in mugs`

### Task 10: On-machine check (manual, with the user)

Not code. Start `mug_ready_for_water` as `mug_ready_for_coffee` with `"translation": {"y": -32}` on the switch (the spout is 32 mm right of the portafilter), and `mug_water_approach` as `cup_under_machine_approach` is built. After the calibration checklist in the spec is done on Cappuccina (crop camera, segmenter, observe switch, shield, offsets, handle dims, the four claws poses, `can_serve_mug_drinks: true`, refill display "Mugs" key and `mugs_level_monitor` at threshold 4):

- [ ] `execute_action pick_mug` → log line `grabbed mug: gripper pos=…`; widen the holding band only if outside 380–720.
- [ ] `set_mug_for_coffee`, then by hand confirm spout clearance.
- [ ] `grab_brewed_mug`, `slide_mug_to_water` (empty mug), `set_mug_at_water`, `regrab_mug_at_water`.
- [ ] `serve_hot_water` end to end (stub hook), then one `prepare_order` of `americano`.
- [ ] Confirm `mugs_used` incremented once per drink and the refill display shows Mugs.
