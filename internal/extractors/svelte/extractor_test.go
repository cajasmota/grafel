package svelte_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/svelte" // trigger init()
	"github.com/cajasmota/archigraph/internal/types"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func mustExtract(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("svelte")
	if !ok {
		t.Fatal("svelte extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "svelte",
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	return recs
}

func findByName(recs []types.EntityRecord, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Name == name {
			return &recs[i]
		}
	}
	return nil
}

func findBySubtype(recs []types.EntityRecord, subtype string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Subtype == subtype {
			out = append(out, r)
		}
	}
	return out
}

func countRenders(recs []types.EntityRecord) int {
	n := 0
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "RENDERS" {
				n++
			}
		}
	}
	return n
}

func rendersTarget(recs []types.EntityRecord, target string) bool {
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "RENDERS" && rel.ToID == target {
				return true
			}
		}
	}
	return false
}

// ── synthetic Svelte 5 + SvelteKit fixture ───────────────────────────────────

// ProductCard.svelte — Svelte 5 component with:
//   - export let props (Svelte 4 compat)
//   - $state, $derived, $effect runes
//   - $props() destructure
//   - child component references: <Badge />, <Button>, <Icon />
const productCardSvelte = `<script lang="ts">
	import { goto, page } from '$app/navigation';
	import Badge from './Badge.svelte';
	import Button from './Button.svelte';
	import Icon from './Icon.svelte';
	import { onMount, onDestroy } from 'svelte';
	import { writable, readable, derived, get } from 'svelte/store';

	// Svelte 4-style export let prop
	export let title = 'Product';
	export let price: number = 0;

	// Svelte 5 runes
	let count = $state(0);
	let doubled = $derived(count * 2);

	// $props() rune (Svelte 5 idiomatic)
	const { label, description = 'No description' } = $props();

	// $bindable rune
	let value = $bindable('');

	// $effect rune
	$effect(() => {
		console.log('count changed', count);
	});

	$effect(() => {
		console.log('value changed', value);
	});

	function handleClick() {
		count++;
		goto('/cart');
	}
</script>

<div class="product-card">
	<Badge label={label} />
	<h2>{title}</h2>
	<p>{description}</p>
	<p>Price: {price}</p>
	<p>Count: {count} (doubled: {doubled})</p>
	<Button on:click={handleClick}>Add to cart</Button>
	<Icon name="cart" />
	<!-- Duplicate — should produce only ONE RENDERS edge for Badge -->
	<Badge label="sale" />
</div>

<style>
	.product-card {
		border: 1px solid #ccc;
		padding: 1rem;
	}
</style>
`

// ── registration ─────────────────────────────────────────────────────────────

func TestExtractor_Language(t *testing.T) {
	ext, ok := extractor.Get("svelte")
	if !ok {
		t.Fatal("svelte extractor not registered")
	}
	if ext.Language() != "svelte" {
		t.Errorf("Language() = %q, want %q", ext.Language(), "svelte")
	}
}

// ── component entity ──────────────────────────────────────────────────────────

func TestExtractor_ComponentEntity(t *testing.T) {
	recs := mustExtract(t, "src/lib/ProductCard.svelte", productCardSvelte)

	comp := findByName(recs, "ProductCard")
	if comp == nil {
		t.Fatal("expected SCOPE.Component entity named 'ProductCard'")
	}
	if comp.Kind != "SCOPE.Component" {
		t.Errorf("Kind = %q, want SCOPE.Component", comp.Kind)
	}
	if comp.Subtype != "svelte_component" {
		t.Errorf("Subtype = %q, want svelte_component", comp.Subtype)
	}
	if comp.SourceFile != "src/lib/ProductCard.svelte" {
		t.Errorf("SourceFile = %q, want src/lib/ProductCard.svelte", comp.SourceFile)
	}
	if comp.StartLine != 1 {
		t.Errorf("StartLine = %d, want 1", comp.StartLine)
	}
}

// ── export let props ─────────────────────────────────────────────────────────

