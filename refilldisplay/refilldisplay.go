// Package refilldisplay registers the viam:beanjamin:refill-display model, a
// generic service that keeps the stream deck's Refill page in step with the usage
// counters: each button shows its live count and is coloured by how close the
// consumable is to needing a refill.
//
// It only reads the sensor-monitor and tells the stream deck what to show, so
// neither of those modules needs coffee-specific logic. See README.
package refilldisplay

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	generic "go.viam.com/rdk/services/generic"
)

// Model is the full model triplet for this service.
var Model = resource.NewModel("viam", "beanjamin", "refill-display")

func init() {
	resource.RegisterService(generic.API, Model,
		resource.Registration[resource.Resource, *Config]{
			Constructor: newRefillDisplay,
		},
	)
}

const (
	defaultPollInterval = 2 * time.Second
	// repaintEvery re-sends every button even if nothing changed, so a stream
	// deck that reloaded its saved config (which drops live updates) recovers.
	repaintEvery = 30 * time.Second
	// callTimeout caps one read or one deck update so a stuck dependency can't
	// stall the loop.
	callTimeout = 5 * time.Second

	// defaultText is the label, then the count against its limit on a new line.
	// defaultTextNoLimit is used when the monitor reports no threshold.
	defaultText        = "{label} {value}/{threshold}"
	defaultTextNoLimit = "{label} {value}"
)

// Colors are the button backgrounds for each state. Empty fields use the
// defaults.
type Colors struct {
	// Fresh is used when the count is 0, which is right after a refill.
	Fresh string `json:"fresh,omitempty"`
	// OK is used when the count is above 0 and the monitor's rule is not firing.
	OK string `json:"ok,omitempty"`
	// Near is used when the count is within NearPercent of the threshold but the
	// rule is not firing yet. It has no effect unless near_percent is set.
	Near string `json:"near,omitempty"`
	// Due is used when the monitor's rule for the reading is firing.
	Due string `json:"due,omitempty"`
}

func (c Colors) withDefaults() Colors {
	if c.Fresh == "" {
		c.Fresh = "green"
	}
	if c.OK == "" {
		c.OK = "teal"
	}
	if c.Near == "" {
		c.Near = "salmon"
	}
	if c.Due == "" {
		c.Due = "red"
	}
	return c
}

// Button ties one stream deck key to one counter.
type Button struct {
	// Name is the stream deck key's name (the "name" field on the key).
	Name string `json:"name"`
	// Reading is the counter's key in the monitor's readings, for example
	// "espresso_cups_used".
	Reading string `json:"reading"`
	// Label is the short title on the button. Defaults to Name.
	Label string `json:"label,omitempty"`
	// Threshold is the counter's limit, for when the monitor doesn't report one
	// as "<key>_threshold". It repeats the monitor rule's threshold, so keep the
	// two in step. A threshold the monitor reports wins over this. Unset or 0
	// means none.
	Threshold float64 `json:"threshold,omitempty"`
	// Text is the button text, with {label}, {value} and {threshold} filled in.
	// Defaults to "{label} {value}/{threshold}" (for example "Water 12/25"), or
	// just "{label} {value}" if there is no threshold. The stream deck
	// wraps text on spaces and shrinks it to fit, so on a small key this reads as
	// the label over the count.
	Text string `json:"text,omitempty"`
}

// Config is the attribute set of the viam:beanjamin:refill-display service.
type Config struct {
	// MonitorSensorName is the viam:sensor-bundle:sensor-monitor sensor whose
	// readings and "<key>_triggered" flags drive the display.
	MonitorSensorName string `json:"monitor_sensor_name"`
	// UsageSensorName is the viam:sensor-bundle:stateful-sensor that holds the
	// counters. A button's "refill" command resets its counter there.
	UsageSensorName string `json:"usage_sensor_name"`
	// StreamDeckName is the stream deck service to update.
	StreamDeckName string `json:"stream_deck_name"`
	// Page is the stream deck page the buttons are on. Empty means whichever
	// page is showing.
	Page    string   `json:"page,omitempty"`
	Buttons []Button `json:"buttons"`
	Colors  Colors   `json:"colors,omitempty"`
	// NearPercent, if set, shows a counter in the "near" colour once it is within
	// this percent of its threshold (20 means from 80% of the way there), before
	// the monitor's rule fires. Unset or 0 turns the warning off. It assumes the
	// counter counts up toward its threshold, as the refill rules do.
	NearPercent float64 `json:"near_percent,omitempty"`
	// PollIntervalSeconds is how often the monitor is read. Defaults to 2.
	PollIntervalSeconds float64 `json:"poll_interval_seconds,omitempty"`
}

