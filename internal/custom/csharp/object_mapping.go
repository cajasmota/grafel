package csharp

// object_mapping.go — AutoMapper / Mapster object-mapping topology extractor
// (#5074, spun out of #5016/#4969). Surfaces DTO↔entity mapping topology so a
// configured object-mapping is a traversable subgraph rather than being buried
// inside a mapper-configuration class.
//
// AutoMapper — mapping is declared inside a `Profile` subclass via
// `CreateMap<TSource, TDest>()`, optionally refined by chained `.ForMember(...)`
// member maps:
//
//	public class UserProfile : Profile {
//	    public UserProfile() {
//	        CreateMap<User, UserDto>()
//	            .ForMember(d => d.FullName, o => o.MapFrom(s => s.Name));
//	        CreateMap<Order, OrderDto>().ReverseMap();
//	    }
//	}
//
// Mapster — global/typed configuration registrations and inline projections:
//
//	TypeAdapterConfig<User, UserDto>.NewConfig();
//	config.NewConfig<Order, OrderDto>();
//	var dto = user.Adapt<UserDto>();          // inline projection (dest only)
//
// Emission model (reuses existing entity Kinds — no new Kind):
//   - one SCOPE.Pattern subtype="mapping_profile" per AutoMapper Profile class
//     (the owning configuration unit).
//   - one SCOPE.Pattern subtype="object_mapping" per CreateMap / NewConfig pair,
//     carrying a MAPS_TO relationship from the source type (`Class:<TSrc>`) to
//     the destination type (`Class:<TDest>`) so source→dest is resolvable to the
//     real DTO/entity class entities by the linker's byName fallback.
//   - one SCOPE.Pattern subtype="object_mapping" per `.Adapt<TDest>()` inline
//     projection (destination-only — the source is a runtime expression, so we
//     emit honest-partial: no fabricated source edge).
//
// MAPS_TO edge properties: framework, source_type, dest_type, member_map_count
// (AutoMapper .ForMember count), reverse ("true" when .ReverseMap() present),
// owning profile (AutoMapper), provenance.
//
// Honest-partial: open generics, runtime-typed sources (`.Adapt<T>()`) and
// fully-dynamic registrations carry no fabricated MAPS_TO source.

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_csharp_object_mapping", &objectMappingExtractor{})
}

type objectMappingExtractor struct{}

func (e *objectMappingExtractor) Language() string { return "custom_csharp_object_mapping" }

var (
	// AutoMapper Profile subclass: `class X : Profile` (also `: Profile,` / `: Profile {`).
	csProfileRe = regexp.MustCompile(`\bclass\s+(\w+)\s*:\s*[^\{]*\bProfile\b`)

	// AutoMapper CreateMap<TSource, TDest>() — two explicit type args.
	csCreateMapRe = regexp.MustCompile(`\bCreateMap\s*<\s*([\w.]+)\s*,\s*([\w.]+)\s*>\s*\(`)

	// Mapster TypeAdapterConfig<TSource, TDest>.NewConfig() and
	// `someConfig.NewConfig<TSource, TDest>()` — both supply explicit src,dest.
	csTypeAdapterCfgRe = regexp.MustCompile(`\bTypeAdapterConfig\s*<\s*([\w.]+)\s*,\s*([\w.]+)\s*>`)
	csNewConfigRe      = regexp.MustCompile(`\.NewConfig\s*<\s*([\w.]+)\s*,\s*([\w.]+)\s*>\s*\(`)

	// Mapster inline projection `expr.Adapt<TDest>()` — destination only.
	csAdaptRe = regexp.MustCompile(`\.Adapt\s*<\s*([\w.]+)\s*>\s*\(`)
)

// csChainWindow returns the source slice of the fluent call chain starting at
// `start`, terminated by the first top-level `;` (depth-aware over parens so a
// `MapFrom(s => s.X)` lambda's own `;`-free body is included). The fluent
// chain may span multiple lines, so only a depth-0 `;` terminates it.
func csChainWindow(src string, start int) string {
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 {
				return src[start:i]
			}
		}
	}
	return src[start:]
}

// csShortType returns the trailing identifier of a possibly-dotted type name
// (`App.Models.User` -> `User`) for the byName-resolvable `Class:<T>` ref.
func csShortType(t string) string {
	if i := strings.LastIndexByte(t, '.'); i >= 0 {
		return t[i+1:]
	}
	return t
}

func csMapsToEdge(srcType, destType, framework, profile string, memberMaps int, reverse bool) types.RelationshipRecord {
	props := types.Props{
		{K: "dest_type", V: destType},
		{K: "framework", V: framework},
		{K: "language", V: "csharp"},
		{K: "provenance", V: "INFERRED_FROM_OBJECT_MAPPING"},
		{K: "source_type", V: srcType},
	}
	if memberMaps > 0 {
		props.Set("member_map_count", fmt.Sprintf("%d", memberMaps))
	}
	if reverse {
		props.Set("reverse", "true")
	}
	if profile != "" {
		props.Set("profile", profile)
	}
	return types.RelationshipRecord{
		FromID:     "Class:" + csShortType(srcType),
		ToID:       "Class:" + csShortType(destType),
		Kind:       string(types.RelationshipKindMapsTo),
		Properties: props,
	}
}