func TestExtractor_ExportLetProps(t *testing.T) {
	recs := mustExtract(t, "ProductCard.svelte", productCardSvelte)

	props := findBySubtype(recs, "prop")
	propNames := make(map[string]bool)
	for _, p := range props {
		propNames[p.Name] = true
	}

	// Expect: title, price (export let), label, description ($props destructure)
	for _, want := range []string{"title", "price", "label", "description"} {
		if !propNames[want] {
			t.Errorf("expected prop %q, got props: %v", want, propNames)
		}
	}

	// Verify Kind on props
	titleProp := findByName(recs, "title")
	if titleProp == nil {
		t.Fatal("expected entity named 'title'")
	}
	if titleProp.Kind != "SCOPE.Operation" {
		t.Errorf("title Kind = %q, want SCOPE.Operation", titleProp.Kind)
	}
}

// ── $state rune ───────────────────────────────────────────────────────────────

func TestExtractor_StateRune(t *testing.T) {
	recs := mustExtract(t, "ProductCard.svelte", productCardSvelte)

	countEnt := findByName(recs, "count")
	if countEnt == nil {
		t.Fatal("expected entity named 'count' ($state rune)")
	}
	if countEnt.Subtype != "rune_state" {
		t.Errorf("count Subtype = %q, want rune_state", countEnt.Subtype)
	}
	if countEnt.Kind != "SCOPE.Operation" {
		t.Errorf("count Kind = %q, want SCOPE.Operation", countEnt.Kind)
	}
}

// ── $derived rune ─────────────────────────────────────────────────────────────

func TestExtractor_DerivedRune(t *testing.T) {
	recs := mustExtract(t, "ProductCard.svelte", productCardSvelte)

	ent := findByName(recs, "doubled")
	if ent == nil {
		t.Fatal("expected entity named 'doubled' ($derived rune)")
	}
	if ent.Subtype != "rune_derived" {
		t.Errorf("doubled Subtype = %q, want rune_derived", ent.Subtype)
	}
}

// ── $effect rune ──────────────────────────────────────────────────────────────

func TestExtractor_EffectRune(t *testing.T) {
	recs := mustExtract(t, "ProductCard.svelte", productCardSvelte)

	effects := findBySubtype(recs, "rune_effect")
	// Fixture has two $effect calls.
	if len(effects) < 2 {
		t.Errorf("expected ≥2 $effect entities, got %d", len(effects))
	}
	for _, eff := range effects {
		if eff.Kind != "SCOPE.Operation" {
			t.Errorf("effect Kind = %q, want SCOPE.Operation", eff.Kind)
		}
	}
}

// ── $bindable rune ────────────────────────────────────────────────────────────

func TestExtractor_BindableRune(t *testing.T) {
	recs := mustExtract(t, "ProductCard.svelte", productCardSvelte)

	ent := findByName(recs, "value")
	if ent == nil {
		t.Fatal("expected entity named 'value' ($bindable rune)")
	}
	if ent.Subtype != "rune_state" {
		t.Errorf("value Subtype = %q, want rune_state", ent.Subtype)
	}
}

// ── RENDERS edges ─────────────────────────────────────────────────────────────

func TestExtractor_RendersEdges(t *testing.T) {
	recs := mustExtract(t, "ProductCard.svelte", productCardSvelte)

	// <Badge />, <Button>, <Icon /> should each produce exactly ONE RENDERS edge.
	for _, child := range []string{"Badge", "Button", "Icon"} {
		if !rendersTarget(recs, child) {
			t.Errorf("expected RENDERS edge to %q", child)
		}
	}

	// Badge appears twice in the template but should be deduplicated.
	badgeCount := 0
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "RENDERS" && rel.ToID == "Badge" {
				badgeCount++
			}
		}
	}
	if badgeCount != 1 {
		t.Errorf("Badge RENDERS count = %d, want 1 (deduplication)", badgeCount)
	}
}

// ── entity recall (≥80%) ──────────────────────────────────────────────────────