// Validate rejects configs the display cannot use and declares the two
// dependencies.
func (cfg *Config) Validate(path string) ([]string, []string, error) {
	if cfg.MonitorSensorName == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "monitor_sensor_name")
	}
	if cfg.UsageSensorName == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "usage_sensor_name")
	}
	if cfg.StreamDeckName == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "stream_deck_name")
	}
	if len(cfg.Buttons) == 0 {
		return nil, nil, fmt.Errorf("%s: buttons needs at least 1 button", path)
	}
	if cfg.NearPercent < 0 || cfg.NearPercent >= 100 {
		return nil, nil, fmt.Errorf("%s: near_percent must be at least 0 and less than 100", path)
	}
	if cfg.PollIntervalSeconds < 0 {
		return nil, nil, fmt.Errorf("%s: poll_interval_seconds must not be negative", path)
	}
	seen := map[string]bool{}
	for i, b := range cfg.Buttons {
		if b.Name == "" {
			return nil, nil, fmt.Errorf("%s: buttons[%d] needs a name", path, i)
		}
		// The stream deck treats a numeric key reference as an index, so a
		// numeric name could never be addressed.
		if _, err := strconv.Atoi(b.Name); err == nil {
			return nil, nil, fmt.Errorf("%s: button name %q must not be a number", path, b.Name)
		}
		if seen[b.Name] {
			return nil, nil, fmt.Errorf("%s: button %q listed twice", path, b.Name)
		}
		seen[b.Name] = true
		if b.Threshold < 0 {
			return nil, nil, fmt.Errorf("%s: button %q threshold must not be negative", path, b.Name)
		}
		if b.Reading == "" {
			return nil, nil, fmt.Errorf("%s: button %q needs a reading", path, b.Name)
		}
	}
	return []string{
		sensor.Named(cfg.MonitorSensorName).String(),
		sensor.Named(cfg.UsageSensorName).String(),
		generic.Named(cfg.StreamDeckName).String(),
	}, nil, nil
}

type refillDisplay struct {
	resource.AlwaysRebuild

	name    resource.Name
	logger  logging.Logger
	monitor sensor.Sensor
	usage   sensor.Sensor
	deck    resource.Resource
	page    string
	buttons []Button
	colors  Colors
	near    float64
	every   time.Duration
	now     func() time.Time

	cancel context.CancelFunc
	done   chan struct{}

	mu        sync.Mutex
	pushed    map[string]painted // what the deck was last told, by button name
	lastFull  time.Time
	lastError string
}

func newRefillDisplay(ctx context.Context, deps resource.Dependencies, rawConf resource.Config, logger logging.Logger) (resource.Resource, error) {
	conf, err := resource.NativeConfig[*Config](rawConf)
	if err != nil {
		return nil, err
	}
	mon, err := sensor.FromProvider(deps, conf.MonitorSensorName)
	if err != nil {
		return nil, fmt.Errorf("monitor_sensor_name %q: %w", conf.MonitorSensorName, err)
	}
	usage, err := sensor.FromProvider(deps, conf.UsageSensorName)
	if err != nil {
		return nil, fmt.Errorf("usage_sensor_name %q: %w", conf.UsageSensorName, err)
	}
	deck, err := generic.FromProvider(deps, conf.StreamDeckName)
	if err != nil {
		return nil, fmt.Errorf("stream_deck_name %q: %w", conf.StreamDeckName, err)
	}

	every := defaultPollInterval
	if conf.PollIntervalSeconds > 0 {
		every = time.Duration(conf.PollIntervalSeconds * float64(time.Second))
	}

	loopCtx, cancel := context.WithCancel(context.Background())
	r := &refillDisplay{
		name:    rawConf.ResourceName(),
		logger:  logger,
		monitor: mon,
		usage:   usage,
		deck:    deck,
		page:    conf.Page,
		buttons: conf.Buttons,
		colors:  conf.Colors.withDefaults(),
		near:    conf.NearPercent,
		every:   every,
		now:     time.Now,
		cancel:  cancel,
		done:    make(chan struct{}),
		pushed:  map[string]painted{},
	}
	go r.run(loopCtx)
	return r, nil
}

