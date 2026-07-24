// ORM model lifecycle-hook / signal → handler TRIGGERS edges (#3628 area).
//
// This pass links a model persistence lifecycle event (before/after
// create/update/delete/save/validate) to the handler function it fires,
// answering "what runs after a User is saved?". It is a genuinely untouched
// dimension: before this pass the only model-event linkage in the graph was
// Django's HANDLES_SIGNAL (handler → sender model, no event node) and the
// scheduler-only TRIGGERS edge. Neither makes the *event* a first-class,
// queryable node.
//
// The event node is a SCOPE.ModelEvent entity named "<Model>.<event>" (e.g.
// "User.post_save", "Order.afterInsert"). The handler join is a TRIGGERS edge:
//
//	SCOPE.ModelEvent:<Model>.<event>  --TRIGGERS-->  <handler fn/method>
//
// Frameworks covered (all single-file: the model/entity and the handler are
// co-located, so a per-file pass resolves every endpoint without cross-file
// reference plumbing):
//
//	Django signals (python)      @receiver(post_save, sender=User) def f(...)
//	SQLAlchemy events (python)   @event.listens_for(User, 'after_insert') def f
//	ActiveRecord (ruby)          after_create :send_welcome  /  before_save :norm
//	TypeORM (jsts)               @AfterInsert() method  /  @EventSubscriber afterInsert
//	Sequelize (jsts)             User.afterCreate((user)=>{...})  /  hooks:{afterCreate}
//	Mongoose (jsts)              schema.post('save', fn)  /  schema.pre('save', fn)
//	MikroORM (jsts)              @BeforeCreate() method  /  @OnLoad() method
//
// Honest-partial discipline: a dynamic / missing model (e.g. @receiver with no
// sender, an all-models signal), a dynamic event name, or an anonymous /
// dynamic handler is SKIPPED — we never fabricate a model or a handler.
//
// All emissions are append-only — existing entities and edges are never
// modified or removed, so this pass cannot regress surrounding passes.
//
// Refs #3628.
package engine

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// modelEventKind is the entity kind for a single model lifecycle event node.
const modelEventKind = "SCOPE.ModelEvent"

// ---------------------------------------------------------------------------
// Python — Django signals + SQLAlchemy events
// ---------------------------------------------------------------------------

// djangoBuiltinSignals is the set of built-in model persistence signals that
// carry a model sender. Custom signals (Signal()) are intentionally excluded:
// they are pub/sub topics, handled by ApplyDjangoSignalPubSub, and have no
// single model identity.
var djangoBuiltinSignals = map[string]bool{
	"pre_init": true, "post_init": true,
	"pre_save": true, "post_save": true,
	"pre_delete": true, "post_delete": true,
	"m2m_changed":  true,
	"pre_migrate":  true,
	"post_migrate": true,
}

// ormReceiverRe matches a @receiver(<signal>, sender=<Model>) decorator
// (allowing stacked decorators between it and the def) directly above a def.
// Group 1 = signal expression (possibly dotted, e.g. signals.post_save).
// Group 2 = sender model name (may be empty when no sender= kwarg is present).
// Group 3 = handler function name.
var ormReceiverRe = regexp.MustCompile(
	`(?m)@receiver\s*\(\s*([\w.]+)(?:[^)]*?\bsender\s*=\s*([\w.]+))?[^)]*\)(?:\s*\n\s*@[^\n]*)*\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`,
)

// sqlAlchemyListensRe matches @event.listens_for(<Target>, '<event>') above a
// def. Group 1 = target (model class / mapper); Group 2 = event name string;
// Group 3 = handler function name.
var sqlAlchemyListensRe = regexp.MustCompile(
	`(?m)@event\.listens_for\s*\(\s*([\w.]+)\s*,\s*['"]([\w]+)['"][^)]*\)(?:\s*\n\s*@[^\n]*)*\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`,
)

// ---------------------------------------------------------------------------
// Ruby — ActiveRecord callbacks
// ---------------------------------------------------------------------------

// arCallbackNames is the set of ActiveRecord lifecycle callback macros. Each
// declares one or more handler method symbols.
var arCallbackNames = map[string]bool{
	"after_initialize": true, "after_find": true,
	"before_validation": true, "after_validation": true,
	"before_save": true, "after_save": true, "around_save": true,
	"before_create": true, "after_create": true, "around_create": true,
	"before_update": true, "after_update": true, "around_update": true,
	"before_destroy": true, "after_destroy": true, "around_destroy": true,
	"after_commit": true, "after_rollback": true,
	"after_create_commit": true, "after_update_commit": true,
	"after_destroy_commit": true, "after_save_commit": true,
}

