// config_consumer.go — supplemental pass that emits DEPENDS_ON_CONFIG edges
// from Python operations / classes / modules that consume settings or
// environment variables to the Config entity that owns them.
//
// Issue #1982 — #1919 emitted SCOPE.Config entities for settings.py and other
// canonical config files, but the consumer side of the relationship was
// never wired: views, serializers, tasks, and middleware that read
// `settings.X` or `os.environ.get("X")` were topologically isolated from
// the Config entity that owned that key. Without consumer edges the Config
// entities were dead islands in the graph and #1919 only shipped half the
// feature.
//
// This pass runs AFTER all primary entity emission so it can observe:
//
//   1. The file's own imports (already attached to the file entity by
//      extractImports / attachImportRelationships).
//   2. Every Operation/Class entity that walkNode emitted in the file.
//
// Detected consumer shapes:
//
//   - `from django.conf import settings` + `settings.X`
//   - `from django.conf import settings as my_settings` + `my_settings.X`
//   - `import django.conf` + `django.conf.settings.X` (rare but legal)
//   - `os.environ.get("X")`, `os.environ["X"]`, `os.getenv("X")`
//
// For each consumer call, we emit a single DEPENDS_ON_CONFIG edge from the
// enclosing function/method/class entity (or, when at module scope, the
// file entity) to a structural-ref ToID that points at the settings.py
// Config entity or, for env consumers, the project `.env` Config entity.
//
// The ToID format mirrors the existing config_module entity emitted by
// emitConfigModuleEntity:
//
//	scope:config:config_module:python:<settings.py path>:settings
//
// Cross-file resolution: we cannot know the canonical settings.py path
// from inside a single-file extractor pass. We emit a STRUCTURAL ToID
// that the resolver binds by Name ("settings" or ".env") + Kind
// (SCOPE.Config). The resolver's existing structural-ref → byName path
// (mirrors emitReferences in references.go) covers this without new
// dispatcher work.
//
// Dedup: at most one DEPENDS_ON_CONFIG edge per (from_id, settings_or_env)
// pair per file. Properties on the edge record the specific keys
// referenced (joined with ',') so downstream consumers (e.g. #1942 auth
// resolver, #1944 Event Flows) can filter by key.

package python

