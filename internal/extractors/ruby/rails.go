// Rails framework detection for Ruby class/module entities.
//
// This file is intentionally additive — it does not change the set of
// entities emitted by the Ruby extractor. It only enriches already-emitted
// SCOPE.Component records with Rails-specific properties and tags when
// the class/module clearly belongs to the Rails framework.
//
// Detection rules (applied in order, short-circuit on first match):
//
//  1. Controller — class inherits from ApplicationController or any
//     ActionController::* ancestor, OR the file path lives under
//     app/controllers/.
//  2. Model      — class inherits from ApplicationRecord or
//     ActiveRecord::Base, OR the file path lives under app/models/.
//  3. Migration  — file path lives under db/migrate/.
//  4. Route      — file path ends with config/routes.rb.
//
// Rules 1–3 additionally accept path-only detection so that Rails
// entities without an explicit superclass (e.g. concerns that mix in
// ActiveSupport::Concern) still get labelled, matching the
// acceptance criteria.
package ruby

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// railsFramework is the framework identifier emitted for Rails entities.
const railsFramework = "rails"

// railsControllerSuperclasses lists the superclass names (as they appear
// in the Ruby source) that unambiguously identify a Rails controller.
var railsControllerSuperclasses = map[string]struct{}{
	"ApplicationController":     {},
	"ActionController::Base":    {},
	"ActionController::API":     {},
	"ActionController::Metal":   {},
	"ActionController::Metal::": {}, // guard against children referring with trailing ::
}

// railsModelSuperclasses lists the superclass names that identify a
// Rails model backed by ActiveRecord.
var railsModelSuperclasses = map[string]struct{}{
	"ApplicationRecord":   {},
	"ActiveRecord::Base":  {},
	"ActiveRecord::Model": {},
}

// classSuperclass extracts the superclass identifier text (if present)
// from a Ruby `class` CST node. Returns "" when the class has no
// explicit superclass.
//
// Tree-sitter exposes the superclass as the "superclass" field containing
// a single `superclass` node whose text is the constant reference, e.g.
//
//	class Foo < Bar
//
// will yield a field child `superclass` whose subtree serialises back
// to "< Bar". We strip the leading "< " to return just "Bar".
func classSuperclass(node ts.Node, src []byte) string {
	if node == nil {
		return ""
	}
	child := node.ChildByFieldName("superclass")
	if child == nil {
		return ""
	}
	raw := strings.TrimSpace(string(src[child.StartByte():child.EndByte()]))
	raw = strings.TrimPrefix(raw, "<")
	return strings.TrimSpace(raw)
}

// tagRails enriches a SCOPE.Component entity with Rails framework
// properties when the class/module is identifiable as a Rails entity.
// Called by buildComponent after the record is otherwise populated.
//
// The function is purely additive: if no Rails signal is detected, the
// record is left untouched.
func tagRails(rec *types.EntityRecord, node ts.Node, src []byte, path string) {
	if rec == nil {
		return
	}

	// Normalise the path so "\" vs "/" does not matter on any host.
	normPath := strings.ReplaceAll(path, "\\", "/")

	// Rule 4: routes file. We match the Ruby extractor's class/module
	// nodes inside config/routes.rb and mark them as the routes DSL.
	// This is intentionally loose: any top-level block in routes.rb is
	// part of the Rails routing definition.
	if strings.HasSuffix(normPath, "config/routes.rb") {
		applyRailsProps(rec, "route", "")
		return
	}

	// Rule 3: migration. db/migrate/ is the canonical Rails migration
	// directory; entries inside it are migration classes.
	if strings.Contains(normPath, "db/migrate/") {
		applyRailsProps(rec, "migration", "")
		return
	}

	superclass := ""
	if rec.Subtype == "class" {
		superclass = classSuperclass(node, src)
	}

	// Rule 1: controller.
	if _, ok := railsControllerSuperclasses[superclass]; ok {
		applyRailsProps(rec, "controller", superclass)
		return
	}
	if strings.Contains(normPath, "app/controllers/") {
		applyRailsProps(rec, "controller", superclass)
		return
	}

	// Rule 2: model.
	if _, ok := railsModelSuperclasses[superclass]; ok {
		applyRailsProps(rec, "model", superclass)
		return
	}
	if strings.Contains(normPath, "app/models/") {
		applyRailsProps(rec, "model", superclass)
		return
	}
}

// applyRailsProps sets the framework, kind, service_kind and (optionally)
// orm properties on rec, and appends a "framework:rails" tag. Callers
// pass the Rails kind as framework-extracted ("controller", "model",
// "migration", "route") — this function maps it to downstream labels
// that framework-test.sh expects (service_kind="rails_service",
// orm="activerecord").
func applyRailsProps(rec *types.EntityRecord, kind, superclass string) {
	if rec.Properties == nil {
		rec.Properties = make(map[string]string)
	}
	rec.Properties["framework"] = railsFramework
	rec.Properties["kind"] = kind
	rec.Properties["service_kind"] = "rails_service"
	if superclass != "" {
		rec.Properties["superclass"] = superclass
	}
	if kind == "model" {
		rec.Properties["orm"] = "activerecord"
	}
	rec.Tags = appendUniqueTag(rec.Tags, "framework:"+railsFramework)
	rec.Tags = appendUniqueTag(rec.Tags, "rails:"+kind)
}

// appendUniqueTag appends v to slice only if not already present.
// Duplicated here (rather than imported from references) to keep the
// Ruby extractor self-contained — it has no dependency on references.
func appendUniqueTag(slice []string, v string) []string {
	for _, existing := range slice {
		if existing == v {
			return slice
		}
	}
	return append(slice, v)
}
