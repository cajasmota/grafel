// wire_normalize.go — JSON wire-contract normalization for dashboard responses.
//
// Problem (#4516): the dashboard report structs declare many []T fields
// WITHOUT `,omitempty`. In Go a nil slice marshals to JSON `null`, but the
// webui-v2 TypeScript report types declare these fields as non-nullable arrays
// and call `.length` / `.map` / `.filter` on them directly. An empty result
// therefore arrives as `null` and the frontend crashes on `null.length`.
//
// Rather than sprinkle `,omitempty` (which would DROP the field entirely —
// the frontend wants `[]`, not a missing key) or hand-initialize every slice
// field in every handler, we normalize at the marshal boundary: a single
// reflection pass walks the payload and replaces every nil slice with a
// non-nil empty slice of the same element type. One fix covers all current and
// future fields, and the on-wire shape becomes `[]` instead of `null`.
//
// Scope of the walk:
//   - nil slices            -> empty slice ([]T{})  (the fix)
//   - populated slices      -> recursed into, elements normalized
//   - structs / *structs    -> recursed into addressable copies
//   - arrays                -> recursed into elements
//   - maps                  -> recursed into VALUES (keys untouched). Map nil
//     itself is left as `null`: a nil map is semantically "absent" and the
//     frontend does not call array methods on object-typed fields. Only the
//     values are normalized so nested slices inside map values still become [].
//   - pointers              -> followed when non-nil; nil pointers untouched
//     (null is semantically meaningful for an optional object).
//
// The function operates on a deep, addressable copy so the caller's value is
// never mutated — important because handlers often share/cache report structs.

package dashboard

import "reflect"

// normalizeNilSlices returns a copy of v in which every nil slice reachable
// through exported, settable fields has been replaced by an empty (non-nil)
// slice of the same element type. The input is not mutated.
//
// Returns the normalized value as an `any` suitable for json.Marshal /
// Encoder.Encode. If v is nil it is returned unchanged.
func normalizeNilSlices(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	out := normalizeValue(rv)
	if !out.IsValid() {
		return v
	}
	return out.Interface()
}

// normalizeValue returns a normalized copy of rv. It allocates a fresh
// addressable value, copies rv into it, walks it, and returns it.
func normalizeValue(rv reflect.Value) reflect.Value {
	if !rv.IsValid() {
		return rv
	}
	// Allocate a settable copy we can mutate freely.
	cp := reflect.New(rv.Type()).Elem()
	cp.Set(rv)
	walk(cp)
	return cp
}

// walk mutates v in place, replacing nil slices with empty slices and
// recursing into nested structs, slices, arrays, maps and pointers.
// v must be addressable/settable.
func walk(v reflect.Value) {
	switch v.Kind() {
	case reflect.Slice:
		if v.IsNil() {
			// nil slice -> empty (non-nil) slice of the same element type.
			v.Set(reflect.MakeSlice(v.Type(), 0, 0))
			return
		}
		for i := 0; i < v.Len(); i++ {
			walk(v.Index(i))
		}

	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walk(v.Index(i))
		}

	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			// Skip unexported fields — not settable, not marshaled.
			if t.Field(i).PkgPath != "" {
				continue
			}
			walk(v.Field(i))
		}

	case reflect.Ptr:
		if v.IsNil() {
			return // nil pointer stays null — semantically meaningful.
		}
		walk(v.Elem())

	case reflect.Map:
		if v.IsNil() {
			return // nil map stays null; object-typed, not iterated as array.
		}
		// Normalize map VALUES (keys are immutable map index values). We build
		// a normalized copy of each value and write it back.
		for _, k := range v.MapKeys() {
			ev := v.MapIndex(k)
			nv := reflect.New(ev.Type()).Elem()
			nv.Set(ev)
			walk(nv)
			v.SetMapIndex(k, nv)
		}

	case reflect.Interface:
		if v.IsNil() {
			return
		}
		// The dynamic value inside an interface is not addressable, so make a
		// settable copy, walk it, and store it back into the interface slot.
		elem := v.Elem()
		nv := reflect.New(elem.Type()).Elem()
		nv.Set(elem)
		walk(nv)
		v.Set(nv)
	}
}
