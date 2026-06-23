// mobile_navigation.go — Issue #2860.
//
// Mobile navigation / native-bridge / platform-branch extraction for the four
// mobile framework families: Expo, React Native, Ionic, NativeScript.
//
// The pre-existing navigation.go covers the *call* side of navigation
// (router.push / navigation.navigate / <Link>). This file covers the
// *declaration* and *configuration* side that those frameworks rely on, plus
// the native-bridge and platform-branch dimensions:
//
//   - Navigator / screen DECLARATIONS (navigation_extraction, screen_detection)
//     React Navigation:  createStackNavigator / createBottomTabNavigator /
//     createDrawerNavigator / createNativeStackNavigator,
//     <Stack.Screen name=… component=…>,
//     <Tab.Screen name=…>.
//     Ionic:             <IonRouterOutlet>, <Route path=… component=…>,
//     <IonTabs>/<IonTabButton tab=…>.
//     NativeScript:      <Frame>, <Page>, Vue/Angular <RouterExtensions>,
//     $navigateTo(Page), registerElement('Foo', …).
//
//   - Deep-link CONFIG (deep_link_extraction)
//     Linking.createURL('/x'), Linking.getInitialURL,
//     a `linking = { prefixes: [...], config: { screens: {...} } }` object,
//     Expo Router `scheme` / app.json deep-link config surfaced in code.
//
//   - Native-bridge imports (native_module_imports)
//     NativeModules.<Mod>, requireNativeComponent('Foo'),
//     new NativeEventEmitter(…), TurboModuleRegistry.get*,
//     and imports of native packages (react-native-*, expo-*,
//     @nativescript/*, @capacitor/*, NativeModules from 'react-native').
//
//   - Platform branching (platform_branching)
//     Platform.OS === 'ios' / Platform.OS !== 'android', Platform.select({…}),
//     Ionic isPlatform('ios'), Capacitor.getPlatform(), and the .ios/.android
//     file-variant split already detected by platform_variants.go.
//
// All signals decorate the file-level entity (entities[0] for the file) with
// summary Properties and emit edges with REUSED kinds only — NAVIGATES_TO for
// screen/route declarations and deep links, IMPORTS decoration (a property on
// the existing edge) for native modules, and BRANCHES_ON for platform
// branches. No new entity or edge Kinds are introduced.
//
// The pass is tolerant: a panic is recovered so the primary pipeline is
// unaffected.
package javascript

