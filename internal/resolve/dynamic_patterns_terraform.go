package resolve

import "regexp"

// terraformDynamicPatterns are per-language patterns for Terraform (.tf / .tfvars).
// Registered via init() into dynamicPatternsByLang under the "terraform" key.
//
// Terraform resolution strategy:
//
//  1. The HCL extractor emits same-file structural-refs for var.*, local.*,
//     module.*, data.*.*, output.*, and provider.* that are bound by byLocation
//     without ever reaching this catalog.
//
//  2. Cross-file refs inside a multi-file module (e.g. main.tf references
//     var.region declared in variables.tf) fall through here and are correctly
//     tagged Dynamic — the resolver cannot statically cross file boundaries in
//     a single-pass walk.
//
//  3. Terraform built-in functions, meta-arguments, iteration symbols, and
//     provider-prefixed resource refs that miss the same-file bind are also
//     tagged Dynamic.
//
// This catalog is a superset of hclDynamicPatterns: it adds Terraform-specific
// provider-function patterns and module-source URL schemes that are not present
// in generic HCL (Packer, Vault, Consul) contexts.
var terraformDynamicPatterns = append(
	hclDynamicPatterns,
	// ----------------------------------------------------------------
	// Terraform built-in functions (spec §resolver-slice)
	// Already present in hclDynamicPatterns above; listed here explicitly
	// for documentation completeness and to keep the "terraform" key
	// self-contained.
	// ----------------------------------------------------------------

	// Encoding / serialisation.
	regexp.MustCompile(`^jsonencode$`),
	regexp.MustCompile(`^jsondecode$`),

	// Collection.
	regexp.MustCompile(`^concat$`),
	regexp.MustCompile(`^merge$`),
	regexp.MustCompile(`^lookup$`),
	regexp.MustCompile(`^coalesce$`),

	// String.
	regexp.MustCompile(`^format$`),
	regexp.MustCompile(`^formatlist$`),

	// Filesystem / template.
	regexp.MustCompile(`^file$`),
	regexp.MustCompile(`^templatefile$`),

	// ----------------------------------------------------------------
	// Module-source references that do not statically resolve.
	// The HCL extractor emits the raw `source` value as an IMPORTS ToID;
	// these patterns classify them Dynamic so the resolver does not emit
	// false-positive unresolved edges.
	// ----------------------------------------------------------------

	// Terraform public registry (short form: <namespace>/<module>/<provider>).
	// Anchored to the three-segment slash-separated form used by HashiCorp
	// registry. We are careful not to match generic paths like docs/modules/vpc
	// (those are already handled via the long `registry.terraform.io/` form in
	// hclDynamicPatterns and the `../` relative-path patterns).
	regexp.MustCompile(`^[A-Za-z0-9_-]+/[A-Za-z0-9_-]+/[A-Za-z0-9_-]+$`),

	// Private / enterprise module registries.
	regexp.MustCompile(`^[a-z0-9.-]+\.[a-z]{2,}/`),

	// Version-pinned registry sources (source = "x//subdir?ref=v1.0").
	regexp.MustCompile(`//`),

	// ----------------------------------------------------------------
	// Provider-function dispatch: providers register functions under
	// `provider::<name>::<function>` namespacing (Terraform ≥ 1.8).
	// These are runtime-resolved via the provider plugin and cannot be
	// statically bound.
	// ----------------------------------------------------------------
	regexp.MustCompile(`^provider::`),
)

func init() {
	dynamicPatternsByLang["terraform"] = terraformDynamicPatterns
}