func (r *refillDisplay) Name() resource.Name {
	return r.name
}

func (r *refillDisplay) Close(context.Context) error {
	r.cancel()
	<-r.done
	return nil
}

func (r *refillDisplay) run(ctx context.Context) {
	defer close(r.done)
	r.sync(ctx, false)
	t := time.NewTicker(r.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.sync(ctx, false)
		}
	}
}

// Status reports what each button last showed.
func (r *refillDisplay) Status(context.Context) (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.statusLocked(), nil
}

func (r *refillDisplay) statusLocked() map[string]any {
	out := make(map[string]any, len(r.pushed))
	for name, p := range r.pushed {
		out[name] = map[string]any{"text": p.Text, "color": p.Color, "state": string(p.State)}
	}
	return out
}

// DoCommand supports:
//   - {"refill": "<button name>"} resets that button's counter to 0 and repaints.
//   - {"refresh": true} repaints every button now instead of waiting for the next
//     poll, and returns what each shows.
func (r *refillDisplay) DoCommand(ctx context.Context, cmd map[string]any) (map[string]any, error) {
	if v, ok := cmd["refill"]; ok {
		return r.refill(ctx, v)
	}
	if _, ok := cmd["refresh"]; ok {
		if err := r.sync(ctx, true); err != nil {
			return nil, err
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		return map[string]any{"buttons": r.statusLocked()}, nil
	}
	return nil, fmt.Errorf("unknown command, supported: refill, refresh")
}

// refill handles {"refill": "<button name>"}: it sets the button's counter back
// to 0, asks the monitor to re-evaluate now, and repaints. Resetting the counter
// is the part that matters, so it comes first and its failure is the only one
// returned as an error. If the monitor can't be asked or the deck can't be
// reached, the counter is still reset and the buttons catch up on the next poll,
// so pressing again is harmless.
func (r *refillDisplay) refill(ctx context.Context, v any) (map[string]any, error) {
	name, ok := v.(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("refill: expected a button name, got %v", v)
	}
	var button *Button
	names := make([]string, 0, len(r.buttons))
	for i := range r.buttons {
		names = append(names, r.buttons[i].Name)
		if r.buttons[i].Name == name {
			button = &r.buttons[i]
		}
	}
	if button == nil {
		return nil, fmt.Errorf("refill: no button named %q, have %s", name, strings.Join(names, ", "))
	}

	resetCtx, cancel := context.WithTimeout(ctx, callTimeout)
	_, err := r.usage.DoCommand(resetCtx, map[string]any{"merge": map[string]any{button.Reading: 0.0}})
	cancel()
	if err != nil {
		return nil, fmt.Errorf("refill %q: reset %s: %w", name, button.Reading, err)
	}
	r.logger.Infof("refilled %s (reset %s to 0)", name, button.Reading)

	out := map[string]any{"refill": name, "reading": button.Reading, "refreshed": true}

	checkCtx, cancel := context.WithTimeout(ctx, callTimeout)
	_, err = r.monitor.DoCommand(checkCtx, map[string]any{"check": true})
	cancel()
	if err != nil {
		r.logger.Warnf("refill %q: the counter was reset but the monitor did not re-check: %v", name, err)
		out["refreshed"] = false
	}
	if err := r.sync(ctx, false); err != nil {
		out["refreshed"] = false
	}
	return out, nil
}

// sync reads the monitor and sends the stream deck whatever has changed since
// the last send. force, or repaintEvery having passed, sends every button.
func (r *refillDisplay) sync(ctx context.Context, force bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	readCtx, cancel := context.WithTimeout(ctx, callTimeout)
	readings, err := r.monitor.Readings(readCtx, nil)
	cancel()
	if err != nil {
		return r.failLocked(fmt.Errorf("read monitor: %w", err))
	}

	now := r.now()
	full := force || now.Sub(r.lastFull) >= repaintEvery

	updates := map[string]any{}
	next := map[string]painted{}
	for _, b := range r.buttons {
		p, ok := render(b, readings, r.colors, r.near)
		if !ok {
			// The reading is missing or not a number. Leave the button as it is
			// rather than paint a guess over it.
			continue
		}
		if full || r.pushed[b.Name] != p {
			updates[b.Name] = map[string]any{"text": p.Text, "color": p.Color}
			next[b.Name] = p
		}
	}
	if len(updates) == 0 {
		r.lastError = ""
		return nil
	}

	payload := map[string]any{"keys": updates}
	if r.page != "" {
		payload["page"] = r.page
	}
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	_, err = r.deck.DoCommand(callCtx, map[string]any{"update_display": payload})
	cancel()
	if err != nil {
		return r.failLocked(fmt.Errorf("update stream deck: %w", err))
	}

	for name, p := range next {
		r.pushed[name] = p
	}
	if full {
		r.lastFull = now
	}
	r.lastError = ""
	return nil
}

// failLocked logs an error once per distinct message so a deck that is unplugged
// doesn't fill the log every poll, and returns it. Nothing is recorded as sent,
// so the next poll tries again.
func (r *refillDisplay) failLocked(err error) error {
	if msg := err.Error(); msg != r.lastError {
		r.logger.Warnf("refill display: %v", err)
		r.lastError = msg
	}
	return err
}

// state is how far a counter is from needing a refill.
type state string

const (
	stateFresh state = "fresh"
	stateOK    state = "ok"
	stateNear  state = "near"
	stateDue   state = "due"
)

// painted is what one button shows.
type painted struct {
	Text  string
	Color string
	State state
}

// stateOf classifies a counter. The monitor's rule is the authority on "due",
// so the threshold lives in one place; 0 is "fresh" (just refilled); "near" is
// the warning band just short of due; anything else is "ok".
func stateOf(value float64, triggered, near bool) state {
	switch {
	case triggered:
		return stateDue
	case value == 0:
		return stateFresh
	case near:
		return stateNear
	default:
		return stateOK
	}
}

// isNear reports whether value is within nearPercent of threshold, that is at
// least (100-nearPercent)% of the way there. It multiplies instead of building
// threshold*0.8 so a value sitting exactly on the line isn't lost to float
// rounding (20 against 25 at 20%).
func isNear(value, threshold, nearPercent float64) bool {
	if nearPercent <= 0 {
		return false
	}
	return value*100 >= threshold*(100-nearPercent)
}

// render works out what one button should show from the monitor's readings.
// nearPercent is the width of the "near" warning band (0 for none). It returns
// false if the button's reading is missing or not a number.
func render(b Button, readings map[string]any, colors Colors, nearPercent float64) (painted, bool) {
	value, ok := number(readings[b.Reading])
	if !ok {
		return painted{}, false
	}
	// The monitor may report "<key>_threshold" for a reading it has a rule for.
	// If it doesn't, fall back to the button's own threshold.
	threshold, hasLimit := number(readings[b.Reading+"_threshold"])
	if !hasLimit && b.Threshold > 0 {
		threshold, hasLimit = b.Threshold, true
	}

	// A rule's "<key>_triggered" flag is absent if the monitor has no rule for
	// the reading, which just means it can never be due.
	triggered, _ := readings[b.Reading+"_triggered"].(bool)
	// Outside a rule's active windows the monitor freezes its state, so the flag
	// would keep showing the last in-hours answer (red after a weekend refill).
	// Counters count up toward their limit, so judge by the count instead.
	if snoozed, _ := readings[b.Reading+"_snoozed"].(bool); snoozed && hasLimit {
		triggered = value >= threshold
	}

	st := stateOf(value, triggered, hasLimit && isNear(value, threshold, nearPercent))
	var color string
	switch st {
	case stateFresh:
		color = colors.Fresh
	case stateDue:
		color = colors.Due
	case stateNear:
		color = colors.Near
	default:
		color = colors.OK
	}

	label := b.Label
	if label == "" {
		label = b.Name
	}
	limit := "?"
	if hasLimit {
		limit = formatNumber(threshold)
	}
	text := b.Text
	if text == "" {
		text = defaultText
		if !hasLimit {
			text = defaultTextNoLimit
		}
	}
	text = strings.NewReplacer("{label}", label, "{value}", formatNumber(value), "{threshold}", limit).Replace(text)
	return painted{Text: text, Color: color, State: st}, true
}

// number reads a sensor value as a float64. Values arrive as float64 over the
// wire; the integer types are accepted defensively.
func number(v any) (float64, bool) {
	switch v := v.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}

// formatNumber prints a count without trailing zeros: 12, or 4.5 for a water
// brew that counts 1.5.
func formatNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
