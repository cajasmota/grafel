package substrate

import "testing"

// TestCallContextsPython proves that a guarded call (inside an `if`) is stamped
// conditional=true with its guarding condition, a call inside a `for` loop is
// in_loop=true, and an unconditional top-level call is absent from the map
// (treated as conditional=false by the caller). Mirrors the EffectContexts
// Python fixture but keyed on call-site lines (the CALLS edges' "line" props).
func TestCallContextsPython(t *testing.T) {
	src := `def sync(self, request):
    audit.log("start")
    if request.data.get("force"):
        notifier.send(request.user)
    for row in rows:
        mailer.deliver(row)
    return Response({"ok": True})
`
	// startLine = 100, so absolute lines:
	//   101 audit.log              (top-level, unconditional)
	//   103 notifier.send          (inside `if`)
	//   105 mailer.deliver         (inside `for`)
	const start = 100
	callLines := []int{101, 103, 105}
	ctxs := CallContextsFor("python", src, start, callLines)

	if _, conditional := ctxs[101]; conditional {
		t.Errorf("line 101 (audit.log) should be unconditional/absent, got %+v", ctxs[101])
	}

	send, ok := ctxs[103]
	if !ok {
		t.Fatalf("line 103 (notifier.send) should be conditional; map=%+v", ctxs)
	}
	if !send.Conditional {
		t.Errorf("notifier.send should be conditional, got %+v", send)
	}
	if send.Condition == "" {
		t.Errorf("notifier.send should carry its guarding condition, got %+v", send)
	}
	if send.InLoop {
		t.Errorf("notifier.send is not in a loop, got %+v", send)
	}

	deliver, ok := ctxs[105]
	if !ok {
		t.Fatalf("line 105 (mailer.deliver) should be in a loop; map=%+v", ctxs)
	}
	if !deliver.InLoop {
		t.Errorf("mailer.deliver should be in_loop (fan-out), got %+v", deliver)
	}
	if !deliver.Conditional {
		t.Errorf("mailer.deliver inside for-loop should be conditional, got %+v", deliver)
	}
}

// TestCallContextsGo proves the same classifier works on a brace-dialect
// language: a guarded call inside `if force { … }` is conditional with its
// no-paren condition text, a call inside a `for` loop is in_loop, and a
// top-level call is unconditional/absent.
func TestCallContextsGo(t *testing.T) {
	src := `func Sync(force bool, rows []Row) error {
	audit.Log("start")
	if force {
		notifier.Send(rows)
	}
	for _, row := range rows {
		mailer.Deliver(row)
	}
	return nil
}
`
	// startLine = 50:
	//   51 audit.Log         top-level
	//   53 notifier.Send     inside `if force {`
	//   56 mailer.Deliver    inside `for ... {`
	const start = 50
	callLines := []int{51, 53, 56}
	ctxs := CallContextsFor("go", src, start, callLines)

	if _, conditional := ctxs[51]; conditional {
		t.Errorf("line 51 (audit.Log) should be unconditional/absent, got %+v", ctxs[51])
	}

	send, ok := ctxs[53]
	if !ok {
		t.Fatalf("line 53 (notifier.Send) should be conditional; map=%+v", ctxs)
	}
	if !send.Conditional {
		t.Errorf("notifier.Send should be conditional, got %+v", send)
	}
	if send.Condition == "" {
		t.Errorf("notifier.Send should carry its guarding condition, got %+v", send)
	}
	if send.InLoop {
		t.Errorf("notifier.Send is not in a loop, got %+v", send)
	}

	deliver, ok := ctxs[56]
	if !ok {
		t.Fatalf("line 56 (mailer.Deliver) should be in a loop; map=%+v", ctxs)
	}
	if !deliver.InLoop {
		t.Errorf("mailer.Deliver should be in_loop (fan-out), got %+v", deliver)
	}
	if !deliver.Conditional {
		t.Errorf("mailer.Deliver inside for-loop should be conditional, got %+v", deliver)
	}
}

// TestCallContextsEmpty covers the honest-partial / no-op edges: empty source,
// no call lines, and a language with no block detector all return nil.
func TestCallContextsEmpty(t *testing.T) {
	if got := CallContextsFor("python", "", 1, []int{1}); got != nil {
		t.Errorf("empty source should return nil, got %+v", got)
	}
	if got := CallContextsFor("python", "def f():\n    pass\n", 1, nil); got != nil {
		t.Errorf("no call lines should return nil, got %+v", got)
	}
	// A top-level-only call falls in no block → nil map.
	if got := CallContextsFor("python", "def f():\n    g()\n", 1, []int{2}); got != nil {
		t.Errorf("top-level-only call should yield nil map, got %+v", got)
	}
}
