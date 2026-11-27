package toast_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/base/toast"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func ids[K comparable, V any](es []toast.Entry[K, V]) []K {
	var out []K
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}

func TestToastTimeoutPausesAndRemovalWaitsForTheExit(t *testing.T) {
	motion := toast.SonnerMotion()
	start := time.Unix(0, 0)
	var m toast.Manager[string, int]
	m.Push("a", 1, 5*time.Second, start)
	m.Advance(start.Add(motion.Duration), false)
	m.Advance(start.Add(motion.Duration+4*time.Second), true)
	if s := m.Entries()[0].Status; s != toast.Present {
		t.Fatalf("status = %v, want present while paused", s)
	}
	if a := m.Advance(start.Add(motion.Duration+9*time.Second), false); !slices.Equal(a.Ending, []string{"a"}) || len(a.Removed) > 0 {
		t.Fatalf("advance = %+v, want a ending and nothing removed", a)
	}
	a := m.Advance(start.Add(motion.Duration+motion.ExitDuration+9*time.Second), false)
	if len(a.Removed) != 1 || a.Removed[0].ID != "a" || a.Removed[0].Value != 1 || m.Len() != 0 {
		t.Fatalf("advance = %+v, want a removed after its exit", a)
	}
}

func TestToastLimitQueuesAndPushMergesInPlace(t *testing.T) {
	motion := toast.SonnerMotion()
	now := time.Unix(0, 0)
	var m toast.Manager[string, int]
	m.Limit = 1
	for i, id := range []string{"a", "b", "c"} {
		m.Push(id, i, 0, now)
	}
	// The queue shows the oldest and keeps the rest in turn.
	if got := ids(m.Visible()); !slices.Equal(got, []string{"a"}) {
		t.Errorf("visible = %v, want the queue's front", got)
	}
	if n := m.Waiting(); n != 2 {
		t.Errorf("waiting = %d, want 2", n)
	}
	// A toast of the same id merges into its own place, not the newest's.
	if old, ok := m.Push("b", 9, 0, now); !ok || old != 1 {
		t.Errorf("push = %v, %v, want the replaced value", old, ok)
	}
	if got := ids(m.Entries()); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("entries = %v, want the replaced toast in its place", got)
	}
	// A high priority shows before the ones waiting.
	m.PushPriority("e", 4, 0, toast.PriorityHigh, now)
	if got := ids(m.Visible()); !slices.Equal(got, []string{"e"}) {
		t.Errorf("visible = %v, want the error before the toasts waiting", got)
	}
	m.Dismiss("e", now)
	if got := ids(m.Visible()); !slices.Equal(got, []string{"a", "e"}) {
		t.Errorf("visible = %v, want the toast taking its place, and it as it leaves", got)
	}
	m.Advance(now.Add(motion.ExitDuration), false)
	if got := ids(m.Visible()); !slices.Equal(got, []string{"a"}) {
		t.Errorf("visible = %v, want the queue's front after the exit", got)
	}
	if got := m.DismissAll(now); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("dismiss all = %v, want those not ending yet", got)
	}
}

func TestToastWaitingKeepsItsTimeoutUntilItShows(t *testing.T) {
	motion := toast.SonnerMotion()
	start := time.Unix(0, 0)
	var m toast.Manager[string, int]
	m.Limit = 1
	m.Push("shown", 1, 5*time.Second, start)
	m.Push("waiting", 2, 5*time.Second, start)
	m.Advance(start.Add(motion.Duration), false)
	// The first expires while the second waits, its own timeout untouched.
	at := start.Add(motion.Duration + 5*time.Second)
	if a := m.Advance(at, false); !slices.Equal(a.Ending, []string{"shown"}) {
		t.Fatalf("advance = %+v, want the toast that timed out ending", a)
	}
	if got := ids(m.Visible()); !slices.Equal(got, []string{"shown", "waiting"}) {
		t.Fatalf("visible = %v, want the waiting toast taking the leaving toast's slot", got)
	}
	gone := m.Advance(at.Add(motion.ExitDuration), false)
	if len(gone.Removed) != 1 || gone.Removed[0].ID != "shown" {
		t.Fatalf("removed = %+v, want the toast that timed out", gone.Removed)
	}
	if got := ids(m.Visible()); !slices.Equal(got, []string{"waiting"}) {
		t.Fatalf("visible = %v, want the waiting toast", got)
	}
	if s := m.Entries()[0].Status; s != toast.Starting {
		t.Fatalf("status = %v, want starting during its entry motion", s)
	}
	// Once its entry finishes, its full 5 s timeout counts down from there.
	if a := m.Advance(at.Add(motion.Duration), false); !slices.Equal(a.Presented, []string{"waiting"}) {
		t.Fatalf("advance = %+v, want waiting presented after Duration", a)
	}
	presentAt := at.Add(motion.Duration)
	m.Advance(presentAt.Add(4*time.Second), false)
	if s := m.Entries()[0].Status; s != toast.Present {
		t.Errorf("status = %v, want present 4 s into its 5", s)
	}
	if a := m.Advance(presentAt.Add(5*time.Second), false); !slices.Equal(a.Ending, []string{"waiting"}) {
		t.Errorf("advance = %+v, want ending once its 5 s timeout ran out", a)
	}
}