// rubyClassRe matches `class User < ApplicationRecord` / `class User <
// ActiveRecord::Base`. Group 1 = model class name. We only treat callbacks
// inside an ActiveRecord-descended class as model lifecycle hooks.
var rubyClassRe = regexp.MustCompile(
	`(?m)^\s*class\s+(\w+)\s*<\s*[\w:]*(?:ApplicationRecord|ActiveRecord::Base)`,
)

// arCallbackRe matches `after_create :send_welcome` /
// `before_save :normalize_name, :downcase_email`. Group 1 = callback macro;
// Group 2 = the comma-separated symbol list.
var arCallbackRe = regexp.MustCompile(
	`(?m)^[ \t]*(\w+)[ \t]+((?::\w+[ \t]*,?[ \t]*)+)`,
)

// arSymbolRe extracts each `:handler` symbol from a callback's argument list.
var arSymbolRe = regexp.MustCompile(`:(\w+)`)

// ---------------------------------------------------------------------------
// JS / TS — TypeORM, Sequelize, Mongoose
// ---------------------------------------------------------------------------

// typeormDecoratorEvents maps a TypeORM lifecycle decorator to its event name.
var typeormDecoratorEvents = map[string]string{
	"BeforeInsert": "beforeInsert", "AfterInsert": "afterInsert",
	"BeforeUpdate": "beforeUpdate", "AfterUpdate": "afterUpdate",
	"BeforeRemove": "beforeRemove", "AfterRemove": "afterRemove",
	"BeforeSoftRemove": "beforeSoftRemove", "AfterSoftRemove": "afterSoftRemove",
	"BeforeRecover": "beforeRecover", "AfterRecover": "afterRecover",
	"AfterLoad": "afterLoad",
}

// tsEntityClassRe matches an @Entity(...) decorated class declaration. Group 1
// = entity class name. Used to scope @BeforeInsert()/@AfterInsert() methods to
// their owning entity.
var tsEntityClassRe = regexp.MustCompile(
	`(?m)@Entity\s*\([^)]*\)(?:\s*\n\s*@[^\n]*)*\s*\n\s*export\s+class\s+(\w+)|@Entity\s*\([^)]*\)\s*\n\s*class\s+(\w+)`,
)

// tsLifecycleMethodRe matches a TypeORM lifecycle decorator directly above a
// method. Group 1 = decorator name (e.g. AfterInsert); Group 2 = method name.
var tsLifecycleMethodRe = regexp.MustCompile(
	`(?m)@(BeforeInsert|AfterInsert|BeforeUpdate|AfterUpdate|BeforeRemove|AfterRemove|BeforeSoftRemove|AfterSoftRemove|BeforeRecover|AfterRecover|AfterLoad)\s*\(\s*\)(?:\s*\n\s*@[^\n]*)*\s*\n\s*(?:async\s+)?(\w+)\s*\(`,
)

// mikroORMDecoratorEvents maps a MikroORM lifecycle hook decorator to its
// event name. These are the MikroORM-EXCLUSIVE decorators: @BeforeUpdate /
// @AfterUpdate / @AfterLoad share their decorator name with TypeORM and are
// already handled (with the same event name) by tsLifecycleMethodRe, so
// including them here would double-emit. Restricting this set to the
// MikroORM-only decorators keeps emissions disjoint from the TypeORM branch.
// Ref: MikroORM lifecycle hooks (@BeforeCreate/@AfterCreate/@BeforeUpsert/
// @AfterUpsert/@BeforeDelete/@AfterDelete/@OnInit/@OnLoad).
var mikroORMDecoratorEvents = map[string]string{
	"BeforeCreate": "beforeCreate", "AfterCreate": "afterCreate",
	"BeforeUpsert": "beforeUpsert", "AfterUpsert": "afterUpsert",
	"BeforeDelete": "beforeDelete", "AfterDelete": "afterDelete",
	"OnInit": "onInit", "OnLoad": "onLoad",
}

