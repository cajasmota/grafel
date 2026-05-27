// Fixture for #2709 — object-subscript static enumeration in
// template-literal HTTP paths.
//
// The path interpolation `${TYPES[t]}` references a same-file `const TYPES`
// object literal whose values are all string literals. The extractor must
// enumerate one endpoint per known value, tagged with the source subscript
// expression in the `polymorphic_subscript` property.

import apiClient from "./client";

const TYPES = {
    a: "alpha",
    b: "beta",
};

// Dynamic-key subscript: `t` is a function parameter. Enumeration applies.
export async function listByType(t) {
    return apiClient.get(`/${TYPES[t]}/x`);
}

// Static-key subscript: `'a'` is a string literal. Direct substitution.
// Used to guard against regressions in the literal-key branch.
export async function listAlpha() {
    return apiClient.get(`/${TYPES["a"]}/y`);
}