func TestToastDismissWaitingNeverShows(t *testing.T) {
	motion := toast.SonnerMotion()
	now := time.Unix(0, 0)
	var m toast.Manager[string, int]
	m.Limit = 1
	m.Push("a", 1, 0, now)
	m.Push("b", 2, 0, now)
	m.Push("c", 3, 0, now)
	if !m.Dismiss("b", now) {
		t.Fatal("dismissing a waiting toast returned false")
	}
	if m.Dismiss("b", now) {
		t.Fatal("dismissing an already removed waiting toast returned true")
	}
	if got := ids(m.Visible()); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("visible = %v, want only the shown toast", got)
	}
	if n := m.Waiting(); n != 1 {
		t.Fatalf("waiting = %d, want 1", n)
	}
	if a := m.Advance(now, false); !a.Changed || len(a.Removed) != 1 || a.Removed[0].ID != "b" || a.Removed[0].Value != 2 {
		t.Fatalf("advance = %+v, want b reported removed", a)
	}
	if got := m.DismissAll(now); !slices.Equal(got, []string{"a", "c"}) {
		t.Fatalf("dismiss all = %v, want [a c]", got)
	}
	if got := ids(m.Visible()); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("visible after dismissAll = %v, want only the toast that was showing", got)
	}
	if n := m.Waiting(); n != 0 {
		t.Fatalf("waiting after dismissAll = %d, want 0", n)
	}
	if a := m.Advance(now, false); !a.Changed || len(a.Removed) != 1 || a.Removed[0].ID != "c" {
		t.Fatalf("advance after dismissAll = %+v, want waiting toast c removed immediately", a)
	}
	if a := m.Advance(now.Add(motion.ExitDuration), false); len(a.Removed) != 1 || a.Removed[0].ID != "a" || m.Len() != 0 {
		t.Fatalf("advance after exit = %+v, len = %d", a, m.Len())
	}
}

func TestToastDismissPromotionAndPreemptionKeepTime(t *testing.T) {
	motion := toast.SonnerMotion()
	t0 := time.Unix(0, 0)
	var m toast.Manager[string, int]
	m.Limit = 1
	m.Push("a", 1, 0, t0)
	m.Push("b", 2, 5*time.Second, t0)
	m.Advance(t0.Add(motion.Duration), false)

	// Dismissing "a" after a quiet gap promotes "b" with a fresh entry clock.
	t1 := t0.Add(3 * time.Second)
	m.Dismiss("a", t1)
	m.Advance(t1.Add(motion.ExitDuration), false)
	if got := m.Entries(); len(got) != 1 || got[0].ID != "b" || got[0].Status != toast.Starting {
		t.Fatalf("entries = %+v, want b still starting after ExitDuration", got)
	}
	t2 := t1.Add(motion.Duration)
	if a := m.Advance(t2, false); !slices.Equal(a.Presented, []string{"b"}) {
		t.Fatalf("advance = %+v, want b presented after Duration", a)
	}

	// Count 1 s off "b", then preempt it with a high-priority toast.
	tPreempt := t2.Add(time.Second)
	m.Advance(tPreempt, false)
	m.PushPriority("urgent", 3, 0, toast.PriorityHigh, tPreempt)
	if got := ids(m.Visible()); !slices.Equal(got, []string{"urgent"}) {
		t.Fatalf("visible = %v, want urgent", got)
	}
	m.Advance(tPreempt.Add(motion.Duration), false)

	// Dismiss "urgent" 10 s later; "b" must return with its remaining 4 s intact.
	tReturn := tPreempt.Add(10 * time.Second)
	m.Dismiss("urgent", tReturn)
	m.Advance(tReturn.Add(motion.ExitDuration), false)
	if got := m.Entries(); len(got) != 1 || got[0].ID != "b" || got[0].Status != toast.Present {
		t.Fatalf("entries after preemption = %+v, want b still present", got)
	}
	m.Advance(tReturn.Add(3500*time.Millisecond), false)
	if s := m.Entries()[0].Status; s != toast.Present {
		t.Fatalf("status = %v, want b still present with 500 ms left", s)
	}
	if a := m.Advance(tReturn.Add(4*time.Second), false); !slices.Equal(a.Ending, []string{"b"}) {
		t.Fatalf("advance = %+v, want b ending after its remaining 4 s", a)
	}
}