import (
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitConfigConsumerEdges scans the file body for settings / env consumption
// patterns and appends DEPENDS_ON_CONFIG edges to the enclosing entity.
//
// Mutates *entities in place. Safe to call with nil or empty input.
func emitConfigConsumerEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	// Phase 1 — read the file entity's import bindings to find any alias
	// that resolves to `django.conf.settings` (or an equivalent project
	// config module classified by #1919). We track:
	//
	//   settingsAliases : set of local identifiers that BIND to the Django
	//                     settings object (most commonly "settings").
	//
	// We also flag whether `os` is imported (or the explicit names
	// `environ`, `getenv` are imported from `os`) so the env scan can be
	// gated on actual presence.
	settingsAliases := map[string]struct{}{}
	osImported := false
	osEnvironLocal := ""
	osGetenvLocal := ""

	fileEnt := &(*entities)[0]
	for _, r := range fileEnt.Relationships {
		if r.Kind != "IMPORTS" {
			continue
		}
		src := r.Properties["source_module"]
		imp := r.Properties["imported_name"]
		local := r.Properties["local_name"]

		// `from django.conf import settings [as X]`
		if src == "django.conf" && imp == "settings" {
			if local != "" {
				settingsAliases[local] = struct{}{}
			} else {
				settingsAliases["settings"] = struct{}{}
			}
		}
		// `import django.conf` — settings is reached via the dotted path,
		// which the receiver scan can't trivially follow without name
		// resolution; we emit the edge unconditionally on a `django.conf`
		// occurrence below.
		if src == "django.conf" && imp == "" {
			settingsAliases["django.conf.settings"] = struct{}{}
		}

		// os module: a bare `import os` makes os.environ + os.getenv
		// available. Explicit `from os import environ/getenv [as Y]`
		// rebinds them under the chosen local name.
		//
		// `import os` is emitted by extractImports as
		//   source_module="os", imported_name="os" (module-self pair).
		// `from os import environ` is emitted as
		//   source_module="os", imported_name="environ".
		if src == "os" && (imp == "" || imp == "os") {
			osImported = true
		}
		if src == "os" && imp == "environ" {
			osEnvironLocal = local
			if osEnvironLocal == "" {
				osEnvironLocal = "environ"
			}
		}
		if src == "os" && imp == "getenv" {
			osGetenvLocal = local
			if osGetenvLocal == "" {
				osGetenvLocal = "getenv"
			}
		}
	}

	// Phase 2 — walk every function/method body (and module-scope) and
	// detect consumer attribute / call shapes. We track the enclosing
	// entity Name so the edge attaches to the right entity. Module-scope
	// references attach to the file entity (entities[0]).
	type bucket struct {
		settingsKeys map[string]struct{}
		envKeys      map[string]struct{}
	}
	per := map[string]*bucket{}
	getBucket := func(emittedName string) *bucket {
		b, ok := per[emittedName]
		if !ok {
			b = &bucket{
				settingsKeys: map[string]struct{}{},
				envKeys:      map[string]struct{}{},
			}
			per[emittedName] = b
		}
		return b
	}

	var stack []string // emitted-name stack; top = current entity
	currentName := func() string {
		if len(stack) == 0 {
			return "" // module scope → file entity
		}
		return stack[len(stack)-1]
	}

	var walk func(n ts.Node, parentClass string)
	walk = func(n ts.Node, parentClass string) {
		if n == nil {
			return
		}
		nt := n.Type()
		switch nt {
		case "class_definition":
			nameNode := n.ChildByFieldName("name")
			cls := ""
			if nameNode != nil {
				cls = nodeText(nameNode, file.Content)
			}
			childCls := cls
			if parentClass != "" && cls != "" {
				childCls = parentClass + "." + cls
			}
			// Push the class itself as the bucket key for class-body
			// (non-method) statements that consume settings (e.g. class-
			// level attribute defaults read from settings.X).
			stack = append(stack, childCls)
			body := n.ChildByFieldName("body")
			if body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), childCls)
				}
			}
			stack = stack[:len(stack)-1]
			return
		case "function_definition":
			nameNode := n.ChildByFieldName("name")
			leaf := ""
			if nameNode != nil {
				leaf = nodeText(nameNode, file.Content)
			}
			emitted := leaf
			if parentClass != "" && leaf != "" {
				emitted = parentClass + "." + leaf
			}
			stack = append(stack, emitted)
			body := n.ChildByFieldName("body")
			if body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), parentClass)
				}
			}
			stack = stack[:len(stack)-1]
			return
		case "decorated_definition":
			inner := n.ChildByFieldName("definition")
			if inner != nil {
				walk(inner, parentClass)
			}
			return
		}

		// Detect settings.X attribute access.
		if nt == "attribute" {
			obj := n.ChildByFieldName("object")
			attr := n.ChildByFieldName("attribute")
			if obj != nil && attr != nil {
				recv := strings.TrimSpace(nodeText(obj, file.Content))
				attrName := nodeText(attr, file.Content)
				if _, ok := settingsAliases[recv]; ok && attrName != "" {
					getBucket(currentName()).settingsKeys[attrName] = struct{}{}
				}
			}
		}

		// Detect call shapes for env access.
		if nt == "call" {
			fn := n.ChildByFieldName("function")
			args := n.ChildByFieldName("arguments")
			if fn != nil && args != nil {
				if key := envKeyFromCall(fn, args, file.Content, osImported, osEnvironLocal, osGetenvLocal); key != "" {
					getBucket(currentName()).envKeys[key] = struct{}{}
				}
			}
		}

		// Detect subscript shapes: os.environ["X"] / environ["X"].
		if nt == "subscript" {
			val := n.ChildByFieldName("value")
			subs := n.ChildByFieldName("subscript")
			if val != nil && subs != nil {
				recv := strings.TrimSpace(nodeText(val, file.Content))
				isEnviron := false
				if osImported && recv == "os.environ" {
					isEnviron = true
				}
				if osEnvironLocal != "" && recv == osEnvironLocal {
					isEnviron = true
				}
				if isEnviron && subs.Type() == "string" {
					key := stripQuotes(nodeText(subs, file.Content))
					if key != "" {
						getBucket(currentName()).envKeys[key] = struct{}{}
					}
				}
			}
		}

		// Recurse into children for every other node type.
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), parentClass)
		}
	}
	walk(root, "")

	// Phase 3 — emit one edge per (entity, target) pair.
	for emittedName, b := range per {
		if len(b.settingsKeys) > 0 {
			attachConfigEdge(file, entities, emittedName, "settings", keysSorted(b.settingsKeys))
		}
		if len(b.envKeys) > 0 {
			attachConfigEdge(file, entities, emittedName, ".env", keysSorted(b.envKeys))
		}
	}
}

