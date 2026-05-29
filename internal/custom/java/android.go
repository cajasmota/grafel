package java

import (
	"regexp"
	"strconv"
	"strings"
)

// Android Java custom extractor: Manifest, Intent, Fragments, ViewModel/LiveData.
// Ported from: android_extractor.py

var androidFrameworks = map[string]bool{
	"android_sdk": true, "android": true, "android_jetpack": true,
	"android_jetpack_compose":                                   true,
	"android_jetpack_(viewmodel/livedata/room/navigation/hilt)": true,
}

var (
	adManifestActivityRE = regexp.MustCompile(
		`(?m)<activity\b[^>]*android:name\s*=\s*\"([^\"]+)\"`)
	adManifestServiceRE = regexp.MustCompile(
		`(?m)<service\b[^>]*android:name\s*=\s*\"([^\"]+)\"`)
	adManifestReceiverRE = regexp.MustCompile(
		`(?m)<receiver\b[^>]*android:name\s*=\s*\"([^\"]+)\"`)
	adManifestProviderRE = regexp.MustCompile(
		`(?m)<provider\b[^>]*android:name\s*=\s*\"([^\"]+)\"`)
	adActivityClassRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+extends\s+` +
			`(?:AppCompatActivity|Activity|FragmentActivity|ComponentActivity|ListActivity` +
			`|TabActivity|LauncherActivity|PreferenceActivity)\b`)
	adFragmentClassRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+extends\s+` +
			`(?:Fragment|DialogFragment|BottomSheetDialogFragment|ListFragment` +
			`|PreferenceFragment|PreferenceFragmentCompat)\b`)
	adServiceClassRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+extends\s+` +
			`(?:Service|IntentService|JobIntentService|LifecycleService)\b`)
	adReceiverClassRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+extends\s+BroadcastReceiver\b`)
	adProviderClassRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+extends\s+ContentProvider\b`)
	adViewModelClassRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+extends\s+` +
			`(?:ViewModel|AndroidViewModel)\b`)
	adIntentExplicitRE = regexp.MustCompile(
		`(?m)new\s+Intent\s*\([^,]*,\s*(\w+)\.class\s*\)`)
	adFragmentTransactionRE = regexp.MustCompile(
		`(?s)(?:getSupportFragmentManager|getFragmentManager|getChildFragmentManager)` +
			`\s*\(\s*\)[^;]*?\.(?:add|replace)\s*\([^,]*,\s*(?:new\s+)?(\w+)\s*(?:\(\s*\))?`)
	adViewModelProviderRE = regexp.MustCompile(
		`(?m)(?:new\s+)?ViewModelProvider\s*\([^)]*\)\s*\.get\s*\(\s*(\w+)\.class\s*\)`)
	// platform_branching: Build.VERSION.SDK_INT comparisons against an API level.
	// Captures the comparison operator and the right-hand API-level token
	// (e.g. Build.VERSION_CODES.O, an int literal, or a constant).
	adSdkIntBranchRE = regexp.MustCompile(
		`(?m)Build\.VERSION\.SDK_INT\s*(>=|<=|>|<|==|!=)\s*([A-Za-z0-9_.]+)`)
	// native_module_imports: <uses-permission android:name="android.hardware.*">
	// (or android.permission.*) declarations in AndroidManifest.xml.
	adUsesPermissionRE = regexp.MustCompile(
		`(?m)<uses-permission\b[^>]*android:name\s*=\s*\"([^\"]+)\"`)
	// native_module_imports: <uses-feature android:name="android.hardware.*">
	// declarations in AndroidManifest.xml (hardware/software feature gates).
	adUsesFeatureRE = regexp.MustCompile(
		`(?m)<uses-feature\b[^>]*android:name\s*=\s*\"([^\"]+)\"`)
	// native_module_imports: import android.hardware.* statements in Java source
	// (Camera, Sensor, fingerprint, usb, etc. — the native-device bridge surface).
	adHardwareImportRE = regexp.MustCompile(
		`(?m)^\s*import\s+(android\.hardware\.[A-Za-z0-9_.]+)\s*;`)
)

