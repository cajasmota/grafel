package cpp

// unreal_extractor.go — Unreal Engine C++ extractor.
//
// Covered DSL surfaces (partial — heuristic regex; no AST):
//
//  ipc_extraction:       UE messaging / RPC patterns:
//                        UFUNCTION(Server, Reliable) / UFUNCTION(Client, Reliable)
//                        — RPC declarations.
//                        FMessageEndpoint::Builder / Subscribe<T> — UE message bus.
//                        GameplayMessageSubsystem: BroadcastMessage / RegisterListener.
//                        Delegates: DECLARE_MULTICAST_DELEGATE / DECLARE_DYNAMIC_MULTICAST_DELEGATE.
//
//  native_module_imports: .Build.cs PublicDependencyModuleNames / PrivateDependencyModuleNames
//                         array literals → UE module deps.
//
//  main_renderer_split:  not_applicable — Unreal Engine is a game engine; the
//                        "game thread" vs "render thread" distinction exists but
//                        it is not the same architectural concept as a
//                        main-process / renderer-process split (e.g. Electron).
//
// Status: partial

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_cpp_unreal", &unrealExtractor{})
}

type unrealExtractor struct{}

func (e *unrealExtractor) Language() string { return "custom_cpp_unreal" }

var (
	// Gate: must look like an Unreal file.
	reUnrealGate = regexp.MustCompile(`(?:#\s*include\s+[<"]\w+(?:/\w+)*\.generated\.h|UCLASS\s*\(|UFUNCTION\s*\(|UPROPERTY\s*\(|USTRUCT\s*\(|DECLARE_(?:DYNAMIC_)?MULTICAST_DELEGATE)`)

	// RPC declarations: UFUNCTION(Server, Reliable, ...) / UFUNCTION(Client, Reliable, ...)
	// / UFUNCTION(NetMulticast, ...)
	reUnrealRPC = regexp.MustCompile(
		`UFUNCTION\s*\([^)]*\b(Server|Client|NetMulticast)\b[^)]*\)\s*(?:virtual\s+)?(?:\w[\w\s*&]*\s+)?(\w+)\s*\(`,
	)

	// UE message bus: FMessageEndpoint::Builder("name") or Subscribe<T>()
	reUnrealMsgEndpoint = regexp.MustCompile(
		`FMessageEndpoint\s*::\s*Builder\s*\(\s*"([^"]+)"`,
	)
	reUnrealMsgSubscribe = regexp.MustCompile(
		`\.\s*Handling\s*<([^>]+)>`,
	)

	// Gameplay Message Subsystem
	reUnrealGMSBroadcast = regexp.MustCompile(
		`\bBroadcastMessage\s*<[^>]*>\s*\(\s*(?:FGameplayTag\s*::\s*RequestGameplayTag\s*\(\s*)?"([^"]+)"`,
	)
	reUnrealGMSListen = regexp.MustCompile(
		`\bRegisterListener\s*<([^>]+)>`,
	)

	// Delegates: DECLARE_MULTICAST_DELEGATE_*  / DECLARE_DYNAMIC_MULTICAST_DELEGATE_*
	reUnrealDelegate = regexp.MustCompile(
		`\b(DECLARE_(?:DYNAMIC_)?MULTICAST_DELEGATE(?:_\w+)?)\s*\(\s*(\w+)`,
	)

	// .Build.cs: PublicDependencyModuleNames.AddRange(new string[] { "Engine", "Core", ... })
	reUnrealBuildCsAddRange = regexp.MustCompile(
		`(?:PublicDependencyModuleNames|PrivateDependencyModuleNames)\s*\.\s*AddRange\s*\(\s*new\s+string\s*\[\s*\]\s*\{([^}]+)\}`,
	)
	// Also: PublicDependencyModuleNames.Add("Module")
	reUnrealBuildCsAdd = regexp.MustCompile(
		`(?:PublicDependencyModuleNames|PrivateDependencyModuleNames)\s*\.\s*Add\s*\(\s*"([^"]+)"`,
	)
	// Extract quoted strings from AddRange body
	reUnrealStringLiteral = regexp.MustCompile(`"([A-Za-z][A-Za-z0-9_]*)"`)
)