// attachConfigEdge appends a DEPENDS_ON_CONFIG relationship to the entity
// identified by emittedName (file entity when empty). The ToID is a
// Name-keyed structural reference resolved at link time.
func attachConfigEdge(file extractor.FileInput, entities *[]types.EntityRecord, emittedName, configName string, keys []string) {
	var hostIdx int
	if emittedName == "" {
		hostIdx = 0 // file entity
	} else {
		hostIdx = -1
		for i := range *entities {
			e := &(*entities)[i]
			if e.SourceFile == file.Path && e.Name == emittedName {
				hostIdx = i
				break
			}
		}
		if hostIdx < 0 {
			return
		}
	}
	toID := buildConfigTargetID(configName)
	props := map[string]string{
		"config_name": configName,
	}
	if len(keys) > 0 {
		props["keys"] = strings.Join(keys, ",")
	}
	(*entities)[hostIdx].Relationships = append((*entities)[hostIdx].Relationships,
		types.RelationshipRecord{
			ToID:       toID,
			Kind:       string(types.RelationshipKindDependsOnConfig),
			Properties: props,
		})
}

// buildConfigTargetID returns a structural-ref ToID that the resolver binds
// by Name to a SCOPE.Config entity. Two well-known shapes:
//
//	"settings"  → Django settings module (settings.py)
//	".env"      → environment-file Config entity
//
// The resolver's by-Name index for SCOPE.Config picks the canonical
// settings.py entity emitted by emitConfigModuleEntity (config_module
// subtype, Name="settings"). When a project has no canonical settings.py
// the edge remains unresolved — same disposition as other cross-file
// structural refs (e.g. CALLS to a missing function).
func buildConfigTargetID(configName string) string {
	// Use "ref:<lang>:<name>" — same shape family as
	// buildPyReferenceTargetID but with scope segment "config" so the
	// resolver's structural dispatch routes it to the SCOPE.Config family.
	return "scope:config:ref:python:" + filepath.ToSlash(configName) + ":" + configName
}

// envKeyFromCall returns the literal env var key when fn(args) matches one
// of the supported env-read shapes, or empty when it doesn't.
//
// Supported shapes:
//
//	os.environ.get("X" [, default])
//	os.getenv("X" [, default])
//	environ.get("X" [, default])          (when `environ` was imported from os)
//	getenv("X" [, default])               (when `getenv` was imported from os)
func envKeyFromCall(fn, args ts.Node, src []byte, osImported bool, environLocal, getenvLocal string) string {
	calleeText := strings.TrimSpace(nodeText(fn, src))
	matched := false
	switch {
	case osImported && (calleeText == "os.environ.get" || calleeText == "os.getenv"):
		matched = true
	case environLocal != "" && calleeText == environLocal+".get":
		matched = true
	case getenvLocal != "" && calleeText == getenvLocal:
		matched = true
	}
	if !matched {
		return ""
	}
	if args.NamedChildCount() == 0 {
		return ""
	}
	first := args.NamedChild(0)
	if first == nil || first.Type() != "string" {
		return ""
	}
	return stripQuotes(nodeText(first, src))
}

// keysSorted returns the keys of m as a stable, lexically-sorted slice.
func keysSorted(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// simple insertion sort — n is small (per-file)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