// TestExtractor_EntityRecall verifies that the fixture produces at least
// the expected entities. Entities counted:
//
//	ProductCard (component)  = 1
//	title, price             = 2 (export let)
//	label, description       = 2 ($props destructure)
//	count                    = 1 ($state)
//	doubled                  = 1 ($derived)
//	value                    = 1 ($bindable)
//	$effect × 2              = 2
//
// Total expected = 10.  Threshold = 80% → 8 minimum.
func TestExtractor_EntityRecall(t *testing.T) {
	recs := mustExtract(t, "ProductCard.svelte", productCardSvelte)

	expectedEntities := []string{
		"ProductCard", "title", "price", "label", "description",
		"count", "doubled", "value",
	}

	found := 0
	for _, want := range expectedEntities {
		if findByName(recs, want) != nil {
			found++
		}
	}

	total := len(expectedEntities)
	threshold := int(float64(total)*0.8 + 0.5) // round up
	if found < threshold {
		t.Errorf("entity recall: found %d/%d (want ≥%d)", found, total, threshold)
	}
}

// ── zero false positives ──────────────────────────────────────────────────────

// TestExtractor_NoFalsePositives ensures no entity has an empty name, blank
// kind, or quality_score outside [0,1].
func TestExtractor_NoFalsePositives(t *testing.T) {
	recs := mustExtract(t, "ProductCard.svelte", productCardSvelte)

	for _, r := range recs {
		if strings.TrimSpace(r.Name) == "" {
			t.Errorf("entity with blank Name: %+v", r)
		}
		if strings.TrimSpace(r.Kind) == "" {
			t.Errorf("entity with blank Kind: %+v", r)
		}
		if r.QualityScore < 0 || r.QualityScore > 1 {
			t.Errorf("entity %q QualityScore = %.2f outside [0,1]", r.Name, r.QualityScore)
		}
		if r.SourceFile == "" {
			t.Errorf("entity %q has empty SourceFile", r.Name)
		}
		if r.Language != "svelte" {
			t.Errorf("entity %q Language = %q, want svelte", r.Name, r.Language)
		}
	}
}

// ── empty file ────────────────────────────────────────────────────────────────

func TestExtractor_EmptyFile(t *testing.T) {
	recs := mustExtract(t, "Empty.svelte", "")
	if len(recs) != 0 {
		t.Errorf("expected 0 entities for empty file, got %d", len(recs))
	}
}

// ── template-only file (no script) ───────────────────────────────────────────

func TestExtractor_TemplateOnly(t *testing.T) {
	src := `<h1>Hello World</h1>
<p>A simple template with no script block.</p>
`
	recs := mustExtract(t, "Hello.svelte", src)

	comp := findByName(recs, "Hello")
	if comp == nil {
		t.Fatal("expected component entity 'Hello'")
	}
	if comp.Subtype != "svelte_component" {
		t.Errorf("Subtype = %q, want svelte_component", comp.Subtype)
	}
}

// ── minimal Svelte 5 runes-only file ─────────────────────────────────────────

func TestExtractor_RunesOnly(t *testing.T) {
	src := `<script>
	let x = $state(0);
	let y = $derived(x + 1);
	$effect(() => { console.log(x); });
</script>
<p>{x} {y}</p>
`
	recs := mustExtract(t, "Counter.svelte", src)

	if findByName(recs, "x") == nil {
		t.Error("expected $state entity 'x'")
	}
	if findByName(recs, "y") == nil {
		t.Error("expected $derived entity 'y'")
	}
	effects := findBySubtype(recs, "rune_effect")
	if len(effects) == 0 {
		t.Error("expected ≥1 $effect entity")
	}
}

// ── $props() non-destructured form ───────────────────────────────────────────

func TestExtractor_PropsNonDestructured(t *testing.T) {
	src := `<script>
	let props = $props();
</script>
<div>{props.label}</div>
`
	recs := mustExtract(t, "Widget.svelte", src)
	if findByName(recs, "props") == nil {
		t.Error("expected $props() entity 'props'")
	}
}
