package types

import (
	"encoding/json"
	"reflect"
	"runtime"
	"sort"
	"testing"
)

// measureAlloc returns the bytes allocated by f, as seen by the runtime.
func measureAlloc(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// oracle mirrors the map[string]string semantics that Props must reproduce
// exactly. Every Props behaviour below is asserted against it.
type oracle map[string]string

func (o oracle) get(k string) string { return o[k] }
func (o oracle) lookup(k string) (string, bool) {
	v, ok := o[k]
	return v, ok
}
func (o oracle) sortedKeys() []string {
	ks := make([]string, 0, len(o))
	for k := range o {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func TestPropsMatchesMapOracle(t *testing.T) {
	cases := []struct {
		name string
		ops  [][2]string // ordered key/value assignments
	}{
		{"empty", nil},
		{"one", [][2]string{{"language", "go"}}},
		{"two", [][2]string{{"language", "go"}, {"line", "42"}}},
		{"two-reverse-insert", [][2]string{{"line", "42"}, {"language", "go"}}},
		{"many", [][2]string{
			{"zeta", "1"}, {"alpha", "2"}, {"mid", "3"},
			{"beta", "4"}, {"omega", "5"}, {"gamma", "6"},
		}},
		{"overwrite", [][2]string{{"k", "v1"}, {"k", "v2"}, {"k", "v3"}}},
		{"overwrite-mixed", [][2]string{
			{"b", "1"}, {"a", "1"}, {"b", "2"}, {"c", "1"}, {"a", "2"},
		}},
		{"empty-values", [][2]string{{"a", ""}, {"b", "x"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := oracle{}
			var got Props
			for _, kv := range tc.ops {
				want[kv[0]] = kv[1]
				got.Set(kv[0], kv[1])
			}

			if got.Len() != len(want) {
				t.Fatalf("Len() = %d, want %d", got.Len(), len(want))
			}

			// Get / Lookup parity, including a guaranteed miss.
			for k, v := range want {
				if g := got.Get(k); g != v {
					t.Errorf("Get(%q) = %q, want %q", k, g, v)
				}
				gv, gok := got.Lookup(k)
				wv, wok := want.lookup(k)
				if gv != wv || gok != wok {
					t.Errorf("Lookup(%q) = (%q,%v), want (%q,%v)", k, gv, gok, wv, wok)
				}
			}
			if g := got.Get("__missing__"); g != want.get("__missing__") {
				t.Errorf("Get(missing) = %q, want %q", g, want.get("__missing__"))
			}
			if gv, gok := got.Lookup("__missing__"); gv != "" || gok {
				t.Errorf("Lookup(missing) = (%q,%v), want (\"\",false)", gv, gok)
			}

			// Range yields key-sorted order, deterministically.
			var keys []string
			got.Range(func(k, v string) bool {
				keys = append(keys, k)
				if v != want[k] {
					t.Errorf("Range gave %q=%q, want %q", k, v, want[k])
				}
				return true
			})
			if len(keys) != len(want) || (len(keys) > 0 && !reflect.DeepEqual(keys, want.sortedKeys())) {
				t.Errorf("Range order = %v, want %v", keys, want.sortedKeys())
			}

			// Snapshot round-trips to an equal, independent map.
			snap := got.Snapshot()
			if len(want) == 0 {
				if snap != nil {
					t.Errorf("Snapshot() = %v, want nil for empty", snap)
				}
			} else if !reflect.DeepEqual(snap, map[string]string(want)) {
				t.Errorf("Snapshot() = %v, want %v", snap, want)
			}
			if snap != nil {
				snap["__mutated__"] = "x"
				if got.Len() != len(want) {
					t.Errorf("mutating Snapshot() leaked into Props")
				}
			}

			// PropsFromMap reproduces the same sorted state.
			from := PropsFromMap(map[string]string(want))
			if !reflect.DeepEqual(from, got) {
				t.Errorf("PropsFromMap = %v, want %v", from, got)
			}
		})
	}
}

func TestPropsRangeEarlyStop(t *testing.T) {
	var p Props
	p.Set("a", "1")
	p.Set("b", "2")
	p.Set("c", "3")
	var seen []string
	p.Range(func(k, v string) bool {
		seen = append(seen, k)
		return k != "b"
	})
	if !reflect.DeepEqual(seen, []string{"a", "b"}) {
		t.Errorf("early stop visited %v, want [a b]", seen)
	}
}

func TestPropsDelete(t *testing.T) {
	want := map[string]string{"a": "1", "b": "2", "c": "3"}
	var p Props
	for k, v := range want {
		p.Set(k, v)
	}
	p.Delete("b")
	delete(want, "b")
	p.Delete("__missing__") // no-op, matches map delete semantics
	if !reflect.DeepEqual(p.Snapshot(), want) {
		t.Errorf("after Delete = %v, want %v", p.Snapshot(), want)
	}
	var zero Props
	zero.Delete("anything") // must not panic on nil
	if zero.Len() != 0 {
		t.Errorf("nil Props Delete changed Len")
	}
}

func TestPropsZeroValueSafe(t *testing.T) {
	var p Props
	if p.Len() != 0 || p.Get("x") != "" || p.Snapshot() != nil {
		t.Errorf("zero-value Props not inert")
	}
	p.Range(func(k, v string) bool { t.Errorf("zero Props ranged %q", k); return true })
}

// TestPropsJSONWireShapeIdenticalToMap is the byte-identity guard: the
// serialized form must be indistinguishable from the map it replaces, so
// the on-disk graph and every golden fixture stay unchanged.
func TestPropsJSONWireShapeIdenticalToMap(t *testing.T) {
	cases := []map[string]string{
		nil,
		{"language": "go"},
		{"language": "go", "line": "42"},
		{"z": "1", "a": "2", "m": "3", "b": "4"},
		{"quote\"key": "va\\lue", "uni¢ode": "ok"},
	}
	for _, m := range cases {
		wantJSON, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		p := PropsFromMap(m)
		gotJSON, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("Marshal(Props) = %s, want %s", gotJSON, wantJSON)
		}

		var back Props
		if err := json.Unmarshal(wantJSON, &back); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(back, p) {
			t.Errorf("Unmarshal(%s) = %v, want %v", wantJSON, back, p)
		}
		// Re-marshalling the decoded value is byte-stable.
		reJSON, err := json.Marshal(back)
		if err != nil {
			t.Fatal(err)
		}
		if string(reJSON) != string(wantJSON) {
			t.Errorf("round-trip Marshal = %s, want %s", reJSON, wantJSON)
		}
	}
}

// TestEmptyPropsOmittedLikeEmptyMap pins the wire shape at the struct-field
// level: an unset/empty property set is omitted by `omitempty` for both the
// map and Props spellings, so no record gains or loses a "properties" key.
func TestEmptyPropsOmittedLikeEmptyMap(t *testing.T) {
	mapForm := struct {
		P map[string]string `json:"properties,omitempty"`
	}{}
	propsForm := struct {
		P Props `json:"properties,omitempty"`
	}{}
	for _, m := range []map[string]string{nil, {}} {
		mapForm.P = m
		propsForm.P = PropsFromMap(m)
		want, err := json.Marshal(mapForm)
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(propsForm)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("empty(%v): got %s, want %s", m, got, want)
		}
	}
}

// propsSink defeats escape analysis so AllocsPerRun measures the real cost
// of building a property set rather than a dead local the compiler removes.
var (
	propsSink Props
	mapSink   map[string]string
)

// TestPropsSmallSetDoesNotAllocateMap is the whole point of the change:
// the common 1-2 property case must not pay Go hmap/bucket overhead.
func TestPropsSmallSetDoesNotAllocateMap(t *testing.T) {
	mapAllocs1 := testing.AllocsPerRun(1000, func() {
		mapSink = map[string]string{"language": "go"}
	})
	propsAllocs1 := testing.AllocsPerRun(1000, func() {
		var p Props
		p.Set("language", "go")
		propsSink = p
	})
	if propsAllocs1 != 1 {
		t.Errorf("Props 1-key build = %v allocs, want exactly 1 (the KV slice)", propsAllocs1)
	}
	if propsAllocs1 >= mapAllocs1 {
		t.Errorf("Props 1-key = %v allocs, map = %v allocs; Props must allocate less",
			propsAllocs1, mapAllocs1)
	}

	mapAllocs2 := testing.AllocsPerRun(1000, func() {
		mapSink = map[string]string{"language": "go", "line": "42"}
	})
	propsAllocs2 := testing.AllocsPerRun(1000, func() {
		p := make(Props, 0, 2)
		p.Set("language", "go")
		p.Set("line", "42")
		propsSink = p
	})
	if propsAllocs2 != 1 {
		t.Errorf("Props 2-key build = %v allocs, want exactly 1", propsAllocs2)
	}
	if propsAllocs2 > mapAllocs2 {
		t.Errorf("Props 2-key = %v allocs, map = %v allocs; Props must not allocate more",
			propsAllocs2, mapAllocs2)
	}
	t.Logf("allocs 1-key: Props %v, map %v; 2-key: Props %v, map %v",
		propsAllocs1, mapAllocs1, propsAllocs2, mapAllocs2)
}

// TestPropsHeapFootprintSmallerThanMap measures resident bytes rather than
// allocation count — the 300B-per-map hmap overhead is what motivates #5954.
func TestPropsHeapFootprintSmallerThanMap(t *testing.T) {
	const n = 50000
	maps := make([]map[string]string, 0, n)
	mapBytes := measureAlloc(func() {
		for i := 0; i < n; i++ {
			maps = append(maps, map[string]string{"language": "go"})
		}
	})
	props := make([]Props, 0, n)
	propBytes := measureAlloc(func() {
		for i := 0; i < n; i++ {
			var p Props
			p.Set("language", "go")
			props = append(props, p)
		}
	})
	if len(maps) != n || len(props) != n {
		t.Fatal("setup")
	}
	if propBytes >= mapBytes {
		t.Errorf("Props used %d B for %d sets, maps used %d B; Props must be smaller",
			propBytes, n, mapBytes)
	}
	t.Logf("per-entry: Props %.0f B, map %.0f B (%.1fx)",
		float64(propBytes)/n, float64(mapBytes)/n, float64(mapBytes)/float64(propBytes))
}
