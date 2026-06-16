// Package fixture is the embedded grafel selftest fixture (#5224).
//
// It is intentionally tiny and deterministic: the selftest ladder asserts
// against KNOWN entities and a KNOWN CALLS edge produced from this file, so
// any change here must be matched by the assertions in cmd/grafel/selftest.go.
//
// Known entities the selftest depends on:
//   - function Greet          (cold-index assertion)
//   - function RunGreeting    (calls Greet → CALLS edge assertion)
//   - function AddedByReindex (appended/removed at the incremental layer)
package fixture

import "fmt"

// Greet returns a greeting for name. Referenced by RunGreeting so the
// indexer emits a CALLS edge between the two — the cold-index edge assertion.
func Greet(name string) string {
	return fmt.Sprintf("hello, %s", name)
}

// RunGreeting calls Greet and prints the result. The selftest asserts that a
// CALLS edge RunGreeting -> Greet exists after a cold index.
func RunGreeting() {
	msg := Greet("grafel")
	fmt.Println(msg)
}