// mikroLifecycleMethodRe matches a MikroORM-exclusive lifecycle decorator
// directly above a method. Group 1 = decorator name; Group 2 = method name.
// Scoped, like the TypeORM matcher, to @Entity class bodies by the caller.
var mikroLifecycleMethodRe = regexp.MustCompile(
	`(?m)@(BeforeCreate|AfterCreate|BeforeUpsert|AfterUpsert|BeforeDelete|AfterDelete|OnInit|OnLoad)\s*\(\s*\)(?:\s*\n\s*@[^\n]*)*\s*\n\s*(?:async\s+)?(\w+)\s*\(`,
)

// sequelizeHookEvents is the set of Sequelize hook method / option names.
var sequelizeHookEvents = map[string]bool{
	"beforeValidate": true, "afterValidate": true,
	"beforeCreate": true, "afterCreate": true,
	"beforeUpdate": true, "afterUpdate": true,
	"beforeSave": true, "afterSave": true,
	"beforeDestroy": true, "afterDestroy": true,
	"beforeBulkCreate": true, "afterBulkCreate": true,
	"beforeUpsert": true, "afterUpsert": true,
}

// sequelizeHookCallRe matches `User.afterCreate(<handler>` and
// `User.afterCreate('name', <handler>`. Group 1 = model name; Group 2 = hook
// event; Group 3 = a named-function handler if the first arg is a bare
// identifier (anonymous arrow/function handlers leave it empty → skipped).
var sequelizeHookCallRe = regexp.MustCompile(
	`(?m)\b([A-Z]\w*)\.(beforeValidate|afterValidate|beforeCreate|afterCreate|beforeUpdate|afterUpdate|beforeSave|afterSave|beforeDestroy|afterDestroy|beforeBulkCreate|afterBulkCreate|beforeUpsert|afterUpsert)\s*\(\s*(?:['"][^'"]*['"]\s*,\s*)?([A-Za-z_]\w*)\s*\)`,
)

// mongooseHookRe matches `<schema>.post('save', <handler>)` and
// `<schema>.pre('save', <handler>)` where the handler is a bare named
// function identifier. Group 1 = pre|post; Group 2 = event; Group 3 = handler.
// Anonymous handlers (arrow / function-expression) leave Group 3 empty →
// skipped (honest-partial: no symbol to point TRIGGERS at).
var mongooseHookRe = regexp.MustCompile(
	`(?m)\b(\w+)\.(pre|post)\s*\(\s*['"]([\w]+)['"]\s*,\s*(?:function\s*)?([A-Za-z_]\w*)\s*\)`,
)

// mongooseSchemaModelRe maps a schema variable to a model name via
// `mongoose.model('User', <schemaVar>)` / `model<IUser>('User', <schemaVar>)`.
// Group 1 = model name; Group 2 = schema variable. Lets us name the event node
// after the model rather than the bare schema variable when both are present.
var mongooseSchemaModelRe = regexp.MustCompile(
	`(?m)\bmodel(?:<[^>]+>)?\s*\(\s*['"](\w+)['"]\s*,\s*(\w+)\s*\)`,
)

// ---------------------------------------------------------------------------
// Pass entry point
// ---------------------------------------------------------------------------