func TestToastLimitChangeAndNegativeMotion(t *testing.T) {
	motion := toast.SonnerMotion()
	t0 := time.Unix(0, 0)
	var m toast.Manager[string, int]
	m.Limit = 1
	m.Push("a", 1, 0, t0)
	m.Push("b", 2, 0, t0)
	m.Advance(t0.Add(motion.Duration), false)

	// Raising Limit reports a change and starts "b" fresh from now.
	m.Limit = 2
	t1 := t0.Add(5 * time.Second)
	if a := m.Advance(t1, false); !a.Changed || !slices.Equal(ids(m.Visible()), []string{"a", "b"}) || m.Waiting() != 0 {
		t.Fatalf("after raising limit: advance=%+v visible=%v waiting=%d", a, ids(m.Visible()), m.Waiting())
	}
	if s := m.Entries()[1].Status; s != toast.Starting {
		t.Fatalf("b status = %v, want starting immediately after limit increase", s)
	}

	// Lowering Limit hides "b" again and reports a change.
	m.Limit = 1
	if a := m.Advance(t1.Add(100*time.Millisecond), false); !a.Changed || !slices.Equal(ids(m.Visible()), []string{"a"}) || m.Waiting() != 1 {
		t.Fatalf("after lowering limit: advance=%+v visible=%v waiting=%d", a, ids(m.Visible()), m.Waiting())
	}

	// Negative durations disable entry and exit waits.
	var still toast.Manager[string, int]
	still.Motion = toast.Motion{Duration: -1, ExitDuration: -1}
	still.Push("x", 1, 0, t0)
	if a := still.Advance(t0, false); !slices.Equal(a.Presented, []string{"x"}) {
		t.Fatalf("negative duration advance = %+v, want immediate present", a)
	}
	still.Dismiss("x", t0)
	if a := still.Advance(t0, false); len(a.Removed) != 1 || a.Removed[0].ID != "x" || still.Len() != 0 {
		t.Fatalf("negative exit duration advance = %+v, want immediate removal", a)
	}
}

func TestToastStack(t *testing.T) {
	type entry = toast.Entry[int, string]
	var dismissed []int
	var expanded []bool
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shown := core.NewSignalFunc(cx, []entry{{1, "one", toast.Present, toast.PriorityNormal}, {2, "two", toast.Present, toast.PriorityNormal}}, slices.Equal)
		return core.View{Style: core.Style{FlexGrow: 1, Padding: core.All(20)}, Children: []core.Node{
			toast.Stack[entry, int]{TestID: "stack", Toasts: shown.Get, Key: func(e entry) int { return e.ID },
				Style:            core.Style{Width: core.Pt(200)},
				OnExpandedChange: func(e bool) { expanded = append(expanded, e) },
				Toast: func(cx *core.Scope, e func() entry) core.Node {
					id := core.Untracked(cx, e).ID
					return toast.Toast{TestID: fmt.Sprint("toast-", id), Label: core.Untracked(cx, e).Value,
						Status:           func() toast.Status { return e().Status },
						OnDismissRequest: func() { dismissed = append(dismissed, id) },
						Style:            core.Style{Height: core.Pt(core.Points(30 + 10*id))}}
				}},
		}}
	})
	// Collapsed: the newest (50) in front, the older (40) 14 behind it.
	if h := app.Find("id:stack").Frame.Size.Height; h != 64 {
		t.Errorf("collapsed height = %v, want 64", h)
	}
	if v := app.Find("id:toast-1"); v.Props.Role != core.RoleAlert {
		t.Errorf("role = %v, want alert", v.Props.Role)
	}
	app.Hover("id:stack")
	if h := app.Find("id:stack").Frame.Size.Height; h != 40+14+50 {
		t.Errorf("expanded height = %v, want both toasts and the gap", h)
	}
	app.HoverOut()
	if !slices.Equal(expanded, []bool{true, false}) {
		t.Errorf("expanded changes = %v", expanded)
	}
	app.Drag("id:toast-2", core.Point{X: 30}, core.Point{X: 80})
	if !slices.Equal(dismissed, []int{2}) {
		t.Errorf("dismissed = %v, want the swiped toast", dismissed)
	}
	app.Drag("id:toast-2", core.Point{Y: 80})
	if len(dismissed) != 1 {
		t.Errorf("a drag across the swipe dismissed %v", dismissed)
	}
}
