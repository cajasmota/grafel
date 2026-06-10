package javascript

import "testing"

// Issue #4488 — unit coverage for nestResolveResponseType: Promise/Observable
// async wrappers, envelope wrappers, arrays, inline `{ data: T }`, unions, and
// genuine void/no-content. Framework-agnostic unwrap rules.
func TestNestResolveResponseType(t *testing.T) {
	cases := []struct {
		raw     string
		dto     string
		isArray bool
		isVoid  bool
	}{
		// Plain + async wrappers.
		{"InspectorDto", "InspectorDto", false, false},
		{"Promise<InspectorDto>", "InspectorDto", false, false},
		{"Observable<InspectorDto>", "InspectorDto", false, false},
		{"Promise<Observable<InspectorDto>>", "InspectorDto", false, false},
		// Arrays (flag set, element resolved).
		{"Promise<InspectorDto[]>", "InspectorDto", true, false},
		{"Promise<Array<InspectorDto>>", "InspectorDto", true, false},
		{"InspectorDto[]", "InspectorDto", true, false},
		// Envelopes unwrap to payload.
		{"Promise<ApiResponse<UserDto>>", "UserDto", false, false},
		{"Promise<PagedResponse<GroupResponse>>", "GroupResponse", false, false},
		{"Promise<PaginatedResponse<UserDto>>", "UserDto", false, false},
		{"Promise<Page<EquipmentType>>", "EquipmentType", false, false},
		{"ApiResponse<UserDto[]>", "UserDto", true, false},
		// Inline data-envelope.
		{"Promise<{ data: UserDto }>", "UserDto", false, false},
		{"{ data: UserDto[] }", "UserDto", true, false},
		{"Promise<{ data: UserDto; meta: PageMeta }>", "UserDto", false, false},
		// Unions with null/undefined.
		{"Promise<UserDto | null>", "UserDto", false, false},
		// Genuine void / no-content.
		{"Promise<void>", "", false, true},
		{"void", "", false, true},
		{"Promise<undefined>", "", false, true},
		{"never", "", false, true},
		// Typed primitives — not a DTO, not void.
		{"Promise<number>", "", false, false},
		{"Promise<string>", "", false, false},
		{"Promise<boolean>", "", false, false},
	}
	for _, c := range cases {
		dto, isArray, isVoid := nestResolveResponseType(c.raw)
		if dto != c.dto || isArray != c.isArray || isVoid != c.isVoid {
			t.Errorf("nestResolveResponseType(%q) = (%q,%v,%v); want (%q,%v,%v)",
				c.raw, dto, isArray, isVoid, c.dto, c.isArray, c.isVoid)
		}
	}
}