// applyORMLifecycleHookEdges is the per-file engine pass. It emits
// SCOPE.ModelEvent entities + TRIGGERS edges (event → handler) for every
// resolvable ORM lifecycle hook / model signal in the file. Append-only.
func applyORMLifecycleHookEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	src := string(content)

	seenEvent := map[string]bool{}
	seenEdge := map[string]bool{}

	// emit registers a model-event node (once) and a TRIGGERS edge to the
	// handler. model, event, and handler must all be non-empty; callers that
	// cannot resolve any of the three must skip (honest-partial).
	emit := func(model, event, handler, framework string, line int) {
		if model == "" || event == "" || handler == "" {
			return
		}
		nodeName := model + "." + event
		stub := modelEventKind + ":" + nodeName
		if !seenEvent[stub] {
			seenEvent[stub] = true
			entities = append(entities, types.EntityRecord{
				ID:         stub,
				Name:       nodeName,
				Kind:       modelEventKind,
				SourceFile: path,
				Language:   lang,
				Properties: map[string]string{
					"model":        model,
					"event":        event,
					"framework":    framework,
					"pattern_type": "orm_lifecycle_hook_synthesis",
					"line":         strconv.Itoa(line),
				},
				EnrichmentRequired: false,
				EnrichmentStatus:   types.StatusPending,
				QualityScore:       0.8,
			})
		}
		key := stub + "|" + handler
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: stub,
			ToID:   "Function:" + handler,
			Kind:   string(types.RelationshipKindTriggers),
			Properties: types.Props{
				{K: "event", V: event},
				{K: "framework", V: framework},
				{K: "model", V: model},
				{K: "pattern_type", V: "orm_lifecycle_hook_synthesis"},
			},
		})
	}

	lineAt := func(off int) int { return strings.Count(src[:off], "\n") + 1 }

	switch lang {
	case "python":
		synthesizePythonORMHooks(src, emit, lineAt)
	case "ruby":
		synthesizeRubyARCallbacks(src, emit, lineAt)
	case "javascript", "typescript":
		synthesizeJSTSORMHooks(src, emit, lineAt)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// synthesizePythonORMHooks handles Django built-in signals (with a concrete
// sender model) and SQLAlchemy @event.listens_for events.
func synthesizePythonORMHooks(src string, emit func(model, event, handler, framework string, line int), lineAt func(int) int) {
	if strings.Contains(src, "@receiver") {
		for _, m := range ormReceiverRe.FindAllStringSubmatchIndex(src, -1) {
			signalExpr := src[m[2]:m[3]]
			// Use the final dotted segment as the signal name (signals.post_save).
			signal := signalExpr
			if i := strings.LastIndex(signal, "."); i >= 0 {
				signal = signal[i+1:]
			}
			if !djangoBuiltinSignals[signal] {
				continue // custom signal → pub/sub topic, handled elsewhere
			}
			if m[4] == -1 {
				continue // no sender= → dynamic/all-models, honest-partial skip
			}
			sender := src[m[4]:m[5]]
			// A dotted sender (e.g. app.models.User) — take the final segment.
			if i := strings.LastIndex(sender, "."); i >= 0 {
				sender = sender[i+1:]
			}
			handler := src[m[6]:m[7]]
			emit(sender, signal, handler, "django", lineAt(m[0]))
		}
	}
	if strings.Contains(src, "listens_for") {
		for _, m := range sqlAlchemyListensRe.FindAllStringSubmatchIndex(src, -1) {
			target := src[m[2]:m[3]]
			if i := strings.LastIndex(target, "."); i >= 0 {
				target = target[i+1:]
			}
			// Lower-cased / generic targets (e.g. a `Session`/`mapper` literal)
			// are not a concrete model — require a Capitalized class name.
			if target == "" || target[0] < 'A' || target[0] > 'Z' {
				continue
			}
			event := src[m[4]:m[5]]
			handler := src[m[6]:m[7]]
			emit(target, event, handler, "sqlalchemy", lineAt(m[0]))
		}
	}
}

// synthesizeRubyARCallbacks handles ActiveRecord callbacks declared inside an
// ApplicationRecord / ActiveRecord::Base subclass. Each callback symbol
// becomes a TRIGGERS edge to the model's instance method.
func synthesizeRubyARCallbacks(src string, emit func(model, event, handler, framework string, line int), lineAt func(int) int) {
	classMatches := rubyClassRe.FindAllStringSubmatchIndex(src, -1)
	if len(classMatches) == 0 {
		return
	}
	// Determine each class's [start,end) byte span so callbacks are attributed
	// to the model whose body contains them. The body of class i runs until the
	// start of class i+1 (a flat approximation that is correct for the common
	// one-model-per-file / sequential-class layout).
	for ci, cm := range classMatches {
		model := src[cm[2]:cm[3]]
		bodyStart := cm[1]
		bodyEnd := len(src)
		if ci+1 < len(classMatches) {
			bodyEnd = classMatches[ci+1][0]
		}
		body := src[bodyStart:bodyEnd]
		for _, cb := range arCallbackRe.FindAllStringSubmatchIndex(body, -1) {
			macro := body[cb[2]:cb[3]]
			if !arCallbackNames[macro] {
				continue
			}
			symList := body[cb[4]:cb[5]]
			for _, sm := range arSymbolRe.FindAllStringSubmatch(symList, -1) {
				handler := sm[1]
				emit(model, macro, handler, "activerecord", lineAt(bodyStart+cb[0]))
			}
		}
	}
}

// synthesizeJSTSORMHooks handles TypeORM entity lifecycle decorators,
// MikroORM entity lifecycle decorators, Sequelize hook registrations, and
// Mongoose pre/post middleware.
func synthesizeJSTSORMHooks(src string, emit func(model, event, handler, framework string, line int), lineAt func(int) int) {
	// --- TypeORM: @AfterInsert() etc. methods inside an @Entity class -------
	if strings.Contains(src, "@Entity") {
		// Map each entity class to its body span.
		type span struct {
			name               string
			bodyStart, bodyEnd int
		}
		var spans []span
		ecMatches := tsEntityClassRe.FindAllStringSubmatchIndex(src, -1)
		for ei, em := range ecMatches {
			name := ""
			if em[2] != -1 {
				name = src[em[2]:em[3]]
			} else if em[4] != -1 {
				name = src[em[4]:em[5]]
			}
			bodyStart := em[1]
			bodyEnd := len(src)
			if ei+1 < len(ecMatches) {
				bodyEnd = ecMatches[ei+1][0]
			}
			spans = append(spans, span{name, bodyStart, bodyEnd})
		}
		for _, sp := range spans {
			body := src[sp.bodyStart:sp.bodyEnd]
			for _, lm := range tsLifecycleMethodRe.FindAllStringSubmatchIndex(body, -1) {
				decorator := body[lm[2]:lm[3]]
				method := body[lm[4]:lm[5]]
				event := typeormDecoratorEvents[decorator]
				emit(sp.name, event, method, "typeorm", lineAt(sp.bodyStart+lm[0]))
			}
			// MikroORM-exclusive lifecycle decorators on the same @Entity
			// classes (@BeforeCreate/@AfterCreate/@BeforeUpsert/@AfterUpsert/
			// @BeforeDelete/@AfterDelete/@OnInit/@OnLoad). Disjoint from the
			// TypeORM decorator set above, so no double-emit.
			for _, lm := range mikroLifecycleMethodRe.FindAllStringSubmatchIndex(body, -1) {
				decorator := body[lm[2]:lm[3]]
				method := body[lm[4]:lm[5]]
				event := mikroORMDecoratorEvents[decorator]
				emit(sp.name, event, method, "mikro-orm", lineAt(sp.bodyStart+lm[0]))
			}
		}
	}

	// --- Sequelize: User.afterCreate(handler) -------------------------------
	if strings.Contains(src, ".afterCreate") || strings.Contains(src, ".beforeCreate") ||
		strings.Contains(src, ".afterSave") || strings.Contains(src, ".beforeSave") ||
		strings.Contains(src, ".afterUpdate") || strings.Contains(src, ".beforeUpdate") ||
		strings.Contains(src, ".afterValidate") || strings.Contains(src, ".beforeValidate") ||
		strings.Contains(src, ".afterDestroy") || strings.Contains(src, ".beforeDestroy") {
		for _, hm := range sequelizeHookCallRe.FindAllStringSubmatchIndex(src, -1) {
			model := src[hm[2]:hm[3]]
			event := src[hm[4]:hm[5]]
			if !sequelizeHookEvents[event] {
				continue
			}
			handler := src[hm[6]:hm[7]]
			emit(model, event, handler, "sequelize", lineAt(hm[0]))
		}
	}

	// --- Mongoose: schema.post('save', sendEmail) ---------------------------
	if strings.Contains(src, ".post(") || strings.Contains(src, ".pre(") {
		// Resolve schema variable → model name where possible.
		schemaToModel := map[string]string{}
		for _, sm := range mongooseSchemaModelRe.FindAllStringSubmatch(src, -1) {
			schemaToModel[sm[2]] = sm[1]
		}
		for _, hm := range mongooseHookRe.FindAllStringSubmatchIndex(src, -1) {
			schemaVar := src[hm[2]:hm[3]]
			phase := src[hm[4]:hm[5]]
			event := src[hm[6]:hm[7]]
			handler := src[hm[8]:hm[9]]
			model := schemaToModel[schemaVar]
			if model == "" {
				// Fall back to the schema variable, trimming a `Schema` suffix
				// so `userSchema.post(...)` reads as model `user`. Still a
				// concrete, queryable identity (not fabricated).
				model = strings.TrimSuffix(schemaVar, "Schema")
				if model == "" {
					continue
				}
			}
			// Mongoose event id is phase+event (pre/post) e.g. "pre.save",
			// "post.save" — keep both so pre vs post are distinct nodes.
			emit(model, phase+"."+event, handler, "mongoose", lineAt(hm[0]))
		}
	}
}
