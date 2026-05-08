package java

import (
	"regexp"
	"strings"
)

// Android Java custom extractor: Manifest, Intent, Fragments, ViewModel/LiveData.
// Ported from: android_extractor.py

var androidFrameworks = map[string]bool{
	"android_sdk": true, "android": true, "android_jetpack": true,
	"android_jetpack_compose": true,
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
			re       *regexp.Regexp
			veraType string
			prov     string
			kind     string
			refFn    func(string) string
			known    map[string]string
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
				if pair.veraType == "SCOPE.UIComponent" {
					subtype = "component"
				}
				result.Entities = append(result.Entities, SecondaryEntity{
					Name: shortName, Kind: pair.veraType, Subtype: subtype,
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
	emitClass := func(re *regexp.Regexp, veraType, subtype, prov, kind string,
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
				Name: clsName, Kind: veraType, Subtype: subtype,
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

	return result
}

func shortClassName(fullName string) string {
	if idx := strings.LastIndex(fullName, "."); idx >= 0 {
		return fullName[idx+1:]
	}
	return fullName
}