func (e *unrealExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.unreal_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "unreal-engine"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	isCSharp := strings.HasSuffix(file.Path, ".Build.cs") || file.Language == "csharp"
	isCPP := file.Language == "cpp" || file.Language == "c"

	if !isCPP && !isCSharp {
		return nil, nil
	}

	// For C++ files: gate on Unreal markers.
	if isCPP && !reUnrealGate.MatchString(src) {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	if isCPP {
		// --- ipc_extraction ---

		// RPC declarations
		for _, m := range reUnrealRPC.FindAllStringSubmatchIndex(src, -1) {
			rpcKind := src[m[2]:m[3]]
			funcName := src[m[4]:m[5]]
			name := "rpc:" + strings.ToLower(rpcKind) + ":" + funcName
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "unreal-engine", "provenance", "INFERRED_FROM_UNREAL_RPC",
				"ipc_kind", "rpc", "rpc_type", rpcKind, "function_name", funcName)
			add(ent)
		}

		// Message bus endpoint
		for _, m := range reUnrealMsgEndpoint.FindAllStringSubmatchIndex(src, -1) {
			endpointName := src[m[2]:m[3]]
			name := "msg_endpoint:" + endpointName
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "unreal-engine", "provenance", "INFERRED_FROM_UNREAL_MSG_BUS",
				"ipc_kind", "message_bus_endpoint", "endpoint_name", endpointName)
			add(ent)
		}
		for _, m := range reUnrealMsgSubscribe.FindAllStringSubmatchIndex(src, -1) {
			msgType := strings.TrimSpace(src[m[2]:m[3]])
			name := "msg_subscribe:" + msgType
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "unreal-engine", "provenance", "INFERRED_FROM_UNREAL_MSG_BUS",
				"ipc_kind", "message_bus_subscribe", "message_type", msgType)
			add(ent)
		}

		// Gameplay message subsystem
		for _, m := range reUnrealGMSBroadcast.FindAllStringSubmatchIndex(src, -1) {
			channel := src[m[2]:m[3]]
			name := "gms_broadcast:" + channel
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "unreal-engine", "provenance", "INFERRED_FROM_UNREAL_GMS",
				"ipc_kind", "gameplay_message_broadcast", "channel", channel)
			add(ent)
		}
		for _, m := range reUnrealGMSListen.FindAllStringSubmatchIndex(src, -1) {
			msgType := strings.TrimSpace(src[m[2]:m[3]])
			name := "gms_listen:" + msgType
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "unreal-engine", "provenance", "INFERRED_FROM_UNREAL_GMS",
				"ipc_kind", "gameplay_message_listener", "message_type", msgType)
			add(ent)
		}

		// Delegates as IPC/messaging mechanism
		for _, m := range reUnrealDelegate.FindAllStringSubmatchIndex(src, -1) {
			macro := src[m[2]:m[3]]
			delegateName := src[m[4]:m[5]]
			name := "delegate:" + delegateName
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "unreal-engine", "provenance", "INFERRED_FROM_UNREAL_DELEGATE",
				"ipc_kind", "multicast_delegate", "macro", macro, "delegate_name", delegateName)
			add(ent)
		}
	}

	if isCSharp {
		// --- native_module_imports: .Build.cs module deps ---
		seenMod := map[string]bool{}

		// AddRange with array literal
		for _, m := range reUnrealBuildCsAddRange.FindAllStringSubmatchIndex(src, -1) {
			body := src[m[2]:m[3]]
			for _, sm := range reUnrealStringLiteral.FindAllStringSubmatchIndex(body, -1) {
				modName := body[sm[2]:sm[3]]
				if seenMod[modName] {
					continue
				}
				seenMod[modName] = true
				name := "ue_module:" + modName
				ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
				setProps(&ent, "framework", "unreal-engine", "provenance", "INFERRED_FROM_UNREAL_BUILD_CS",
					"module_kind", "native_import", "module_name", modName)
				add(ent)
			}
		}

		// Add() single module
		for _, m := range reUnrealBuildCsAdd.FindAllStringSubmatchIndex(src, -1) {
			modName := src[m[2]:m[3]]
			if seenMod[modName] {
				continue
			}
			seenMod[modName] = true
			name := "ue_module:" + modName
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "unreal-engine", "provenance", "INFERRED_FROM_UNREAL_BUILD_CS",
				"module_kind", "native_import", "module_name", modName)
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
