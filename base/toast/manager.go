package toast

import (
	"cmp"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// Toasts follow NSH's nsh/base, which has gpui-base's three pieces with
// their names and semantics: Manager keeps the toasts and their
// lifecycle, Stack lays them out, and Toast is each one's root. The
// look and the content are the app's, and so is showing a toast from
// anywhere: an app builds its own toast(...) on these, as the gallery
// does.

// Status is a toast's phase in its lifecycle.
type Status uint8

const (
	// Starting is a toast entering, for the motion's Duration.
	Starting Status = iota
	// Present is a toast shown, counting its timeout down.
	Present
	// Ending is a toast leaving, for the motion's ExitDuration,
	// before it is removed.
	Ending
)

// Priority orders the toasts waiting to show: a higher one shows before
// every lower one, and the oldest of one priority before the newer.
type Priority int

const (
	// PriorityDefault leaves the priority to the caller's default:
	// PriorityNormal in a Manager, and PriorityHigh in ui.Toaster for an
	// error or a toast that stays until it is dismissed.
	PriorityDefault Priority = iota
	// PriorityNormal is a toast shown in turn, even on an error or a toast
	// that stays until it is dismissed.
	PriorityNormal
	// PriorityHigh shows before the toasts waiting of PriorityNormal, as
	// an error or the progress of a running job does.
	PriorityHigh
)

// Motion are the tokens of a Manager and a Stack; a zero
// field keeps Sonner's, which SonnerMotion returns, and a negative
// Duration or ExitDuration disables that transition.
type Motion struct {
	// Duration is how long a toast enters, and the stack's springs'
	// response.
	Duration time.Duration
	// ExitDuration is how long a toast leaves.
	ExitDuration time.Duration
	// CollapsedPeek is how far apart collapsed toasts overlap, and
	// ExpandedGap the space between expanded ones.
	CollapsedPeek, ExpandedGap core.Points
	// CollapsedScaleStep is how much narrower each collapsed toast behind
	// the newest is, as a fraction of its width.
	CollapsedScaleStep float32
	// CollapsedVisible is how many collapsed toasts show.
	CollapsedVisible int
}

// SonnerMotion returns Sonner's motion.
func SonnerMotion() Motion {
	return Motion{Duration: 400 * time.Millisecond, ExitDuration: 200 * time.Millisecond,
		CollapsedPeek: 14, ExpandedGap: 14, CollapsedScaleStep: 0.05, CollapsedVisible: 3}
}

// resolved returns m with Sonner's values for its zero fields, and 0 for
// negative enter or exit durations.
func (m Motion) resolved() Motion {
	s := SonnerMotion()
	if m.Duration == 0 {
		m.Duration = s.Duration
	} else if m.Duration < 0 {
		m.Duration = 0
	}
	if m.ExitDuration == 0 {
		m.ExitDuration = s.ExitDuration
	} else if m.ExitDuration < 0 {
		m.ExitDuration = 0
	}
	if m.CollapsedPeek == 0 {
		m.CollapsedPeek = s.CollapsedPeek
	}
	if m.ExpandedGap == 0 {
		m.ExpandedGap = s.ExpandedGap
	}
	if m.CollapsedScaleStep == 0 {
		m.CollapsedScaleStep = s.CollapsedScaleStep
	}
	if m.CollapsedVisible == 0 {
		m.CollapsedVisible = s.CollapsedVisible
	}
	return m
}

// Entry is a toast a Manager keeps.
type Entry[K comparable, V any] struct {
	ID       K
	Value    V
	Status   Status
	Priority Priority
}

// Advance is what an Advance changed: the toasts it presented, the
// ones whose timeout ran out, and the ones it removed.
type Advance[K comparable, V any] struct {
	Changed   bool
	Presented []K
	Ending    []K
	Removed   []Entry[K, V]
}

type managedToast[K comparable, V any] struct {
	Entry[K, V]
	timeout   time.Duration // left; none when 0 and not timed
	timed     bool
	elapsed   time.Duration // in the transition
	lastCheck time.Time
	visible   bool
}

// Manager keeps toasts in order with their lifecycle, as gpui-base's:
// each one starts, is present once the motion's Duration has passed,
// counts its timeout down while present and not paused, and stays ending
// for the ExitDuration before it is removed. It renders nothing and runs
// no timer: the app pushes toasts, renders Visible with a Stack and
// calls Advance on a timer while any is shown, passing whether the stack
// is expanded. Times are the app's clock's, Scope.Now, so a test's manual
// clock moves them. The zero value has no toasts and Sonner's motion.
//
// A Limit makes the toasts a queue: the oldest of the highest priority
// show, and those beyond the limit wait in their place, their motion and
// their timeout not started until they show, as a list of notifications
// does. Waiting says how many wait.
type Manager[K comparable, V any] struct {
	Motion Motion
	// Limit is how many toasts show at once; 0 shows every one.
	Limit   int
	entries []managedToast[K, V]
	// removed holds waiting toasts dismissed without ever showing, to be
	// reported by the next Advance.
	removed []Entry[K, V]
}

// Push adds a toast, and returns the value of the one with its id, which
// it merges into rather than adding another: that one keeps its place,
// and one leaving shows again. The timeout and priority are the newest
// ones'. A timeout of 0 keeps it until it is dismissed.
func (m *Manager[K, V]) Push(id K, value V, timeout time.Duration, now time.Time) (replaced V, ok bool) {
	return m.PushPriority(id, value, timeout, PriorityNormal, now)
}

// PushPriority is Push with a priority: a PriorityHigh toast shows before
// the toasts waiting of PriorityNormal.
func (m *Manager[K, V]) PushPriority(id K, value V, timeout time.Duration, priority Priority, now time.Time) (replaced V, ok bool) {
	if priority == PriorityDefault {
		priority = PriorityNormal
	}
	for i := range m.entries {
		e := &m.entries[i]
		if e.ID == id {
			replaced, ok = e.Value, true
			e.Value, e.Priority, e.timeout, e.timed, e.lastCheck = value, priority, timeout, timeout > 0, now
			if e.Status == Ending {
				e.Status, e.elapsed = Starting, 0
			}
			m.syncShown(now)
			return replaced, ok
		}
	}
	m.entries = append(m.entries, managedToast[K, V]{Entry: Entry[K, V]{ID: id, Value: value, Priority: priority},
		timeout: timeout, timed: timeout > 0, lastCheck: now})
	m.syncShown(now)
	return replaced, ok
}

// Dismiss begins a shown toast's exit, or removes a waiting toast directly
// without showing it, and reports whether it was not leaving already.
func (m *Manager[K, V]) Dismiss(id K, now time.Time) bool {
	shown := m.shown()
	for i := range m.entries {
		if e := &m.entries[i]; e.ID == id {
			if e.Status == Ending {
				return false
			}
			if !shown[i] {
				m.removed = append(m.removed, e.Entry)
				m.entries = slices.Delete(m.entries, i, i+1)
				m.syncShown(now)
				return true
			}
			e.end(now)
			m.syncShown(now)
			return true
		}
	}
	return false
}

// DismissAll begins every shown toast's exit, removes waiting toasts
// directly without showing them, and returns the ids of those that were
// not leaving already.
func (m *Manager[K, V]) DismissAll(now time.Time) []K {
	shown := m.shown()
	var ids []K
	kept := m.entries[:0]
	for i := range m.entries {
		e := m.entries[i]
		if e.Status == Ending {
			kept = append(kept, e)
			continue
		}
		ids = append(ids, e.ID)
		if !shown[i] {
			m.removed = append(m.removed, e.Entry)
			continue
		}
		e.end(now)
		kept = append(kept, e)
	}
	clear(m.entries[len(kept):])
	m.entries = kept
	return ids
}

func (e *managedToast[K, V]) end(now time.Time) {
	e.Status, e.elapsed, e.lastCheck = Ending, 0, now
}

// syncShown reconciles each entry's visible flag against shown(), resetting
// lastCheck to now whenever an entry newly enters view so time spent
// waiting or preempted is never charged against its motion or timeout.
func (m *Manager[K, V]) syncShown(now time.Time) bool {
	shown := m.shown()
	changed := false
	for i := range m.entries {
		e := &m.entries[i]
		if shown[i] != e.visible {
			changed = true
			if shown[i] {
				e.lastCheck = now
			}
			e.visible = shown[i]
		}
	}
	return changed
}

// Advance moves the lifecycle on to now; paused holds present toasts'
// timeouts, as while the stack is expanded or the app is in the
// background. A toast waiting behind Limit's does not move at all.
func (m *Manager[K, V]) Advance(now time.Time, paused bool) Advance[K, V] {
	motion := m.Motion.resolved()
	var a Advance[K, V]
	if len(m.removed) > 0 {
		a.Removed = append(a.Removed, m.removed...)
		m.removed = nil
		a.Changed = true
	}
	if m.syncShown(now) {
		a.Changed = true
	}
	for i := range m.entries {
		e := &m.entries[i]
		if e.Status != Ending && !e.visible {
			e.lastCheck = now
			continue // waiting: its motion and timeout start when it shows
		}
		delta := max(now.Sub(e.lastCheck), 0)
		e.lastCheck = now
		switch e.Status {
		case Starting:
			if e.elapsed += delta; e.elapsed >= motion.Duration {
				e.Status, e.elapsed = Present, 0
				a.Presented = append(a.Presented, e.ID)
				a.Changed = true
			}
		case Present:
			if !paused && e.timed {
				if e.timeout -= delta; e.timeout <= 0 {
					e.end(now)
					a.Ending = append(a.Ending, e.ID)
					a.Changed = true
				}
			}
		case Ending:
			e.elapsed += delta
		}
	}
	kept := m.entries[:0]
	for _, e := range m.entries {
		if e.Status == Ending && e.elapsed >= motion.ExitDuration {
			a.Removed = append(a.Removed, e.Entry)
			a.Changed = true
		} else {
			kept = append(kept, e)
		}
	}
	clear(m.entries[len(kept):])
	m.entries = kept
	if m.syncShown(now) {
		a.Changed = true
	}
	return a
}

// Entries returns every toast, oldest first.
func (m *Manager[K, V]) Entries() []Entry[K, V] {
	out := make([]Entry[K, V], len(m.entries))
	for i, e := range m.entries {
		out[i] = e.Entry
	}
	return out
}

// Visible returns the toasts a Stack shows, oldest first: those Limit
// leaves room for, which are the highest priorities and then the oldest,
// and every ending one as it leaves.
func (m *Manager[K, V]) Visible() []Entry[K, V] {
	shown := m.shown()
	var out []Entry[K, V]
	for i, e := range m.entries {
		if shown[i] {
			out = append(out, e.Entry)
		}
	}
	return out
}

// Waiting returns how many toasts wait behind the ones showing, for a
// stack that shows how many are to come.
func (m *Manager[K, V]) Waiting() int {
	shown := m.shown()
	n := 0
	for i, e := range m.entries {
		if e.Status != Ending && !shown[i] {
			n++
		}
	}
	return n
}

// shown marks the toasts that show: with no Limit every one, else the
// highest priorities up to it, the oldest of one priority first, and
// every ending toast while it leaves.
func (m *Manager[K, V]) shown() []bool {
	shown := make([]bool, len(m.entries))
	if m.Limit <= 0 {
		for i := range shown {
			shown[i] = true
		}
		return shown
	}
	var waiting []int
	for i, e := range m.entries {
		if e.Status == Ending {
			shown[i] = true
			continue
		}
		waiting = append(waiting, i)
	}
	slices.SortStableFunc(waiting, func(a, b int) int {
		if c := cmp.Compare(m.entries[b].Priority, m.entries[a].Priority); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	for i, at := range waiting {
		if i >= m.Limit {
			break
		}
		shown[at] = true
	}
	return shown
}

// Get returns the value of the toast with id.
func (m *Manager[K, V]) Get(id K) (v V, ok bool) {
	for _, e := range m.entries {
		if e.ID == id {
			return e.Value, true
		}
	}
	return v, false
}

// Len returns how many toasts it keeps.
func (m *Manager[K, V]) Len() int { return len(m.entries) }