func (e *objectMappingExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.csharp_object_mapping")
	_, span := tracer.Start(ctx, "custom.csharp_object_mapping")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	// Cheap pre-filter: only run on files that mention a mapping API.
	if !strings.Contains(src, "CreateMap") && !strings.Contains(src, "TypeAdapterConfig") &&
		!strings.Contains(src, "NewConfig") && !strings.Contains(src, ".Adapt<") &&
		!strings.Contains(src, ".Adapt <") {
		return nil, nil
	}

	var out []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, ent)
	}

	// --- AutoMapper Profile classes (the owning configuration unit) ---
	// Record (name, byte-offset) so each CreateMap can be attributed to its
	// enclosing profile by the nearest preceding profile declaration.
	type profileDecl struct {
		name string
		off  int
	}
	var profiles []profileDecl
	for _, m := range csProfileRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		profiles = append(profiles, profileDecl{name: name, off: m[0]})
		ent := makeEntity(name, "SCOPE.Pattern", "mapping_profile", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "automapper",
			"pattern_type", "mapping_profile",
			"profile", name,
			"provenance", "INFERRED_FROM_AUTOMAPPER_PROFILE")
		add(ent)
	}
	profileAt := func(off int) string {
		best := ""
		bestOff := -1
		for _, p := range profiles {
			if p.off < off && p.off > bestOff {
				bestOff = p.off
				best = p.name
			}
		}
		return best
	}

	// --- AutoMapper CreateMap<TSrc, TDest>() (+ chained .ForMember / .ReverseMap) ---
	for _, m := range csCreateMapRe.FindAllStringSubmatchIndex(src, -1) {
		srcType := src[m[2]:m[3]]
		destType := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		window := csChainWindow(src, m[0])
		memberMaps := strings.Count(window, ".ForMember")
		reverse := strings.Contains(window, ".ReverseMap")
		profile := profileAt(m[0])

		name := srcType + " -> " + destType
		ent := makeEntity(name, "SCOPE.Pattern", "object_mapping", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "automapper",
			"pattern_type", "object_mapping",
			"source_type", srcType,
			"dest_type", destType,
			"member_map_count", fmt.Sprintf("%d", memberMaps),
			"provenance", "INFERRED_FROM_AUTOMAPPER_CREATEMAP")
		if reverse {
			setProps(&ent, "reverse", "true")
		}
		if profile != "" {
			setProps(&ent, "profile", profile)
		}
		ent.Relationships = append(ent.Relationships,
			csMapsToEdge(srcType, destType, "automapper", profile, memberMaps, reverse))
		add(ent)

		// .ReverseMap() declares the inverse mapping too — emit its MAPS_TO so
		// the dest→source direction is also traversable (no separate entity to
		// avoid a duplicate Pattern node; the reverse edge hangs off the same).
		if reverse {
			ent2 := &out[len(out)-1]
			ent2.Relationships = append(ent2.Relationships,
				csMapsToEdge(destType, srcType, "automapper", profile, 0, false))
		}
	}

	// --- Mapster TypeAdapterConfig<TSrc, TDest>... ---
	for _, m := range csTypeAdapterCfgRe.FindAllStringSubmatchIndex(src, -1) {
		srcType := src[m[2]:m[3]]
		destType := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		name := srcType + " -> " + destType
		ent := makeEntity(name, "SCOPE.Pattern", "object_mapping", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "mapster",
			"pattern_type", "object_mapping",
			"source_type", srcType,
			"dest_type", destType,
			"registration", "type_adapter_config",
			"provenance", "INFERRED_FROM_MAPSTER_TYPEADAPTERCONFIG")
		ent.Relationships = append(ent.Relationships,
			csMapsToEdge(srcType, destType, "mapster", "", 0, false))
		add(ent)
	}

	// --- Mapster config.NewConfig<TSrc, TDest>() ---
	for _, m := range csNewConfigRe.FindAllStringSubmatchIndex(src, -1) {
		srcType := src[m[2]:m[3]]
		destType := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		name := srcType + " -> " + destType
		ent := makeEntity(name, "SCOPE.Pattern", "object_mapping", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "mapster",
			"pattern_type", "object_mapping",
			"source_type", srcType,
			"dest_type", destType,
			"registration", "new_config",
			"provenance", "INFERRED_FROM_MAPSTER_NEWCONFIG")
		ent.Relationships = append(ent.Relationships,
			csMapsToEdge(srcType, destType, "mapster", "", 0, false))
		add(ent)
	}

	// --- Mapster inline projection `expr.Adapt<TDest>()` (destination-only) ---
	// Honest-partial: the source is a runtime expression, so we record the
	// destination projection but emit no fabricated MAPS_TO source.
	for _, m := range csAdaptRe.FindAllStringSubmatchIndex(src, -1) {
		destType := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		name := "Adapt -> " + destType
		ent := makeEntity(name, "SCOPE.Pattern", "object_mapping", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "mapster",
			"pattern_type", "object_mapping",
			"dest_type", destType,
			"registration", "adapt_projection",
			"dynamic_source", "true",
			"provenance", "INFERRED_FROM_MAPSTER_ADAPT")
		add(ent)
	}

	return out, nil
}
