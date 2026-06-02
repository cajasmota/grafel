package baseknowledge

// drf.go — Django REST Framework + Django generic class-based view
// knowledge pack.
//
// This is the reference pack for the catalog. It ports the name-only
// `python.cbvBaseInheritedMethods` map into the typed model and ADDS the
// per-verb default contract facts (default status, error statuses,
// behaviour, pagination / permission applicability, defining mixin) that
// MRO resolution (#3833) and effective-contract synthesis (#3835) need.
//
// Sources: DRF docs (generic viewsets, generic API views, mixins) and the
// DRF source. Default statuses are the framework defaults:
//   - create          -> 201 (mixins.CreateModelMixin.create)
//   - update           -> 200 (mixins.UpdateModelMixin.update)
//   - partial_update   -> 200 (mixins.UpdateModelMixin.partial_update)
//   - destroy          -> 204 (mixins.DestroyModelMixin.destroy)
//   - list             -> 200 (mixins.ListModelMixin.list, paginated)
//   - retrieve         -> 200 (mixins.RetrieveModelMixin.retrieve)
//
// The #278 fact: CreateModelMixin.create and UpdateModelMixin.update both
// call `serializer.is_valid(raise_exception=True)`, so an invalid payload
// yields HTTP 400. We record that as ErrorStatuses=[400] + Behaviour text
// so downstream contract synthesis surfaces the implicit 400 the user's
// code never declares.

// DRF FQN roots for the catalogued classes.
const (
	drfMixinsRoot   = "rest_framework.mixins."
	drfViewSetsRoot = "rest_framework.viewsets."
	drfGenericsRoot = "rest_framework.generics."
	drfViewsRoot    = "rest_framework.views."
	djangoCBVRoot   = "django.views.generic."
)

// drfVerb builds a route-handler Member with the standard DRF
// per-verb defaults (permission always applies; pagination only when
// asked). definingFQN is the mixin/base that owns the body.
func drfVerb(name, verb string, status int, definingFQN string, paginated bool, errs []int, behaviour string) Member {
	return Member{
		Name:                 name,
		DefiningClass:        definingFQN,
		HTTPVerb:             verb,
		DefaultStatus:        status,
		ErrorStatuses:        errs,
		Behaviour:            behaviour,
		PaginationApplicable: paginated,
		PermissionApplicable: true,
	}
}

// The five canonical CRUD verb contracts, each attributed to its defining
// mixin. These are the building blocks composed into the viewset bases.
var (
	drfCreate = drfVerb(
		"create", "POST", 201, drfMixinsRoot+"CreateModelMixin", false,
		[]int{400},
		"serializer.is_valid(raise_exception=True) -> 400 on invalid payload; returns 201 with the created representation (#278)",
	)
	drfRetrieve = drfVerb(
		"retrieve", "GET", 200, drfMixinsRoot+"RetrieveModelMixin", false,
		nil,
		"returns the serialized instance; 404 when the lookup misses",
	)
	drfUpdate = drfVerb(
		"update", "PUT", 200, drfMixinsRoot+"UpdateModelMixin", false,
		[]int{400},
		"serializer.is_valid(raise_exception=True) -> 400 on invalid payload; returns 200 with the updated representation (#278)",
	)
	drfPartialUpdate = drfVerb(
		"partial_update", "PATCH", 200, drfMixinsRoot+"UpdateModelMixin", false,
		[]int{400},
		"calls update(partial=True); is_valid(raise_exception=True) -> 400 on invalid payload; returns 200 (#278)",
	)
	drfDestroy = drfVerb(
		"destroy", "DELETE", 204, drfMixinsRoot+"DestroyModelMixin", false,
		nil,
		"deletes the instance and returns 204 No Content with an empty body",
	)
	drfList = drfVerb(
		"list", "GET", 200, drfMixinsRoot+"ListModelMixin", true,
		nil,
		"returns the (optionally paginated) serialized queryset; pagination applies when DEFAULT_PAGINATION_CLASS is set",
	)
)

// members builds a name->Member map from the given verb contracts.
func members(ms ...Member) map[string]Member {
	out := make(map[string]Member, len(ms))
	for _, m := range ms {
		out[m.Name] = m
	}
	return out
}

// drfPack is the DRF + Django-CBV knowledge pack.
type drfPack struct{}

func (drfPack) Framework() string { return "drf" }

