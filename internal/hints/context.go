package hints

import (
	"strings"
	"sync"
	"time"
)

// CallEvent is one record on the session's rolling history. The engine
// builds a fresh CallEvent per tool invocation and appends it to the
// Context before evaluation so rules can reason about prior calls.
//
// Fields are intentionally minimal: anything downstream rules need must
// be derivable from these or the raw args/result strings.
type CallEvent struct {
	// Tool is the MCP / CLI tool name, e.g. "lore_inscribe".
	Tool string
	// Args is a string-keyed view of the tool's input arguments. Values
	// are stored as whatever the caller passed (string/int/bool/slice).
	// Rules read known keys by string indexing — type assertions happen
	// at the rule level, not here.
	Args map[string]any
	// Output is the rendered tool response (post-Format). Used by rules
	// that need to peek at the response body (rare).
	Output string
	// IsError is true when the handler returned a non-nil error.
	IsError bool
	// Timestamp is when the call completed.
	Timestamp time.Time
}

// StringArg returns the string form of e.Args[key], handling nil and
// non-string types defensively. Unset/nil returns "".
func (e CallEvent) StringArg(key string) string {
	v, ok := e.Args[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// HasArg reports whether key is present AND non-empty.
func (e CallEvent) HasArg(key string) bool {
	if v, ok := e.Args[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s) != ""
		}
		return true
	}
	return false
}

// Context is the session-scoped state the engine threads between calls.
// Per-PID (the MCP server or the CLI process). Safe for concurrent use.
type Context struct {
	mu sync.Mutex

	// sessionID is a stable id for this process's lifetime. We use PID
	// as the default; tests override.
	sessionID string
	// era is the invocation surface (MCP vs Bash CLI).
	era Era
	// events is the rolling ring-buffer of CallEvents. Bounded to keep
	// memory flat over long-running MCP servers.
	events []CallEvent
	// maxEvents caps the history length. 256 is plenty — the longest
	// follow-through window any rule uses is 10.
	maxEvents int

	// callCount is the total number of events seen (for cooldown age).
	callCount int

	// lastFire is "per-rule last callCount it fired at", used for the
	// cooldown window check.
	lastFire map[string]int

	// fyiFiresThisSession is the running count of ℹ️ fyi hints fired so
	// far, for the per-session cap (3 per ENTRY-29).
	fyiFiresThisSession int

	// seenSessionStart is set true the first time guild_session_start or
	// quest_bounties runs in this Context's lifetime. The no-session-start
	// rule suppresses once this flips.
	seenSessionStart bool
}

// NewContext builds a Context with the given session id and era. A zero
// maxEvents defaults to 256.
func NewContext(sessionID string, era Era) *Context {
	return &Context{
		sessionID: sessionID,
		era:       era,
		maxEvents: 256,
		lastFire:  map[string]int{},
	}
}

// SessionID returns the session id this Context was built with.
func (c *Context) SessionID() string {
	if c == nil {
		return ""
	}
	return c.sessionID
}

// Era returns the invocation surface for this Context.
func (c *Context) Era() Era {
	if c == nil {
		return EraMCP
	}
	return c.era
}

// CallCount returns the number of events observed so far.
func (c *Context) CallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callCount
}

// Events returns a snapshot copy of the most recent n events (or all
// events if n <= 0 or n > len(events)).
func (c *Context) Events(n int) []CallEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.eventsLocked(n)
}

func (c *Context) eventsLocked(n int) []CallEvent {
	if len(c.events) == 0 {
		return nil
	}
	if n <= 0 || n > len(c.events) {
		n = len(c.events)
	}
	out := make([]CallEvent, n)
	copy(out, c.events[len(c.events)-n:])
	return out
}

// RecordEvent appends ev to the history and bumps CallCount. Returns the
// new callCount.
func (c *Context) RecordEvent(ev CallEvent) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
	if len(c.events) > c.maxEvents {
		// Drop the oldest events in one pass to keep memory flat.
		drop := len(c.events) - c.maxEvents
		c.events = append(c.events[:0], c.events[drop:]...)
	}
	c.callCount++
	// Flip seenSessionStart when the bootstrap tools run.
	if ev.Tool == "guild_session_start" || ev.Tool == "quest_bounties" {
		c.seenSessionStart = true
	}
	return c.callCount
}

// SeenSessionStart reports whether guild_session_start / quest_bounties
// has been observed in this session. Used by the no-session-start rule.
func (c *Context) SeenSessionStart() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seenSessionStart
}

// RecentlyCalled reports whether any event in the last n events invoked
// a tool whose name is in toolNames. Used for contextual suppression
// (the agent already did the suggested action).
func (c *Context) RecentlyCalled(n int, toolNames ...string) bool {
	if len(toolNames) == 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	set := map[string]struct{}{}
	for _, t := range toolNames {
		set[t] = struct{}{}
	}
	events := c.eventsLocked(n)
	// Skip the LAST event — that's the triggering call itself.
	if len(events) > 0 {
		events = events[:len(events)-1]
	}
	for _, e := range events {
		if _, ok := set[e.Tool]; ok {
			return true
		}
	}
	return false
}

// RuleFiredWithin reports whether rule ruleID fired within the last
// cooldownCalls events. Used to enforce the cooldown window.
func (c *Context) RuleFiredWithin(ruleID string, cooldownCalls int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	last, ok := c.lastFire[ruleID]
	if !ok {
		return false
	}
	return c.callCount-last < cooldownCalls
}

// MarkFired updates the last-fire counter for ruleID to the current call
// count. Call this after a hint renders successfully.
func (c *Context) MarkFired(ruleID string, severity Severity) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastFire[ruleID] = c.callCount
	if severity == SeverityFYI {
		c.fyiFiresThisSession++
	}
}

// FYIFiresThisSession returns how many ℹ️ fyi hints have fired in the
// session so far.
func (c *Context) FYIFiresThisSession() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fyiFiresThisSession
}
