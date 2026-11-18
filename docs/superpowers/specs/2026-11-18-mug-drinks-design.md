# Mug drinks: Americano and hot water

## Intent

The machine learns to serve in ceramic mugs, picked up off their own shelf.
Two drinks use them for now:

- **Americano**: a lungo brewed into the mug under the group head, then the
  mug moved a few centimetres to the hot water dispenser and topped off.
- **Hot water**: the mug filled at the dispenser only, for a customer's own tea
  or hot chocolate mix.

Turning the hot water on is a colleague's separate routine, built in parallel.
This work covers everything around it: getting the mug, placing it, moving it,
and serving it, with one obvious hook for that routine to plug into.

## Goals

- Pick a mug off the mug shelf by vision, gripping the body.
- Place it under the group head for a lungo, using poses of its own.
- Move a filled mug from the group head to the hot water spot, set it down,
  let the arm leave and come back, and pick it up again.
- Serve the finished mug to the serving shelf like any other filled drink.
- Order both drinks through `prepare_order` (`americano`, `hot_water`), gated by
  a config flag so machines without mugs or a dispenser never offer them.
- Each new arm step is runnable alone through `execute_action`, so placement can
  be tuned on Cappuccina before the hot water routine exists.

## Non-goals

- Activating the hot water (the colleague's routine).
- Kiosk buttons, images and labels for the new drinks (follow-up; see
  "Left to the builder").
- Delivery-cart placement of mugs.
- Detecting the handle or its direction (the shelf fixes it; it is modeled,
  not seen).
- Telling mugs from cups in one detection: they live on separate shelves.

## Anti-goals

- Any change in how cup or iced drinks behave. The cup path is
  generalized, not rewritten, and its behaviour must stay identical.
- Moving a full mug fast or tilted. Every move with liquid in it is slow and
  level.

## Constraints and facts

- **Grip**: the mug is gripped by the body; the user has already tested the
  grip. The gripper holds the 78 mm glass today. The first real grab logs the
  mug's jaw position (`grabAndVerifyHolding`); `gripper_hold_min_pos` /
  `gripper_hold_max_pos` (380 / 720 on Cappuccina) are widened in config only if
  it falls outside.
- **Handle** (revised 2026-11-18): the mug shelf is arranged so every mug's
  handle sticks out to the arm's left (revised again 2026-11-18 after seeing
  the mugs on the table). The jaws close left and right of the body, so the
  left jaw passes **through the handle's loop** and presses on the
  body inside it. The grip never changes between pickup and serving, so the
  handle stays at a fixed place relative to the gripper: out to the left of
  the jaws, 90° counter-clockwise (seen from above) from the direction the gripper
  approaches. It is modeled for obstacle avoidance (see "The handle" below).
- **Measured mug** (2026-11-18), for `mug_dimensions` and
  `mug_handle_dimensions`:

  | Measurement | Value | Config |
  | --- | --- | --- |
  | Height | 97 mm | `mug_dimensions.height_mm` |
  | Diameter | 80 mm | `mug_dimensions.diameter_mm` |
  | Handle height | 76 mm | not used: the handle box takes the mug's full height |
  | Handle, out from the mug | 39 mm | `mug_handle_dimensions.depth_mm` |
  | Handle, across its surface | 15 mm | `mug_handle_dimensions.width_mm` |

  The cup is 70 × 60 mm and the glass 78 mm across, which the gripper
  already holds, so 80 mm is 2 mm wider than anything gripped today.
- **Grip height** (revised): the mug is gripped in the middle of its height,
  where the left jaw fits through the handle's loop. This is config, not code:
  grab offsets add in world axes onto the seated centroid
  (`geom.PoseRelativeTo`), which sits at 1 mm + half the 97 mm height = 49.5 mm
  above the shelf, so `mug_grab_relative_pose` z ≈ 0 (the cup's is +6, which
  grips a cup 37 mm off the shelf). `mug_approach_relative_pose` keeps the
  same z, so the jaw enters the loop level. The held-item box stays centred on
  the mug's true centre, so collision checks stay correct. On the serving
  shelf, `mug_serving_grab_relative_pose` z should match the grab z, since
  serving offsets are measured from the mug's centre too (see serving below).