import (
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// navigatorFactoryNames is the set of React-Navigation navigator factory
// functions whose return value owns a `.Screen` / `.Navigator` pair. A call to
// any of these is a navigator declaration (navigation_extraction).
var navigatorFactoryNames = map[string]bool{
	"createStackNavigator":             true,
	"createNativeStackNavigator":       true,
	"createBottomTabNavigator":         true,
	"createMaterialTopTabNavigator":    true,
	"createMaterialBottomTabNavigator": true,
	"createDrawerNavigator":            true,
	"createSwitchNavigator":            true,
}

// screenJSXTagSuffixes are the JSX tag *suffixes* that denote a screen/route
// declaration. We match on suffix so `Stack.Screen`, `Tab.Screen`,
// `Drawer.Screen` and a bare `Screen` all qualify, and Ionic's `Route` /
// NativeScript's `Page` are covered by exact tags below.
var screenJSXTagSuffixes = []string{".Screen"}

// screenJSXExactTags are JSX tag names (exact match) that declare a screen or
// route across the mobile families.
var screenJSXExactTags = map[string]bool{
	"Screen": true, // bare React Navigation <Screen name=…> (destructured)
	"Route":  true, // Ionic / react-router <Route path=… component=…>
	"Page":   true, // NativeScript <Page>
}

// navigatorJSXExactTags are container JSX tags that declare a navigator/router
// outlet (navigation_extraction) without themselves being a single screen.
var navigatorJSXExactTags = map[string]bool{
	"IonRouterOutlet":     true,
	"IonTabs":             true,
	"IonReactRouter":      true,
	"NavigationContainer": true,
	"Frame":               true, // NativeScript navigation frame
}

// nativeModulePackagePrefixes are import-specifier prefixes that denote a
// native (bridged) module. Importing any of these surfaces a native-bridge
// dependency (native_module_imports).
var nativeModulePackagePrefixes = []string{
	"react-native-",
	"expo-",
	"@nativescript/",
	"@capacitor/",
	"@react-native-",
}

// nativeBridgeMembers are receiver identifiers whose member access denotes a
// native-bridge boundary (NativeModules.Foo, TurboModuleRegistry.get).
var nativeBridgeMembers = map[string]bool{
	"NativeModules":       true,
	"TurboModuleRegistry": true,
}

// nativeBridgeCallees are call-expression callee names whose invocation crosses
// the native boundary.
var nativeBridgeCallees = map[string]bool{
	"requireNativeComponent": true,
	"requireNativeModule":    true, // Expo modules core
	"NativeEventEmitter":     true,
	"codegenNativeComponent": true, // RN new-arch (Fabric) component spec
	"codegenNativeCommands":  true, // RN new-arch native commands spec
}

// nativeBridgeKindForCallee maps a native-bridge call-expression callee to the
// subtype recorded on the emitted SCOPE.External native-boundary entity. A
// component callee yields "native_component"; a module/registry callee yields
// "native_module".
var nativeBridgeKindForCallee = map[string]string{
	"requireNativeComponent": "native_component",
	"codegenNativeComponent": "native_component",
	"codegenNativeCommands":  "native_component",
	"requireNativeModule":    "native_module",
}

// turboModuleRegistryGetters are the TurboModuleRegistry methods whose string
// argument names the requested TurboModule (RN new architecture).
var turboModuleRegistryGetters = map[string]bool{
	"get":          true,
	"getEnforcing": true,
}

// nativeBridgeDep is a discovered JS↔native boundary dependency: a native
// module or native component the JS code reaches across the bridge. `name` is
// the module/component name (e.g. "BiometricAuth", "RCTMapView"); `subtype` is
// "native_module" or "native_component"; `via` records how it was discovered.
type nativeBridgeDep struct {
	name    string
	subtype string
	via     string
	line    int
}

// platformBranchReceivers are receiver identifiers whose member-comparison is a
// platform branch (platform_branching).
var platformBranchReceivers = map[string]bool{
	"Platform":  true, // React Native / Expo: Platform.OS
	"Capacitor": true, // Ionic: Capacitor.getPlatform()
}

// platformBranchCallees are call-expression callee names whose result drives a
// platform branch.
var platformBranchCallees = map[string]bool{
	"isPlatform":  true, // Ionic: isPlatform('ios')
	"getPlatform": true, // Capacitor.getPlatform()
	"select":      true, // Platform.select({ ios, android }) — gated on receiver
}

// emitMobileNavigationSignals runs the four mobile dimensions over the file's
// AST and decorates entities[0] (the file entity) with summary properties plus
// reused-kind edges. Called from Extract() after emitPlatformVariantRelationships.
func (x *extractor) emitMobileNavigationSignals(root ts.Node) {
	if root == nil || len(x.entities) == 0 {
		return
	}
	defer func() { _ = recover() }()

	fileEnt := x.mobileFileEntity()
	if fileEnt == nil {
		return
	}

	x.emitNativeModuleSignals(root, fileEnt)
	x.emitNavigatorScreenSignals(root, fileEnt)
	x.emitDeepLinkSignals(root, fileEnt)
	x.emitPlatformBranchSignals(root, fileEnt)
}

// mobileFileEntity returns the file-level SCOPE.Component entity for the file
// being extracted, or nil if not present.
func (x *extractor) mobileFileEntity() *types.EntityRecord {
	for i := range x.entities {
		if x.entities[i].Subtype == "file" && x.entities[i].SourceFile == x.filePath {
			return &x.entities[i]
		}
	}
	return nil
}

func ensureProps(e *types.EntityRecord) {
	if e.Properties == nil {
		e.Properties = make(map[string]string)
	}
}

// joinSorted dedupes, sorts and comma-joins a slice of strings.
func joinSorted(items []string) string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, s := range items {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// ── Native bridge ────────────────────────────────────────────────────────────

// isNativeModuleSpecifier reports whether an import specifier names a native
// (bridged) package.
func isNativeModuleSpecifier(spec string) bool {
	if spec == "react-native" {
		// react-native itself is the bridge host; only flag it when the binding
		// pulls in NativeModules (handled separately via importByLocal). The
		// bare module is not a native package on its own.
		return false
	}
	for _, p := range nativeModulePackagePrefixes {
		if strings.HasPrefix(spec, p) {
			return true
		}
	}
	return false
}

// emitNativeModuleSignals detects native-bridge imports and access and stamps
// the file entity's `native_modules` property plus a `native_module=1` marker
// on the matching IMPORTS edge (no new edge kind).
func (x *extractor) emitNativeModuleSignals(root ts.Node, fileEnt *types.EntityRecord) {
	var mods []string
	var deps []nativeBridgeDep

	// 1. Native package imports — both the IMPORTS edges already on the file
	//    entity and the raw import bindings (importByLocal) are inspected.
	for i := range fileEnt.Relationships {
		rel := &fileEnt.Relationships[i]
		if rel.Kind != "IMPORTS" {
			continue
		}
		// import_path carries the raw, un-dotted module specifier
		// (e.g. "@capacitor/geolocation"); source_module is dotted
		// ("@capacitor.geolocation") so it cannot be prefix-matched.
		spec := rel.Properties["import_path"]
		if spec == "" {
			spec = rel.ToID
		}
		// A NativeModules / requireNativeComponent binding from 'react-native'.
		named := rel.Properties["imported_name"]
		if isNativeModuleSpecifier(spec) {
			ensurePropsRel(rel)
			rel.Properties["native_module"] = "1"
			mods = append(mods, spec)
		} else if spec == "react-native" && (named == "NativeModules" || named == "requireNativeComponent" || named == "NativeEventEmitter" || named == "TurboModuleRegistry") {
			ensurePropsRel(rel)
			rel.Properties["native_module"] = "1"
			mods = append(mods, "react-native:"+named)
		}
	}

	// 2. Member access: NativeModules.Foo / TurboModuleRegistry.get('Foo').
	//    A bare `NativeModules.BiometricAuth` names the module "BiometricAuth";
	//    `TurboModuleRegistry.get('Foo')` / `.getEnforcing('Foo')` names the
	//    requested TurboModule via its string argument.
	for _, mem := range findAllNodes(root, "member_expression") {
		obj := mem.ChildByFieldName("object")
		prop := mem.ChildByFieldName("property")
		if obj == nil || prop == nil {
			continue
		}
		recv := x.nodeText(obj)
		if !nativeBridgeMembers[recv] {
			continue
		}
		member := x.nodeText(prop)
		mods = append(mods, recv+"."+member)
		switch recv {
		case "NativeModules":
			// NativeModules.BiometricAuth — `member` is the native module name.
			deps = append(deps, nativeBridgeDep{
				name: member, subtype: "native_module",
				via: "NativeModules", line: int(mem.StartPoint().Row) + 1,
			})
		case "TurboModuleRegistry":
			// TurboModuleRegistry.get('Foo') / .getEnforcing('Foo') — the name is
			// the string argument of the enclosing call, not `member` itself.
			if turboModuleRegistryGetters[member] {
				if call := enclosingCall(mem); call != nil {
					if name := stringArg(x, call); name != "" {
						deps = append(deps, nativeBridgeDep{
							name: name, subtype: "native_module",
							via:  "TurboModuleRegistry." + member,
							line: int(mem.StartPoint().Row) + 1,
						})
					}
				}
			}
		}
	}

	// 2b. Destructured native modules: `const { BiometricAuth } = NativeModules`
	//     — the idiomatic RN form. Each destructured binding names a module.
	for _, decl := range findAllNodes(root, "variable_declarator") {
		val := decl.ChildByFieldName("value")
		if val == nil || val.Type() != "identifier" || x.nodeText(val) != "NativeModules" {
			continue
		}
		nameNode := decl.ChildByFieldName("name")
		if nameNode == nil || nameNode.Type() != "object_pattern" {
			continue
		}
		for i := 0; i < int(nameNode.ChildCount()); i++ {
			c := nameNode.Child(i)
			if c == nil {
				continue
			}
			var local string
			switch c.Type() {
			case "shorthand_property_identifier_pattern", "shorthand_property_identifier":
				local = x.nodeText(c)
			case "pair_pattern":
				local = strings.Trim(x.nodeText(c.ChildByFieldName("key")), `"'`+"`")
			}
			if local == "" {
				continue
			}
			mods = append(mods, "NativeModules."+local)
			deps = append(deps, nativeBridgeDep{
				name: local, subtype: "native_module",
				via: "NativeModules", line: int(decl.StartPoint().Row) + 1,
			})
		}
	}

	// 3. Native-bridge calls: requireNativeComponent('RCTFoo'), new
	//    NativeEventEmitter(), codegenNativeComponent('RCTFoo') (RN new arch),
	//    requireNativeModule('ExpoFoo') (Expo modules core).
	for _, call := range findAllNodes(root, "call_expression", "new_expression") {
		fn := call.ChildByFieldName("function")
		var callee string
		switch {
		case fn != nil && fn.Type() == "identifier":
			callee = x.nodeText(fn)
		case call.Type() == "new_expression":
			if c := call.ChildByFieldName("constructor"); c != nil {
				callee = x.nodeText(c)
			}
		}
		if callee == "" {
			continue
		}
		if nativeBridgeCallees[callee] {
			arg := stringArg(x, call)
			if arg != "" {
				mods = append(mods, callee+"('"+arg+"')")
				if subtype, ok := nativeBridgeKindForCallee[callee]; ok {
					deps = append(deps, nativeBridgeDep{
						name: arg, subtype: subtype,
						via: callee, line: int(call.StartPoint().Row) + 1,
					})
				}
			} else {
				mods = append(mods, callee+"()")
			}
		}
	}

	if len(mods) > 0 {
		ensureProps(fileEnt)
		fileEnt.Properties["native_modules"] = joinSorted(mods)
	}

	x.emitNativeBridgeEntities(fileEnt, deps)
}

// enclosingCall walks up from a member_expression to the call_expression that
// invokes it (so TurboModuleRegistry.get('Foo') resolves from the .get member
// to the call carrying the 'Foo' argument), or returns nil.
func enclosingCall(mem ts.Node) ts.Node {
	for n := mem.Parent(); n != nil; n = n.Parent() {
		switch n.Type() {
		case "call_expression":
			// Confirm `mem` is the function being called, not an argument.
			if fn := n.ChildByFieldName("function"); fn != nil &&
				fn.StartByte() <= mem.StartByte() && fn.EndByte() >= mem.EndByte() {
				return n
			}
			return nil
		case "arguments", "statement_block", "program":
			return nil
		}
	}
	return nil
}

// emitNativeBridgeEntities materialises one SCOPE.External entity per distinct
// JS↔native boundary dependency (native module / native component) and a
// DEPENDS_ON edge from the file entity to it. This makes the bridge boundary
// first-class and queryable (vs. the summary `native_modules` property). The
// edge ToID uses the `native_module:<name>` synthetic form mirrored by
// addFileNavEdge's `route:` convention so the resolver binds it by name.
func (x *extractor) emitNativeBridgeEntities(fileEnt *types.EntityRecord, deps []nativeBridgeDep) {
	seen := make(map[string]bool, len(deps))
	var ents []types.EntityRecord
	// Append every DEPENDS_ON edge to the file entity FIRST, then append the
	// SCOPE.External entities. Appending entities to x.entities may reallocate
	// its backing array and invalidate the fileEnt pointer, so all writes
	// through fileEnt must complete before x.entities grows.
	for _, d := range deps {
		if d.name == "" {
			continue
		}
		key := d.subtype + ":" + d.name
		if seen[key] {
			continue
		}
		seen[key] = true

		fileEnt.Relationships = append(fileEnt.Relationships, types.RelationshipRecord{
			ToID: "native_module:" + d.name,
			Kind: string(types.RelationshipKindDependsOn),
			Properties: map[string]string{
				"native_bridge": "1",
				"native_name":   d.name,
				"subtype":       d.subtype,
				"via":           d.via,
				"line":          strconv.Itoa(d.line),
			},
		})

		ent := types.EntityRecord{
			Name:          d.name,
			QualifiedName: x.qualify(d.name),
			Kind:          string(types.EntityKindExternal),
			SourceFile:    x.filePath,
			StartLine:     d.line,
			EndLine:       d.line,
			Language:      x.language,
			Subtype:       d.subtype,
			Properties: map[string]string{
				"kind":          string(types.EntityKindExternal),
				"subtype":       d.subtype,
				"native_bridge": "1",
				"native_name":   d.name,
				"via":           d.via,
			},
			EnrichmentStatus: types.StatusPending,
			QualityScore:     1.0,
		}
		ent.ID = ent.ComputeID()
		ents = append(ents, ent)
	}
	x.entities = append(x.entities, ents...)
}

func ensurePropsRel(rel *types.RelationshipRecord) {
	if rel.Properties == nil {
		rel.Properties = make(map[string]string)
	}
}

// stringArg returns the first string-literal argument of a call, unquoted, or "".
func stringArg(x *extractor, call ts.Node) string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	first := firstMeaningfulArg(args)
	if first == nil || first.Type() != "string" {
		return ""
	}
	return strings.Trim(x.nodeText(first), `"'`+"`")
}

// ── Navigator / screen declarations ──────────────────────────────────────────

// emitNavigatorScreenSignals detects navigator factory calls and screen/route
// JSX declarations, stamping `navigators` / `screens` on the file entity and
// emitting one NAVIGATES_TO edge per declared screen (via=screen_config) so the
// linker can match screens to route call sites.
func (x *extractor) emitNavigatorScreenSignals(root ts.Node, fileEnt *types.EntityRecord) {
	var navigators, screens []string

	// 1. Navigator factory calls: const Stack = createStackNavigator().
	for _, call := range findAllNodes(root, "call_expression") {
		fn := call.ChildByFieldName("function")
		if fn == nil || fn.Type() != "identifier" {
			continue
		}
		if navigatorFactoryNames[x.nodeText(fn)] {
			navigators = append(navigators, x.nodeText(fn))
		}
	}

	// 1b. NativeScript core navigation: frame.navigate({ moduleName: 'home-page' })
	//     declares a destination screen (the moduleName), and registerElement(
	//     'CustomTag', () => …) registers a navigable element. Both are screen
	//     declarations in the NativeScript (non-React) model.
	for _, call := range findAllNodes(root, "call_expression") {
		fn := call.ChildByFieldName("function")
		if fn == nil {
			continue
		}
		switch fn.Type() {
		case "identifier":
			if x.nodeText(fn) == "registerElement" {
				if tag := stringArg(x, call); tag != "" {
					navigators = append(navigators, "registerElement:"+tag)
				}
			}
		case "member_expression":
			if x.nodeText(fn.ChildByFieldName("property")) != "navigate" {
				continue
			}
			if mod := x.moduleNameArg(call); mod != "" {
				screens = append(screens, mod)
				x.addFileNavEdge(fileEnt, mod, "screen_config", call, "frame.navigate")
			}
		}
	}

	// 2. Screen / route / navigator-outlet JSX declarations.
	jsx := findAllNodes(root, "jsx_opening_element", "jsx_self_closing_element")
	for _, el := range jsx {
		nameNode := el.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		tag := x.nodeText(nameNode)
		switch {
		case navigatorJSXExactTags[tag]:
			navigators = append(navigators, tag)
		case isScreenTag(tag):
			name := jsxAttrValue(x, el, "name")
			if name == "" {
				name = jsxAttrValue(x, el, "path") // Ionic / Route
			}
			if name != "" {
				screens = append(screens, name)
				x.addFileNavEdge(fileEnt, name, "screen_config", el, tag)
			}
		}
	}

	if len(navigators) > 0 {
		ensureProps(fileEnt)
		fileEnt.Properties["navigators"] = joinSorted(navigators)
	}
	if len(screens) > 0 {
		ensureProps(fileEnt)
		fileEnt.Properties["screens"] = joinSorted(screens)
	}
}

// isScreenTag reports whether a JSX tag declares a single screen/route.
func isScreenTag(tag string) bool {
	if screenJSXExactTags[tag] {
		return true
	}
	for _, sfx := range screenJSXTagSuffixes {
		if strings.HasSuffix(tag, sfx) {
			return true
		}
	}
	return false
}

// addFileNavEdge appends a NAVIGATES_TO edge to the file entity for a declared
// screen/route or deep-link target.
func (x *extractor) addFileNavEdge(fileEnt *types.EntityRecord, route, via string, node ts.Node, tag string) {
	props := map[string]string{
		"route": route,
		"line":  strconv.Itoa(int(node.StartPoint().Row) + 1),
		"via":   via,
	}
	if tag != "" {
		props["tag"] = tag
	}
	// Dedupe per (route,via) on the file entity.
	for i := range fileEnt.Relationships {
		r := &fileEnt.Relationships[i]
		if r.Kind == "NAVIGATES_TO" && r.ToID == "route:"+route && r.Properties["via"] == via {
			return
		}
	}
	fileEnt.Relationships = append(fileEnt.Relationships, types.RelationshipRecord{
		ToID:       "route:" + route,
		Kind:       string(types.RelationshipKindNavigatesTo),
		Properties: props,
	})
}

// moduleNameArg returns the `moduleName` string value from the first object-
// literal argument of a NativeScript frame.navigate({ moduleName: 'x' }) call,
// or "" if absent. A bare-string first argument (frame.navigate('home')) is
// also accepted.
func (x *extractor) moduleNameArg(call ts.Node) string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	first := firstMeaningfulArg(args)
	if first == nil {
		return ""
	}
	if first.Type() == "string" {
		return strings.Trim(x.nodeText(first), `"'`+"`")
	}
	if !isObjectLiteral(first) {
		return ""
	}
	for i := 0; i < int(first.ChildCount()); i++ {
		pair := first.Child(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		if strings.Trim(x.nodeText(pair.ChildByFieldName("key")), `"'`+"`") != "moduleName" {
			continue
		}
		val := pair.ChildByFieldName("value")
		if val != nil && val.Type() == "string" {
			return strings.Trim(x.nodeText(val), `"'`+"`")
		}
	}
	return ""
}

// jsxAttrValue returns the string/template-literal value of the named attribute
// on a JSX opening/self-closing element, or "" if absent or non-literal.
func jsxAttrValue(x *extractor, el ts.Node, attrName string) string {
	for i := 0; i < int(el.ChildCount()); i++ {
		attr := el.Child(i)
		if attr == nil || attr.Type() != "jsx_attribute" {
			continue
		}
		var nameChild ts.Node
		for j := 0; j < int(attr.ChildCount()); j++ {
			c := attr.Child(j)
			if c != nil && (c.Type() == "property_identifier" || c.Type() == "identifier") {
				nameChild = c
				break
			}
		}
		if nameChild == nil || x.nodeText(nameChild) != attrName {
			continue
		}
		for j := 0; j < int(attr.ChildCount()); j++ {
			val := attr.Child(j)
			if val == nil || val == nameChild {
				continue
			}
			switch val.Type() {
			case "string":
				return strings.Trim(x.nodeText(val), `"'`+"`")
			case "template_string":
				return normalizeTemplateLiteralRoute(x.nodeText(val))
			case "jsx_expression":
				for k := 0; k < int(val.ChildCount()); k++ {
					inner := val.Child(k)
					if inner == nil {
						continue
					}
					switch inner.Type() {
					case "string":
						return strings.Trim(x.nodeText(inner), `"'`+"`")
					case "template_string":
						return normalizeTemplateLiteralRoute(x.nodeText(inner))
					}
				}
			}
		}
	}
	return ""
}

// ── Deep links ───────────────────────────────────────────────────────────────

// emitDeepLinkSignals detects Expo/RN deep-link configuration — Linking.createURL,
// Linking.getInitialURL, and a `linking` config object with prefixes + screens —
// and stamps `deep_link_prefixes` / `deep_link_screens` plus NAVIGATES_TO edges
// (via=deep_link) on the file entity.
func (x *extractor) emitDeepLinkSignals(root ts.Node, fileEnt *types.EntityRecord) {
	var prefixes, screens []string

	// 1. Linking.createURL('/path') / Linking.openURL — the call form is already
	//    handled by navigation.go for openURL; here we capture createURL and
	//    getInitialURL which seed the app's deep-link scheme.
	for _, call := range findAllNodes(root, "call_expression") {
		fn := call.ChildByFieldName("function")
		if fn == nil || fn.Type() != "member_expression" {
			continue
		}
		obj := fn.ChildByFieldName("object")
		prop := fn.ChildByFieldName("property")
		if obj == nil || prop == nil || x.nodeText(obj) != "Linking" {
			continue
		}
		switch x.nodeText(prop) {
		case "createURL":
			if p := stringArg(x, call); p != "" {
				prefixes = append(prefixes, p)
				x.addFileNavEdge(fileEnt, p, "deep_link", call, "Linking.createURL")
			}
		}
	}

	// 1b. Capacitor / Ionic deep links: App.addListener('appUrlOpen', cb) is the
	//     Capacitor universal/deep-link entry point. The string literal
	//     'appUrlOpen' is the deep-link event; record it as a prefix marker so
	//     the file is recognised as a deep-link handler.
	for _, call := range findAllNodes(root, "call_expression") {
		fn := call.ChildByFieldName("function")
		if fn == nil || fn.Type() != "member_expression" {
			continue
		}
		if x.nodeText(fn.ChildByFieldName("property")) != "addListener" {
			continue
		}
		if stringArg(x, call) == "appUrlOpen" {
			prefixes = append(prefixes, "appUrlOpen")
			x.addFileNavEdge(fileEnt, "appUrlOpen", "deep_link", call, "App.addListener")
		}
	}

	// 1c. NativeScript deep links: handleOpenURL(handler) from
	//     nativescript-urlhandler / @nativescript-community/urlhandler, and
	//     Application.on('launch') with a deep-link intent. handleOpenURL is the
	//     canonical NativeScript deep-link registration call.
	for _, call := range findAllNodes(root, "call_expression") {
		fn := call.ChildByFieldName("function")
		if fn != nil && fn.Type() == "identifier" && x.nodeText(fn) == "handleOpenURL" {
			prefixes = append(prefixes, "handleOpenURL")
			x.addFileNavEdge(fileEnt, "handleOpenURL", "deep_link", call, "handleOpenURL")
		}
	}

	// 2. A `linking = { prefixes: [...], config: { screens: {...} } }` object —
	//    the React Navigation deep-link config shape.
	for _, declr := range findAllNodes(root, "variable_declarator", "pair") {
		var nameNode, valNode ts.Node
		if declr.Type() == "variable_declarator" {
			nameNode = declr.ChildByFieldName("name")
			valNode = declr.ChildByFieldName("value")
		} else {
			nameNode = declr.ChildByFieldName("key")
			valNode = declr.ChildByFieldName("value")
		}
		if nameNode == nil || valNode == nil {
			continue
		}
		name := strings.Trim(x.nodeText(nameNode), `"'`+"`")
		if name != "linking" || !isObjectLiteral(valNode) {
			continue
		}
		p, s := x.parseLinkingConfig(valNode)
		prefixes = append(prefixes, p...)
		for _, scr := range s {
			screens = append(screens, scr)
			x.addFileNavEdge(fileEnt, scr, "deep_link", valNode, "linking.config")
		}
	}

	if len(prefixes) > 0 {
		ensureProps(fileEnt)
		fileEnt.Properties["deep_link_prefixes"] = joinSorted(prefixes)
	}
	if len(screens) > 0 {
		ensureProps(fileEnt)
		fileEnt.Properties["deep_link_screens"] = joinSorted(screens)
	}
}

// parseLinkingConfig extracts the prefixes array entries and the config.screens
// key names from a React Navigation `linking` config object literal.
func (x *extractor) parseLinkingConfig(obj ts.Node) (prefixes, screens []string) {
	for i := 0; i < int(obj.ChildCount()); i++ {
		pair := obj.Child(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		key := strings.Trim(x.nodeText(pair.ChildByFieldName("key")), `"'`+"`")
		val := pair.ChildByFieldName("value")
		if val == nil {
			continue
		}
		switch key {
		case "prefixes":
			if val.Type() == "array" {
				for j := 0; j < int(val.ChildCount()); j++ {
					el := val.Child(j)
					if el != nil && el.Type() == "string" {
						prefixes = append(prefixes, strings.Trim(x.nodeText(el), `"'`+"`"))
					}
				}
			}
		case "config":
			if isObjectLiteral(val) {
				screens = append(screens, x.linkingScreenKeys(val)...)
			}
		}
	}
	return prefixes, screens
}

// linkingScreenKeys returns the screen-name keys from a `config: { screens: {…} }`
// object — the top-level keys under `screens`.
func (x *extractor) linkingScreenKeys(config ts.Node) []string {
	var out []string
	for i := 0; i < int(config.ChildCount()); i++ {
		pair := config.Child(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		if strings.Trim(x.nodeText(pair.ChildByFieldName("key")), `"'`+"`") != "screens" {
			continue
		}
		screensObj := pair.ChildByFieldName("value")
		if screensObj == nil || !isObjectLiteral(screensObj) {
			continue
		}
		for j := 0; j < int(screensObj.ChildCount()); j++ {
			sp := screensObj.Child(j)
			if sp == nil || sp.Type() != "pair" {
				continue
			}
			k := strings.Trim(x.nodeText(sp.ChildByFieldName("key")), `"'`+"`")
			if k != "" {
				out = append(out, k)
			}
		}
	}
	return out
}

// ── Platform branching ───────────────────────────────────────────────────────

// emitPlatformBranchSignals detects platform-conditional logic — Platform.OS
// comparisons, Platform.select({…}), Ionic isPlatform(…), Capacitor.getPlatform()
// — plus the .ios/.android file-variant split, and stamps `platform_branches`
// on the file entity. The generic branch_conditions pass already emits
// BRANCHES_ON edges for the `Platform.OS === 'ios'` comparison shape; this
// summary makes the platform dimension first-class and queryable.
func (x *extractor) emitPlatformBranchSignals(root ts.Node, fileEnt *types.EntityRecord) {
	var branches []string

	// 1. File-variant split (.ios.tsx / .android.tsx) — the file IS a platform
	//    branch even with no in-body conditional.
	if variant := platformVariantSuffix(x.filePath); variant != "" {
		branches = append(branches, "file:"+variant)
	}

	// 2. Platform.OS comparisons, Platform.select, and NativeScript Device.os.
	for _, mem := range findAllNodes(root, "member_expression") {
		obj := mem.ChildByFieldName("object")
		prop := mem.ChildByFieldName("property")
		if obj == nil || prop == nil {
			continue
		}
		o, p := x.nodeText(obj), x.nodeText(prop)
		switch {
		case platformBranchReceivers[o] && p == "OS":
			branches = append(branches, "Platform.OS")
		case o == "Device" && p == "os": // NativeScript: Device.os
			branches = append(branches, "Device.os")
		}
	}

	// 2b. NativeScript boolean platform flags: isIOS / isAndroid from
	//     '@nativescript/core'. Recognised only when imported from a native
	//     package so a same-named local does not trigger a false positive.
	if x.importByLocal["isIOS"] != nil && isNativeScriptCorePkg(x.importByLocal["isIOS"].importPath) {
		branches = append(branches, "isIOS")
	}
	if x.importByLocal["isAndroid"] != nil && isNativeScriptCorePkg(x.importByLocal["isAndroid"].importPath) {
		branches = append(branches, "isAndroid")
	}

	// 3. Platform.select({...}) and isPlatform('ios') / getPlatform() calls.
	for _, call := range findAllNodes(root, "call_expression") {
		fn := call.ChildByFieldName("function")
		if fn == nil {
			continue
		}
		switch fn.Type() {
		case "identifier":
			callee := x.nodeText(fn)
			if callee == "isPlatform" {
				if p := stringArg(x, call); p != "" {
					branches = append(branches, "isPlatform('"+p+"')")
				} else {
					branches = append(branches, "isPlatform()")
				}
			}
		case "member_expression":
			obj := fn.ChildByFieldName("object")
			prop := fn.ChildByFieldName("property")
			if obj == nil || prop == nil {
				continue
			}
			recv, method := x.nodeText(obj), x.nodeText(prop)
			if platformBranchReceivers[recv] && platformBranchCallees[method] {
				branches = append(branches, recv+"."+method+"()")
			}
		}
	}

	if len(branches) > 0 {
		ensureProps(fileEnt)
		fileEnt.Properties["platform_branches"] = joinSorted(branches)
	}
}

// isNativeScriptCorePkg reports whether an import specifier is a NativeScript
// core package (the source of isIOS / isAndroid / Device).
func isNativeScriptCorePkg(spec string) bool {
	return strings.HasPrefix(spec, "@nativescript/") || spec == "tns-core-modules" ||
		strings.HasPrefix(spec, "tns-core-modules/")
}

// platformVariantSuffix returns the platform suffix (e.g. "ios", "android",
// "web", "native") of a platform-variant file, or "" if the file carries no
// platform suffix. Mirrors isPlatformVariantFile's stripping but returns the
// suffix label rather than the canonical path.
func platformVariantSuffix(filePath string) string {
	canonical := isPlatformVariantFile(filePath)
	if canonical == "" {
		return ""
	}
	// The suffix is the segment removed between the bare name and the extension.
	// Recompute it from the original by stripping the extension then the
	// platform suffix segment.
	ext := extFromPath(filePath)
	if ext == "" {
		return ""
	}
	bare := filePath[:len(filePath)-len(ext)]
	if _, sfx, ok := stripOnePlatformSuffix(bare); ok {
		return strings.TrimPrefix(sfx, ".")
	}
	return ""
}

// extFromPath returns the file extension (including the dot) if it is a JS/TS
// variant extension, else "".
func extFromPath(p string) string {
	for ext := range jsVariantExts {
		if strings.HasSuffix(p, ext) {
			return ext
		}
	}
	return ""
}