func (drfPack) Contracts() []BaseClassContract {
	py := func(fwk string, fqns []string, m map[string]Member) BaseClassContract {
		return BaseClassContract{FQNs: fqns, Language: "python", Framework: fwk, Members: m}
	}

	return []BaseClassContract{
		// --- DRF mixins (rest_framework.mixins.*) ------------------------
		py("drf", []string{drfMixinsRoot + "CreateModelMixin", "CreateModelMixin"}, members(drfCreate)),
		py("drf", []string{drfMixinsRoot + "RetrieveModelMixin", "RetrieveModelMixin"}, members(drfRetrieve)),
		py("drf", []string{drfMixinsRoot + "UpdateModelMixin", "UpdateModelMixin"}, members(drfUpdate, drfPartialUpdate)),
		py("drf", []string{drfMixinsRoot + "DestroyModelMixin", "DestroyModelMixin"}, members(drfDestroy)),
		py("drf", []string{drfMixinsRoot + "ListModelMixin", "ListModelMixin"}, members(drfList)),

		// --- DRF viewsets (rest_framework.viewsets.*) --------------------
		// ViewSet / GenericViewSet contribute no CRUD verbs on their own
		// (action-only / mixin host) — recorded with empty member sets so
		// they are still "known" bases (KnownBases / cbv_bases parity).
		py("drf", []string{drfViewSetsRoot + "ViewSet", "ViewSet"}, members()),
		py("drf", []string{drfViewSetsRoot + "GenericViewSet", "GenericViewSet"}, members()),
		py("drf", []string{drfViewSetsRoot + "ReadOnlyModelViewSet", "ReadOnlyModelViewSet"},
			members(drfList, drfRetrieve)),
		py("drf", []string{drfViewSetsRoot + "ModelViewSet", "ModelViewSet"},
			members(drfList, drfRetrieve, drfCreate, drfUpdate, drfPartialUpdate, drfDestroy)),

		// --- DRF generic API views (rest_framework.generics.*) -----------
		py("drf", []string{drfGenericsRoot + "GenericAPIView", "GenericAPIView"}, members()),
		py("drf", []string{drfGenericsRoot + "CreateAPIView", "CreateAPIView"}, httpMembers("post")),
		py("drf", []string{drfGenericsRoot + "ListAPIView", "ListAPIView"}, httpMembers("get")),
		py("drf", []string{drfGenericsRoot + "RetrieveAPIView", "RetrieveAPIView"}, httpMembers("get")),
		py("drf", []string{drfGenericsRoot + "DestroyAPIView", "DestroyAPIView"}, httpMembers("delete")),
		py("drf", []string{drfGenericsRoot + "UpdateAPIView", "UpdateAPIView"}, httpMembers("put", "patch")),
		py("drf", []string{drfGenericsRoot + "ListCreateAPIView", "ListCreateAPIView"}, httpMembers("get", "post")),
		py("drf", []string{drfGenericsRoot + "RetrieveUpdateAPIView", "RetrieveUpdateAPIView"}, httpMembers("get", "put", "patch")),
		py("drf", []string{drfGenericsRoot + "RetrieveDestroyAPIView", "RetrieveDestroyAPIView"}, httpMembers("get", "delete")),
		py("drf", []string{drfGenericsRoot + "RetrieveUpdateDestroyAPIView", "RetrieveUpdateDestroyAPIView"}, httpMembers("get", "put", "patch", "delete")),

		// --- Django generic class-based views (django.views.generic.*) ---
		// HTTP-verb method handlers. These are name/verb only — Django sets
		// no single framework-default success status (the view renders a
		// template or redirects), so DefaultStatus stays StatusUnknown.
		py("django", []string{djangoCBVRoot + "View", "View"}, httpMembers("get", "post", "put", "patch", "delete", "head", "options", "trace")),
		py("django", []string{djangoCBVRoot + "TemplateView", "TemplateView"}, httpMembers("get")),
		py("django", []string{djangoCBVRoot + "RedirectView", "RedirectView"}, httpMembers("get", "post", "put", "patch", "delete", "head", "options")),
		py("django", []string{djangoCBVRoot + "ListView", "ListView"}, httpMembers("get")),
		py("django", []string{djangoCBVRoot + "DetailView", "DetailView"}, httpMembers("get")),
		py("django", []string{djangoCBVRoot + "FormView", "FormView"}, httpMembers("get", "post")),
		py("django", []string{djangoCBVRoot + "CreateView", "CreateView"}, httpMembers("get", "post")),
		py("django", []string{djangoCBVRoot + "UpdateView", "UpdateView"}, httpMembers("get", "post")),
		py("django", []string{djangoCBVRoot + "DeleteView", "DeleteView"}, httpMembers("get", "post", "delete")),
		py("django", []string{djangoCBVRoot + "ArchiveIndexView", "ArchiveIndexView"}, httpMembers("get")),
		py("django", []string{djangoCBVRoot + "YearArchiveView", "YearArchiveView"}, httpMembers("get")),
		py("django", []string{djangoCBVRoot + "MonthArchiveView", "MonthArchiveView"}, httpMembers("get")),
		py("django", []string{djangoCBVRoot + "DayArchiveView", "DayArchiveView"}, httpMembers("get")),
		py("django", []string{djangoCBVRoot + "WeekArchiveView", "WeekArchiveView"}, httpMembers("get")),
		py("django", []string{djangoCBVRoot + "TodayArchiveView", "TodayArchiveView"}, httpMembers("get")),
		py("django", []string{djangoCBVRoot + "DateDetailView", "DateDetailView"}, httpMembers("get")),
		py("django", []string{djangoCBVRoot + "ProcessFormView", "ProcessFormView"}, httpMembers("get", "post")),
		py("django", []string{djangoCBVRoot + "BaseCreateView", "BaseCreateView"}, httpMembers("get", "post")),
		py("django", []string{djangoCBVRoot + "BaseUpdateView", "BaseUpdateView"}, httpMembers("get", "post")),
		py("django", []string{djangoCBVRoot + "BaseDeleteView", "BaseDeleteView"}, httpMembers("get", "post", "delete")),
	}
}

// httpMembers builds a member map for bare HTTP-verb handler methods
// ("get", "post", ...) with no curated default status — used for the
// Django generic CBVs and DRF generic API views whose verbs map 1:1 to a
// lower-case method name and whose success status is not a single
// framework constant.
func httpMembers(verbs ...string) map[string]Member {
	out := make(map[string]Member, len(verbs))
	for _, v := range verbs {
		out[v] = Member{
			Name:                 v,
			HTTPVerb:             httpVerbUpper(v),
			DefaultStatus:        StatusUnknown,
			PermissionApplicable: true,
		}
	}
	return out
}

func httpVerbUpper(v string) string {
	switch v {
	case "get", "post", "put", "patch", "delete", "head", "options", "trace":
		return upper(v)
	default:
		return upper(v)
	}
}

func upper(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}

func init() { Register(drfPack{}) }