func androidFrameworkMatches(fw string) bool {
	if androidFrameworks[fw] {
		return true
	}
	return strings.HasPrefix(fw, "android")
}

// ExtractAndroid runs the Android extractor.
func ExtractAndroid(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !androidFrameworkMatches(ctx.Framework) {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	knownActivities := make(map[string]string)
	knownServices := make(map[string]string)
	knownFragments := make(map[string]string)
	knownViewModels := make(map[string]string)

	isManifest := strings.HasSuffix(fp, "AndroidManifest.xml")

	// Manifest parsing
	if isManifest {
		for _, pair := range []struct {
			re         *regexp.Regexp
			entityKind string
			prov       string
			kind       string
			refFn      func(string) string
			known      map[string]string
		}{
			{adManifestActivityRE, "SCOPE.UIComponent", "INFERRED_FROM_ANDROID_MANIFEST", "activity",
				func(n string) string { return "scope:uicomponent:android_activity:" + fp + ":" + n }, knownActivities},
			{adManifestServiceRE, "SCOPE.Service", "INFERRED_FROM_ANDROID_MANIFEST", "service",
				func(n string) string { return "scope:service:android_service:" + fp + ":" + n }, knownServices},
			{adManifestReceiverRE, "SCOPE.Component", "INFERRED_FROM_ANDROID_MANIFEST", "receiver",
				func(n string) string { return "scope:component:android_receiver:" + fp + ":" + n }, nil},
			{adManifestProviderRE, "SCOPE.Component", "INFERRED_FROM_ANDROID_MANIFEST", "provider",
				func(n string) string { return "scope:component:android_provider:" + fp + ":" + n }, nil},
		} {
			for _, m := range pair.re.FindAllStringSubmatchIndex(source, -1) {
				fullName := source[m[2]:m[3]]
				shortName := shortClassName(fullName)
				ref := pair.refFn(shortName)
				if seenRefs[ref] {
					continue
				}
				seenRefs[ref] = true
				if pair.known != nil {
					pair.known[shortName] = ref
				}
				subtype := ""
				if pair.entityKind == "SCOPE.UIComponent" {
					subtype = "component"
				}
				result.Entities = append(result.Entities, SecondaryEntity{
					Name: shortName, Kind: pair.entityKind, Subtype: subtype,
					SourceFile: fp,
					LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
					Provenance: pair.prov, Ref: ref,
					Properties: map[string]any{
						"component_kind": pair.kind, "manifest_name": fullName,
						"framework": "android",
					},
				})
			}
		}
	}

	// Java class declarations
	emitClass := func(re *regexp.Regexp, entityKind, subtype, prov, kind string,
		refFn func(string) string, known map[string]string) {
		for _, m := range re.FindAllStringSubmatchIndex(source, -1) {
			clsName := source[m[2]:m[3]]
			ref := refFn(clsName)
			if seenRefs[ref] {
				continue
			}
			seenRefs[ref] = true
			if known != nil {
				known[clsName] = ref
			}
			result.Entities = append(result.Entities, SecondaryEntity{
				Name: clsName, Kind: entityKind, Subtype: subtype,
				SourceFile: fp,
				LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: prov, Ref: ref,
				Properties: map[string]any{"component_kind": kind, "framework": "android"},
			})
		}
	}

	emitClass(adActivityClassRE, "SCOPE.UIComponent", "component", "INFERRED_FROM_ANDROID_ACTIVITY", "activity",
		func(n string) string { return "scope:uicomponent:android_activity:" + fp + ":" + n }, knownActivities)
	emitClass(adFragmentClassRE, "SCOPE.UIComponent", "component", "INFERRED_FROM_ANDROID_FRAGMENT", "fragment",
		func(n string) string { return "scope:uicomponent:android_fragment:" + fp + ":" + n }, knownFragments)
	emitClass(adServiceClassRE, "SCOPE.Service", "", "INFERRED_FROM_ANDROID_SERVICE", "service",
		func(n string) string { return "scope:service:android_service:" + fp + ":" + n }, knownServices)
	emitClass(adReceiverClassRE, "SCOPE.Component", "", "INFERRED_FROM_ANDROID_RECEIVER", "receiver",
		func(n string) string { return "scope:component:android_receiver:" + fp + ":" + n }, nil)
	emitClass(adProviderClassRE, "SCOPE.Component", "", "INFERRED_FROM_ANDROID_PROVIDER", "provider",
		func(n string) string { return "scope:component:android_provider:" + fp + ":" + n }, nil)
	emitClass(adViewModelClassRE, "SCOPE.Component", "", "INFERRED_FROM_ANDROID_VIEWMODEL", "viewmodel",
		func(n string) string { return "scope:component:android_viewmodel:" + fp + ":" + n }, knownViewModels)

	resolveRef := func(name string) string {
		if r, ok := knownActivities[name]; ok {
			return r
		}
		if r, ok := knownServices[name]; ok {
			return r
		}
		if r, ok := knownFragments[name]; ok {
			return r
		}
		if r, ok := knownViewModels[name]; ok {
			return r
		}
		return "scope:dependency:android:" + fp + ":" + name
	}

	// Intent navigation
	for _, m := range adIntentExplicitRE.FindAllStringSubmatchIndex(source, -1) {
		targetClass := source[m[2]:m[3]]
		sourceClass := findEnclosingClass(source, m[0])
		if sourceClass == "" {
			continue
		}
		intentRef := "scope:operation:android_intent:" + fp + ":" + sourceClass + "->" + targetClass
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: sourceClass + "->" + targetClass, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_ANDROID_INTENT", Ref: intentRef,
			Properties: map[string]any{
				"source_class": sourceClass, "target_class": targetClass,
				"framework": "android",
			},
		}) {
			srcRef := resolveRef(sourceClass)
			tgtRef := resolveRef(targetClass)
			addRel(&result, seenRels, Relationship{
				SourceRef: srcRef, TargetRef: tgtRef, RelationshipType: "DEPENDS_ON",
				Properties: map[string]string{"navigation_kind": "intent", "target_class": targetClass},
			})
		}
	}

	// Fragment transactions
	for _, m := range adFragmentTransactionRE.FindAllStringSubmatchIndex(source, -1) {
		fragmentClass := source[m[2]:m[3]]
		hostClass := findEnclosingClass(source, m[0])
		if hostClass == "" {
			continue
		}
		txRef := "scope:operation:android_fragment_tx:" + fp + ":" + hostClass + "->" + fragmentClass
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: hostClass + "::" + fragmentClass, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_ANDROID_FRAGMENT_TRANSACTION", Ref: txRef,
			Properties: map[string]any{
				"host_class": hostClass, "fragment_class": fragmentClass,
				"framework": "android",
			},
		}) {
			hostRef := resolveRef(hostClass)
			addRel(&result, seenRels, Relationship{
				SourceRef: hostRef, TargetRef: txRef, RelationshipType: "OWNS",
			})
			fragRef := resolveRef(fragmentClass)
			addRel(&result, seenRels, Relationship{
				SourceRef: hostRef, TargetRef: fragRef, RelationshipType: "DEPENDS_ON",
				Properties: map[string]string{"navigation_kind": "fragment_transaction"},
			})
		}
	}

	// ViewModel provider
	for _, m := range adViewModelProviderRE.FindAllStringSubmatchIndex(source, -1) {
		vmClass := source[m[2]:m[3]]
		hostClass := findEnclosingClass(source, m[0])
		if hostClass == "" {
			continue
		}
		hostRef := resolveRef(hostClass)
		vmRef := resolveRef(vmClass)
		addRel(&result, seenRels, Relationship{
			SourceRef: hostRef, TargetRef: vmRef, RelationshipType: "DEPENDS_ON",
			Properties: map[string]string{"dependency_kind": "viewmodel", "viewmodel_class": vmClass},
		})
	}

	// platform_branching: Build.VERSION.SDK_INT API-level comparisons.
	// Each comparison site becomes a branch operation owned by the enclosing
	// class, mirroring the JS mobile platform_branching capability for Android.
	for _, m := range adSdkIntBranchRE.FindAllStringSubmatchIndex(source, -1) {
		op := source[m[2]:m[3]]
		apiLevel := source[m[4]:m[5]]
		enclosing := findEnclosingClass(source, m[0])
		expr := "Build.VERSION.SDK_INT " + op + " " + apiLevel
		branchRef := "scope:operation:android_platform_branch:" + fp + ":" +
			expr + ":" + lineToStr(source, m[0])
		props := map[string]any{
			"branch_kind": "sdk_int", "operator": op,
			"api_level": apiLevel, "expression": expr,
			"framework": "android",
		}
		if enclosing != "" {
			props["enclosing_class"] = enclosing
		}
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: expr, Kind: "SCOPE.Operation", Subtype: "branch",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_ANDROID_SDK_INT_BRANCH", Ref: branchRef,
			Properties: props,
		}) && enclosing != "" {
			addRel(&result, seenRels, Relationship{
				SourceRef: resolveRef(enclosing), TargetRef: branchRef,
				RelationshipType: "OWNS",
				Properties:       map[string]string{"branch_kind": "platform_sdk_int"},
			})
		}
	}

	// native_module_imports: AndroidManifest <uses-permission> / <uses-feature>
	// hardware declarations + `import android.hardware.*` statements. These
	// surface the native-device bridge surface (camera, sensors, usb, biometrics).
	emitNativeModule := func(name, declKind, prov string, line int) {
		ref := "scope:reference:android_native_module:" + fp + ":" + name
		props := map[string]any{
			"module_name": name, "declaration_kind": declKind,
			"framework": "android",
		}
		if i := strings.LastIndex(name, "."); i >= 0 {
			props["category"] = name[:i]
		}
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Reference", Subtype: "native_module",
			SourceFile: fp, LineStart: line, LineEnd: line,
			Provenance: prov, Ref: ref, Properties: props,
		})
	}

	if isManifest {
		for _, m := range adUsesPermissionRE.FindAllStringSubmatchIndex(source, -1) {
			name := source[m[2]:m[3]]
			if !strings.HasPrefix(name, "android.hardware.") {
				continue
			}
			emitNativeModule(name, "uses-permission",
				"INFERRED_FROM_ANDROID_USES_PERMISSION", lineOf(source, m[0]))
		}
		for _, m := range adUsesFeatureRE.FindAllStringSubmatchIndex(source, -1) {
			name := source[m[2]:m[3]]
			if !strings.HasPrefix(name, "android.hardware.") {
				continue
			}
			emitNativeModule(name, "uses-feature",
				"INFERRED_FROM_ANDROID_USES_FEATURE", lineOf(source, m[0]))
		}
	} else {
		for _, m := range adHardwareImportRE.FindAllStringSubmatchIndex(source, -1) {
			name := source[m[2]:m[3]]
			emitNativeModule(name, "import",
				"INFERRED_FROM_ANDROID_HARDWARE_IMPORT", lineOf(source, m[0]))
		}
	}

	return result
}

// lineToStr returns the 1-indexed line number for offset as a string, for use
// in synthetic entity refs that must stay unique across like-named branches.
func lineToStr(source string, offset int) string {
	return strconv.Itoa(lineOf(source, offset))
}

func shortClassName(fullName string) string {
	if idx := strings.LastIndex(fullName, "."); idx >= 0 {
		return fullName[idx+1:]
	}
	return fullName
}
