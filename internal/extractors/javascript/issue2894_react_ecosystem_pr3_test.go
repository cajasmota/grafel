// issue2894_react_ecosystem_pr3_test.go — issue #2894 PR3 / #2909 proving tests.
//
// Proves the form_library_extraction framework_specific["React Ecosystem"] cell
// against testdata/react_ecosystem/Forms.tsx. Each assertion is the proving
// artifact for the coverage cell:
//   - React Hook Form  → form_library=react_hook_form, form_hooks, form_resolver
//                        (zod/yup), validation_schema (resolver schema ident),
//                        form_fields (register('name') / <Controller name>).
//   - Formik           → form_library=formik, form_components (Formik/Field/...),
//                        validation_schema, form_fields (<Field name>).
package javascript_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestIssue2894PR3_FormLibraryExtraction(t *testing.T) {
	ents := extractTSXFixture(t, "react_ecosystem/Forms.tsx")

	prop := func(e *types.EntityRecord, k string) string {
		if e == nil || e.Properties == nil {
			return ""
		}
		return e.Properties[k]
	}
	mustForm := func(name, wantLib string) *types.EntityRecord {
		e := findByName(ents, name)
		if e == nil {
			t.Fatalf("%s not extracted; names: %v", name, entityNames(ents))
		}
		if got := prop(e, "form_library"); got != wantLib {
			t.Errorf("%s form_library=%q, want %q; props=%v", name, got, wantLib, e.Properties)
		}
		return e
	}

	// React Hook Form component with zod resolver.
	login := mustForm("LoginForm", "react_hook_form")
	if got := prop(login, "form_resolver"); got != "zod" {
		t.Errorf("LoginForm form_resolver=%q, want zod", got)
	}
	if got := prop(login, "validation_schema"); got != "loginSchema" {
		t.Errorf("LoginForm validation_schema=%q, want loginSchema", got)
	}
	// register('email'), register('password'), <Controller name="remember">.
	for _, f := range []string{"email", "password", "remember"} {
		if !containsCSV(prop(login, "form_fields"), f) {
			t.Errorf("LoginForm form_fields=%q missing %q", prop(login, "form_fields"), f)
		}
	}
	if got := prop(login, "form_field_count"); got != "3" {
		t.Errorf("LoginForm form_field_count=%q, want 3", got)
	}

	// React Hook Form custom hook with yup resolver.
	profile := mustForm("useProfileForm", "react_hook_form")
	if got := prop(profile, "form_resolver"); got != "yup" {
		t.Errorf("useProfileForm form_resolver=%q, want yup", got)
	}
	if got := prop(profile, "validation_schema"); got != "signupSchema" {
		t.Errorf("useProfileForm validation_schema=%q, want signupSchema", got)
	}
	if !containsCSV(prop(profile, "form_hooks"), "useFieldArray") {
		t.Errorf("useProfileForm form_hooks=%q missing useFieldArray", prop(profile, "form_hooks"))
	}

	// Context consumer (no useForm, only useFormContext + register).
	addr := mustForm("AddressFields", "react_hook_form")
	if !containsCSV(prop(addr, "form_hooks"), "useFormContext") {
		t.Errorf("AddressFields form_hooks=%q missing useFormContext", prop(addr, "form_hooks"))
	}
	if !containsCSV(prop(addr, "form_fields"), "city") {
		t.Errorf("AddressFields form_fields=%q missing city", prop(addr, "form_fields"))
	}

	// Formik render-prop component.
	signup := mustForm("SignupForm", "formik")
	if got := prop(signup, "validation_schema"); got != "signupSchema" {
		t.Errorf("SignupForm validation_schema=%q, want signupSchema", got)
	}
	for _, c := range []string{"Formik", "Form", "Field", "FieldArray"} {
		if !containsCSV(prop(signup, "form_components"), c) {
			t.Errorf("SignupForm form_components=%q missing %q", prop(signup, "form_components"), c)
		}
	}
	for _, f := range []string{"name", "email", "phones"} {
		if !containsCSV(prop(signup, "form_fields"), f) {
			t.Errorf("SignupForm form_fields=%q missing %q", prop(signup, "form_fields"), f)
		}
	}

	// Formik hook-style form.
	contact := mustForm("ContactForm", "formik")
	if !containsCSV(prop(contact, "form_hooks"), "useFormik") {
		t.Errorf("ContactForm form_hooks=%q missing useFormik", prop(contact, "form_hooks"))
	}
	if got := prop(contact, "validation_schema"); got != "contactSchema" {
		t.Errorf("ContactForm validation_schema=%q, want contactSchema", got)
	}

	// The hook calls also surface as USES_HOOK via the generic pass.
	wantUses := func(e *types.EntityRecord, hook string) {
		for _, r := range e.Relationships {
			if r.Kind == "USES_HOOK" && r.ToID == hook {
				return
			}
		}
		t.Errorf("%s missing USES_HOOK->%s; rels=%v", e.Name, hook, e.Relationships)
	}
	wantUses(login, "useForm")
	wantUses(contact, "useFormik")
}

// containsCSV reports whether comma-separated list s contains element e.
func containsCSV(s, e string) bool {
	for _, part := range splitCSV(s) {
		if part == e {
			return true
		}
	}
	return false
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	out = append(out, cur)
	return out
}
