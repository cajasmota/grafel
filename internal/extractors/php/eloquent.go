// Eloquent / Laravel framework detection for PHP class entities.
//
// This file is intentionally additive — it does not change the set of
// entities emitted by the PHP extractor. It only enriches already-emitted
// SCOPE.Component records with Laravel/Eloquent-specific properties and
// tags when the class clearly belongs to the framework.
//
// Detection rules (applied in order, short-circuit on first match):
//
//  1. Model      — class extends Illuminate\Database\Eloquent\Model
//     (or the shorthand `extends Model` when the namespace import is
//     present), OR the file path lives under app/Models/.
//  2. Migration  — file path lives under database/migrations/.
//  3. Controller — class extends Controller, App\Http\Controllers\Controller,
//     or Illuminate\Routing\Controller, OR the file path lives under
//     app/Http/Controllers/.
//
// Rule 1 additionally accepts path-only detection so that Eloquent
// entities whose superclass is a project-specific base class (e.g.
// `extends BaseModel`) still get labelled when they live in
// app/Models/, matching the acceptance criteria.
package php

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// laravelFramework is the framework identifier for Eloquent / Laravel.
const laravelFramework = "laravel"

// eloquentModelBaseClasses lists the superclass names that identify a
// Laravel Eloquent model. Both fully-qualified and shorthand forms are
// accepted — Laravel projects commonly `use Illuminate\Database\Eloquent\Model`
// and then declare `class X extends Model`.
var eloquentModelBaseClasses = map[string]struct{}{
	"Model":                                 {},
	"Eloquent\\Model":                       {},
	"Illuminate\\Database\\Eloquent\\Model": {},
	// Authenticatable is the standard User-model base in Laravel, which
	// inherits from Model. Treating it as a model keeps the User entity
	// correctly framework-tagged.
	"Authenticatable":                                  {},
	"Illuminate\\Foundation\\Auth\\User":               {},
	"Illuminate\\Database\\Eloquent\\Relations\\Pivot": {},
}

// laravelControllerBaseClasses lists the superclass names that identify
// a Laravel controller.
var laravelControllerBaseClasses = map[string]struct{}{
	"Controller":                         {},
	"App\\Http\\Controllers\\Controller": {},
	"Illuminate\\Routing\\Controller":    {},
}

// classExtends extracts the superclass name from a PHP class_declaration
// CST node. tree-sitter-php exposes the inheritance via a
// `base_clause` child containing a `name` token (or `qualified_name`
// for namespaced references). Returns "" when the class has no parent.
//
// Uses a small CST walk rather than ChildByFieldName because the PHP
// grammar does not expose the base class as a named field — it sits as
// a `base_clause` child whose first meaningful token is the parent
// class name.
func classExtends(node ts.Node, src []byte) string {
	if node == nil {
		return ""
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil || child.Type() != "base_clause" {
			continue
		}
		// The base_clause has the form `extends Foo` or
		// `extends \Fully\Qualified\Name`. We pick the first name-like
		// child and strip any leading root-namespace backslash so the
		// resulting identifier compares equal to its `use`-imported
		// alias (tree-sitter-php retains the leading "\" in the source
		// text of `qualified_name`).
		for j := 0; j < int(child.ChildCount()); j++ {
			sub := child.Child(j)
			if sub == nil {
				continue
			}
			switch sub.Type() {
			case "name", "qualified_name":
				raw := strings.TrimSpace(string(src[sub.StartByte():sub.EndByte()]))
				return strings.TrimPrefix(raw, "\\")
			}
		}
		// Fallback: strip leading "extends " and return the remainder.
		raw := strings.TrimSpace(string(src[child.StartByte():child.EndByte()]))
		raw = strings.TrimPrefix(raw, "extends")
		raw = strings.TrimSpace(raw)
		return strings.TrimPrefix(raw, "\\")
	}
	return ""
}

// tagEloquent enriches a SCOPE.Component entity with Laravel framework
// properties when the class is identifiable as a Laravel entity.
// Called by buildComponent after the record is otherwise populated.
//
// The function is purely additive: if no Laravel signal is detected,
// the record is left untouched.
func tagEloquent(rec *types.EntityRecord, node ts.Node, src []byte, path string) {
	if rec == nil {
		return
	}
	// Interfaces never carry framework kind labels in this scheme — an
	// Eloquent interface (if any) is infrastructure, not a model.
	if rec.Subtype != "class" {
		return
	}

	normPath := strings.ReplaceAll(path, "\\", "/")

	// Rule 2: migration. Laravel migrations are anonymous classes or
	// subclasses of Migration placed under database/migrations/.
	if strings.Contains(normPath, "database/migrations/") {
		applyEloquentProps(rec, "migration", "")
		return
	}

	superclass := classExtends(node, src)

	// Rule 1: Eloquent model.
	if _, ok := eloquentModelBaseClasses[superclass]; ok {
		applyEloquentProps(rec, "model", superclass)
		return
	}
	if strings.Contains(normPath, "app/Models/") {
		applyEloquentProps(rec, "model", superclass)
		return
	}

	// Rule 3: controller.
	if _, ok := laravelControllerBaseClasses[superclass]; ok {
		applyEloquentProps(rec, "controller", superclass)
		return
	}
	if strings.Contains(normPath, "app/Http/Controllers/") {
		applyEloquentProps(rec, "controller", superclass)
		return
	}
}

// applyEloquentProps sets the framework, kind, and (optionally) orm and
// service_kind properties on rec, and appends the Laravel framework
// tag. Callers pass the Laravel kind as framework-extracted ("model",
// "migration", "controller"); this function maps it to the downstream
// labels expected by framework-test.sh (orm="eloquent" for models,
// service_kind="laravel_service" for controllers).
func applyEloquentProps(rec *types.EntityRecord, kind, superclass string) {
	if rec.Properties == nil {
		rec.Properties = make(map[string]string)
	}
	rec.Properties["framework"] = laravelFramework
	rec.Properties["kind"] = kind
	if superclass != "" {
		rec.Properties["superclass"] = superclass
	}
	switch kind {
	case "model":
		rec.Properties["orm"] = "eloquent"
	case "controller":
		rec.Properties["service_kind"] = "laravel_service"
	case "migration":
		rec.Properties["service_kind"] = "laravel_migration"
	}
	rec.Tags = appendUniquePHPTag(rec.Tags, "framework:"+laravelFramework)
	rec.Tags = appendUniquePHPTag(rec.Tags, "laravel:"+kind)
}

// appendUniquePHPTag appends v to slice only if not already present.
// Duplicated here (rather than imported from references) to keep the
// PHP extractor self-contained.
func appendUniquePHPTag(slice []string, v string) []string {
	for _, existing := range slice {
		if existing == v {
			return slice
		}
	}
	return append(slice, v)
}