- **Clearance**: the mug rests on the drip tray whatever its grip height, so it
  spans about z 72–169 under the spout against the cup's 72–132. Whether 169
  clears the spouts is a physical check. The mug is gripped 12.5 mm higher on
  itself than a cup is (48.5 vs 36 mm above its base), so `mug_ready_for_coffee`
  starts as the cup pose raised 12.5 mm, at the same angle (see "Placement
  angle").
- **Dispenser**: the hot water spout is about 79.5 mm to the right of the
  portafilter (measured with a mug in hand; 32 mm was to the mount's edge):
  subtract 79.5 mm from world Y. Activating it needs the arm, so the
  mug is set down there, released, and re-grabbed after.
- **Detection**: the stock YOLO `cup` class covers mugs. The mug gets its own
  cropped camera (cropped to the mug shelf) and segmenter in config, reusing
  `cup-yolo-detector`, exactly as cups and glasses each have their own.
- **Serving order** today: the drink is served (step 6b) before the portafilter
  is unlocked and cleaned (steps 7–9), and the arm is free during the pour on a
  separate-buttons machine (`brewAndPrep`). The Americano follows the same order.

## Approaches considered

- **A. Mug as a second container on the existing cup code (picked).** A
  `mugPickupTarget()` beside the cup and glass ones; the cup's place/retrieve
  path takes a container profile so cup and mug share it; new mug-only steps
  for the move to the dispenser and the re-grab; the drinks behind a flag; an
  empty `dispenseHotWater` hook. Touches the cup serving path, so it needs a
  test that the cup behaves the same.
- **B. Separate mug functions.** Copy the cup placement into `mug.go` and leave
  cups alone. No risk to cups now, but two near-identical paths to keep in sync.
- **C. Placement actions only.** A's pieces as `execute_action` steps, with the
  drinks added once the hot water routine exists. Smallest, but nobody can order
  an Americano yet.

The user picked **A**, matching how the glass already shares the cup's pickup.

## Design

### Container profile (`coffee/serving.go`, `coffee/iced.go`)

Today `setCupForCoffee`, `approachBrewedCup` and `graspBrewedCup` name the cup's
poses (`cup_under_machine_approach`, `cup_ready_for_coffee`) and pick it with
`pickCupDynamic`. They take a small profile instead:

```go
type vessel struct {
    label          string            // "cup" / "mug": logs, held-item key, step text
    pick           func(ctx, cancelCtx context.Context) error // pickCupDynamic / pickMugDynamic
    underApproach  string            // claws pose in front of the group head
    readyForCoffee string            // claws pose under the group head
}
```

`s.cupVessel()` returns today's values, and every existing caller passes it, so
the cup path is unchanged. `s.mugVessel()` returns the mug's. The
`set_cup_for_coffee` and `grab_brewed_cup` actions keep their names and use the
cup; new `set_mug_for_coffee` and `grab_brewed_mug` actions use the mug.

### Mug pickup (`coffee/cup_pickup.go`)

`mugPickupTarget()` and `pickMugDynamic`, alongside the glass:

- vision: `mug_vision_service_name`
- observe switch: `mug_observe_pose_switcher_name`, home pose `mug_observe`
- offsets: `mug_approach_relative_pose`, `mug_grab_relative_pose`
- dimensions: `mug_dimensions`
- shield: `clean-mug-area-shield` (obstacle in config, like the cup and glass ones)
- retreat speed: the arm default (the mug is empty)
- max attempts: shared `cup_pickup_max_attempts`
- prompts: "I don't see a mug yet — please place one on the mug shelf…" and the
  "nudge it" line, worded like the cup's

### The handle (`coffee/geom/pickup.go`, `coffee/held_geometry.go`)

The mug's collision model is two boxes: the body (today's `ContainerBox`, from
`mug_dimensions`) and a handle box from `mug_handle_dimensions`:

- `depth_mm`: how far the handle sticks out from the body (39 mm measured)
- `width_mm`: its thickness side to side (15 mm measured)

Decided: the handle box is the mug's full height (`mug_dimensions.height_mm`),
not the handle's own 76 mm. It is simpler, needs no height-on-the-mug
setting, and is cautious: it can only make the model bigger than the real mug.

The handle box sits flush against the body on the arm's left: 90° counter-clockwise
from the grab heading (`containerYaw`), so it turns with the grab. Its 39 mm
`depth_mm` runs outward along that right-hand direction and its 15 mm
`width_mm` along the grab heading. A new pure function in
`geom` (e.g. `MugGeometries(centroid, dims, handle, yaw, label)
[]spatialmath.Geometry`) builds both, tested like `ContainerBox`.

Today the held-item frame, its per-label cache, the staged-obstacle frame, and
the "configured geometry" fallback (`attachConfiguredGeometry`, used when nothing
was cached) each carry one geometry. They change to carry a list; cups and
glasses pass a one-item list and behave exactly as today. Everything that
models the mug uses both boxes: the held item after pickup, the re-grab from
under the spout, the obstacle while the mug stands at the dispenser, and the
re-grab there. So every move plans around the handle, and a pose that would
put the handle into the machine fails to plan instead of hitting it.

The left jaw sits inside the modeled handle box once the mug is held. That is
already allowed: the held item may touch the claws and gripper
(`heldItemSelfCollisions`), and RDK applies an allowance named for a frame to
every geometry in it, so the handle inherits it. On the re-grab at the
dispenser, the staged mug's allowance against the jaws covers the handle the
same way.

The detection's point cloud now includes the handle, which faces sideways and
is visible to the camera. That can pull the detected centre several mm toward
the handle, and the grab is aimed at that centre. The first calibration grabs
measure the shift; if it is consistent, `mug_grab_relative_pose` x/y absorbs
it.

### Placement angle (config, not code)

The handle sticks out to the arm's left. Coming straight in under the spout,
as for the cup, it points away from the hot water spout (world +Y), not into the
back of the machine. So `mug_ready_for_coffee` starts at the cup pose's angle
and is turned only if the handle hits the machine's left side. The handle
trails the 79.5 mm slide to the dispenser; its clearance from the machine's left side is a check at
the machine (the collision model reports a clash as a failed plan). On the
serving shelf a mug reaches 79 mm to the left of its centre (40 mm radius +
39 mm handle), which the slot spacing must leave room for;
`mug_serving_*_relative_pose` can turn it if not.

No code is needed for the angle itself. The poses on the claws switch carry
their orientation, and each approach pose is defined relative to its ready pose
(as `cup_under_machine_approach` is: 200 mm back along the gripper's heading),
so the arm comes in at the same angle automatically. Building
`mug_ready_for_water` on `mug_ready_for_coffee` as its baseline (79.5 mm in −Y)
gives both the same angle by construction. `slideMugToWater` expects
both ready poses at the same yaw, so the slide is a pure translation; startup
pose validation warns if their yaws differ by more than a few degrees.

### The hot water station (`coffee/mug.go`, new)

New claws poses: `mug_water_approach` (in front of the dispenser) and
`mug_ready_for_water` (under it).

- **`slideMugToWater`**: holding the filled mug at `mug_ready_for_coffee`, one
  linear, slow (`slowMoveOptions`) move straight to `mug_ready_for_water`. Level
  is kept because both poses share the mug's upright orientation and the move is
  linear. Allowed collisions are the held item against the machine, as the
  cup's placement uses today (`heldItemMachineCollisions`).
- **`setMugAtWater`**: release, settle, then turn the mug into a world obstacle
  (the way `stageGlass` stages the glass) so the arm's trip to the hot water
  switch plans around it; linear retreat to `mug_water_approach`; close the
  gripper. A mug left there is added to `strandedState`, so a fault names it.
- **`dispenseHotWater`** (the hook): `func (s *beanjaminCoffee)
  dispenseHotWater(ctx, cancelCtx context.Context, drink string) error`. It
  takes the drink id because a top-off and a full mug are different amounts;
  the routine decides what each means. In this work it logs
  that no routine is wired and returns nil. The colleague replaces its body.
  The routine keeps the arm for the whole pour: it turns the water on, waits
  a set time, and turns it off, so nothing else uses the arm until it returns.
  It starts and must end with the arm clear of the mug.
- **`regrabMugAtWater`**: `mug_water_approach` → open → linear descent to
  `mug_ready_for_water` (jaws allowed to touch the staged mug) → grab and verify
  → obstacle back to a held item → slow linear retreat to `mug_water_approach`.
  The reverse of `setMugAtWater`, mirroring `grabStagedGlass`.
- **`placeMugAtWater`** (hot water drink only): pick the empty mug, then
  `mug_water_approach` → linear to `mug_ready_for_water` → `setMugAtWater`.

Each is an `execute_action` step: `slide_mug_to_water`, `set_mug_at_water`,
`dispense_hot_water`, `regrab_mug_at_water`, `place_mug_at_water`, plus
`serve_americano` and `serve_hot_water` for the full sequences.

### Drinks (`coffee/order/drinks.go`, `coffee/prepare.go`)

- New ids `americano` and `hot_water`; `Menu.MugDrinks` from config;
  `Supports` returns `can_serve_mug_drinks=false` when off.
- `IsLungo("americano")` is true (it brews a lungo). `IsMug(drink)` covers both;
  `BrewsCoffee(drink)` is false for `hot_water`.
- **Americano** runs today's lungo phases with the mug: placing (step 5) uses
  `mugVessel`; while the lungo pours, the arm waits just in front of the mug,
  so it can grab it as soon as the pour ends (on machines like Cappuccina,
  where the arm isn't needed during the pour; `approachBrewedCup` with the
  mug's poses); serving becomes grasp mug →
  `slideMugToWater` → `setMugAtWater` → `dispenseHotWater` → `regrabMugAtWater`
  → `placeHeldInServingArea(heldFilled)`. Steps 7–9 (filter and cleaning) are
  unchanged.
- **Hot water**: the gripper holds the portafilter between orders, so it must
  let go before it can pick up a mug. Decided: lock the **empty** portafilter
  into the group head and release it, exactly as a brew does (`lockPortaFilter`,
  `releaseFilter`), then grab and unlock it at the end (`grabFilter`,
  `unlockPortaFilter`) with no cleaning. No grinding, tamping or brewing. Its
  phases: locking portafilter → releasing filter → placing mug at the water →
  adding hot water → serving → grabbing filter → unlocking portafilter →
  home. Its own progress lines (`step 1/8: locking portafilter`, … through `step 8/8`, the home move, planned with the portafilter unlock) and step
  labels, kept in sync per the CLAUDE.md rule; the existing labels are reused
  where the phase is the same.
- New step label for the Americano's extra phase: "adding hot water", shown
  between brewing and serving.
- Sensor counts: water usage for both; grinder, drip-tray and cleaner counts
  only when coffee is brewed. A new `mugs_used` counter on the usage sensor
  goes up by 1 as soon as a mug has been picked up and placed (Americano: under
  the spout; hot water: at the dispenser), the same point at which
  `espresso_cups_used` counts a cup, so a later fault still counts the mug.
  Cup drinks keep counting `espresso_cups_used`; mug drinks do not.
- Mug drinks are pickup-only for now: `prepare_order` refuses `fulfillment:
  "delivery"` for `americano` and `hot_water` ("delivery isn't available for …
  yet"). (Decided 2026-11-18; the cart needs its own work first. The code
  still keeps mugs out of the cart and reports `cup_type` "mug" defensively.)
- Flat moves around a mug (decided 2026-11-18): whenever the open jaws come
  into or leave a standing mug, the move is straight with no Z change, so a
  jaw can't catch the handle. The mug approach poses and offsets keep the same
  Z as their ready/grab counterparts (checked at startup), and after every
  shelf release, for every drink, the gripper withdraws straight back at the
  drop height. The delivery cart's vertical lift-out is unchanged.
- The Americano's top-up (slide, set down, hot water, re-grab) is its own
  planned stretch before serving, so a serving slot that fails to plan never
  re-records it.
- Serving placement (`tryDropCupInSlot`) uses the shared
  `serving_grab_relative_pose` / `serving_approach_relative_pose` and the held
  item's half-height. The mug is taller and may be gripped below mid-height, so
  optional `mug_serving_grab_relative_pose` / `mug_serving_approach_relative_pose`
  override the shared ones for mugs, falling back to them when unset. The drop
  anchor is the mug's centre (its half-height), as for every container; the
  override's z should equal `mug_grab_relative_pose`'s z (revised in the final
  review, 2026-11-18).
- The new ids show raw (via `DrinkLabel`: "americano", "hot water") in
  `get_queue`, the order sensor, the dashboard and Slack until the kiosk
  follow-up; nothing there may error on an unknown id (checked in this work).
- Cancel and `reset_world` clear a staged mug obstacle the way they clear the
  staged glass.

### Config (`coffee/config.go`, README)

New, all `omitempty`:

| Field | Type | Notes |
| --- | --- | --- |
| `can_serve_mug_drinks` | bool | Turns on `americano` and `hot_water`. |
| `mug_vision_service_name` | string | Segmenter returning mug detections. |
| `mug_observe_pose_switcher_name` | string | Must include `mug_observe`; moves `cam`. |
| `mug_approach_relative_pose` | RelativePose | Like `cup_approach_relative_pose`. |
| `mug_grab_relative_pose` | RelativePose | Like `cup_grab_relative_pose`. |
| `mug_dimensions` | ContainerDimensions | Measured: 80 mm diameter, 97 mm height. |
| `mug_handle_dimensions` | object | `depth_mm`, `width_mm`; the box takes the mug's full height. |
| `mug_serving_grab_relative_pose` | RelativePose | Optional; falls back to `serving_grab_relative_pose`. |
| `mug_serving_approach_relative_pose` | RelativePose | Optional; falls back to `serving_approach_relative_pose`. |

All but the flag and the two serving overrides are required when the flag is set (`Validate`, as for the
glass fields). Startup pose validation (`poses.go`) requires, when the flag is
set: `mug_observe` on the mug observe switch, and `mug_under_machine_approach`,
`mug_ready_for_coffee`, `mug_water_approach`, `mug_ready_for_water` on the claws
switch. README gets a mug section.

### Tests

- Cup path unchanged: the cup vessel resolves to today's pose names and
  `pickCupDynamic`; existing tests pass untouched.
- `MugGeometries`: handle box on the arm's left of the grab yaw, flush with
  the body, spanning the body's full height, for several yaws.
- Held-item attach, cache, re-attach and staged obstacle carry both mug boxes,
  and still exactly one box for cups and glasses.
- `Supports`, `IsLungo`, `IsMug`, `BrewsCoffee` for the new ids, and the flag.
- `Validate` rejects the flag without the mug fields.
- Required-pose list includes the mug poses only with the flag.
- `mugs_used` goes up once per mug drink and `espresso_cups_used` does not.
- Hot water phase list locks and unlocks the empty portafilter and skips
  grinding, tamping, brewing and cleaning.

## Calibration on Cappuccina (config, not code)

Once the mug shelf is in place, using `viam robot part motion get-pose`:

1. Mug shelf crop box → `mug-cropped-camera` (pc-crop-camera on `cam`).
2. `mug-detection-segmenter` (detections-to-segments over `cup-yolo-detector`
   and `mug-cropped-camera`).
3. `mug-observe-switch` with `mug_observe` and any extra vantages.
   Recorded on Cappuccina 2026-11-18 (`cam` frame, world): `mug_observe` =
   `{"x": 468.72, "y": 280.88, "z": 280.89, "o_x": 0.88, "o_y": 0.01, "o_z": -0.47, "theta": -90.70}`.
   From there all three mugs on the table detect as "cup" at 0.94–0.97.
4. `clean-mug-area-shield` obstacle around the mug shelf.
5. `mug_approach_relative_pose` / `mug_grab_relative_pose`: start from the
   cup's x/y with z = 0, so the grip is at the mug's middle and the left jaw
   enters the handle's loop. Check the open left jaw clears the loop on the
   way in.
   Measured on Cappuccina 2026-11-18 by jogging the open jaws around the
   middle mug (left jaw through the loop): grip point (910.75, 167.17, 54.09)
   against the mug's detected centre (890, 181) seated at z 41.9, so
   `mug_grab_relative_pose` = `{"x": 21, "y": -14, "z": 12, "o_x": 1, "o_y": 0, "o_z": 0, "theta": 0}`
   and `mug_approach_relative_pose` = `{"x": -145, "y": -14, "z": 12, "o_x": 1, "o_y": 0, "o_z": 0, "theta": 0}`
   (145 mm back, as for the cup). The −14 mm in y likely absorbs the handle
   pulling the detected centre toward +Y. Closed on the mug's body (left jaw
   inside the loop) the gripper reads pos 680: inside Cappuccina's 380–720
   holding band, 40 counts from the "open" cutoff. `gripper_hold_max_pos` was
   left at 720 by choice; if mug grabs start reading as misses, raise it toward
   765 (midway between 680 and fully open, 850).
   Also added: `clean-mug-area-shield`, a world box centred (888.5, 203.5, 56.2)
   sized 143 × 443 × 128 mm (the three mugs plus 30 mm on every side).
6. `mug_handle_dimensions`: `depth_mm` 39, `width_mm` 15 (both measured).
7. `mug_under_machine_approach`, `mug_ready_for_coffee` (the cup pose raised
   12.5 mm at the same angle; check spout clearance and that the handle clears
   the machine's left side).
   `mug_ready_for_water` starts as `mug_ready_for_coffee` moved 79.5 mm in −Y,
   set on the switch as `"baseline": "mug_ready_for_coffee"`,
   `"translation": {"y": -79.5}` (the way `cup_under_machine_approach` is built
   from `cup_ready_for_coffee`), so it keeps the same angle. Fine-tune on the
   machine. `mug_water_approach` likewise from `mug_ready_for_water`.
   Set on Cappuccina 2026-11-18 (claws-position-switch), all built on the cup:
   `mug_ready_for_coffee` = baseline `cup_ready_for_coffee` + z 24.56 →
   (889, −250, 127) at the cup's angle (0.81, −0.59), checked with a mug in
   hand: handle clear of the recess, rim under the group head (empty — a
   locked portafilter's spouts not yet checked against the rim);
   `mug_under_machine_approach` = baseline `mug_ready_for_coffee`,
   `along_orientation` −200, z 0 (flat); `mug_ready_for_water` = baseline
   `mug_ready_for_coffee`, y −79.5 (jogged under the hot water outlet with a mug in hand:
   grip point (889, −329.5, 127); the 32 mm first measured was to the edge of the
   portafilter mount, not the outlet's centre); `mug_water_approach` = baseline
   `mug_ready_for_water`, `along_orientation` −200, z 0 (flat).
7b. Serving: set on Cappuccina 2026-11-18 and checked with a mug in hand on
   serving slot 2 (grip point (200, 786, 339.5), gripper pointing +Y): the mug
   sits flat on the shelf with the handle (pointing −X) clear of slot 1.
   `mug_serving_grab_relative_pose` = `{"x": 0, "y": 21, "z": 12, "o_x": 0, "o_y": 1, "o_z": 0, "theta": 0}`,
   `mug_serving_approach_relative_pose` = `{"x": 0, "y": -155, "z": 33, "o_x": 0, "o_y": 1, "o_z": 0, "theta": 0}`.
   z 12 matches the grab z (the shared offset's z 4 would push the mug 8 mm into
   the shelf). Slots are 120 mm apart, so a handle (79 mm out) leaves about 1 mm
   to a neighbouring mug's body.
8. First grab: read the logged jaw position, adjust the holding band if needed.

## Mug supply (config, not code)

Four mugs to start. On Cappuccina, alongside the existing cup and glass
entries:

- refill display: a "Mugs" key reading `mugs_used`, threshold 4
- `sensor-monitor`: a `mugs_level_monitor` on `mugs_used` like
  `espresso_cups_level_monitor`, threshold 4

The refill display already resets any counter it reads when its key is pressed,
so mugs need no new code there. README's usage-sensor list gains `mugs_used`.

## Later: a stepped mug shelf

Mugs start on the table. The cups today stand on a stepped shelf
(`step-shelf-*` in config: four steps about 90 mm apart in height and
60–80 mm apart front to back), and the pickup already handles it. Nothing in it assumes one
level:

- the observe sweep visits several camera poses (cups use three, at different
  heights);
- each detection is seated on the highest modeled surface beneath it
  (`geom.SurfaceTopZUnder`), whichever step that is;
- candidates are tried closest-first, skipping any that can't be planned.

So moving mugs to a stepped shelf needs config (the shelf's steps as obstacles,
a crop box, observe poses, the shield), not new code. The limits are physical,
and the handle changes them from the cup's:

- **Step depth.** With the handle to the side, a mug is 80 mm front to back.
  The cup shelf's steps are 60–80 mm apart front to back, so a mug shelf needs
  steps at least that deep plus a little clearance.
- **Side spacing.** Each mug reaches 79 mm to its left (radius + handle), and
  the open left jaw has to get into the loop, so mugs on a step need more
  room side to side than cups.
- **Vertical room.** Each level needs room for a 97 mm mug plus the lift on the
  retreat.

The design keeps that door open: the handle box is the only mug-specific
geometry, and its size comes from config.

## Left to the builder

- Exact names and placement of helpers within the files named above.
- Whether `vessel` also carries the held-item dimensions or reads them from the
  pickup target.
- The hot water drink's step-label strings, as long as they follow the
  existing label/progress-line pairing.

## Calls made without the user (check these)

- Kiosk buttons for the new drinks are a follow-up, not this work.
- A mug delivery order goes to the shelf, not the cart.
- The mug at the dispenser is modeled as a world obstacle while the arm is away.
- `dispenseHotWater` is a no-op that logs, so the full sequence runs end to end
  before the routine exists.
- Mug pickup shares `cup_pickup_max_attempts`.
- `dispenseHotWater` takes the drink id, so the routine knows top-off vs. full.
- Mugs get optional serving offsets that fall back to the shared ones.
- New drink ids show raw everywhere outside the order path until the kiosk
  follow-up.
- The handle is modeled as one box flush with the body, on the arm's left.
- The slide to the dispenser assumes both ready poses share a yaw (warned at
  startup if not).
