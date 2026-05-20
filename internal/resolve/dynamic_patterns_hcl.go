package resolve

import "regexp"

// hclDynamicPatterns are per-language patterns for HCL and Terraform.
// Registered via init() into dynamicPatternsByLang.
//
// HCL / Terraform dynamic-pattern catalog (issue #44). Terraform
// interpolations bind at apply time through provider / module / variable
// indirection that no static resolver can fully follow. The HCL
// extractor already emits same-file structural-refs for `var.*`,
// `local.*`, `module.*`, `data.*.*`, `output.*`, and resource refs;
// what lands here are the residual cross-file refs inside a
// multi-file module (root module's `variables.tf` referenced from
// `main.tf`, etc.), provider/meta-arg dispatches, and Terraform
// built-in function leaves the extractor surfaces as bare callees.
// Per-language gate (lang == "hcl" or "terraform") keeps these
// patterns from poisoning resolution in other ecosystems where
// `local`, `module`, `data`, `var`, `count`, `for_each`, `merge`,
// `lookup`, etc. are common identifiers.
var hclDynamicPatterns = []*regexp.Regexp{
	// Terraform built-in reference prefixes. Same-file structural-refs
	// resolve via byLocation; cross-file leftovers are framework
	// dispatch the resolver cannot statically bind.
	regexp.MustCompile(`^var\.[A-Za-z_]`),      // var.<name>
	regexp.MustCompile(`^local\.[A-Za-z_]`),    // local.<name>
	regexp.MustCompile(`^module\.[A-Za-z_]`),   // module.<name>(.attr)
	regexp.MustCompile(`^data\.[A-Za-z_]`),     // data.<type>.<name>(.attr)
	regexp.MustCompile(`^output\.[A-Za-z_]`),   // output.<name>
	regexp.MustCompile(`^provider\.[A-Za-z_]`), // provider.<name>
	regexp.MustCompile(`^path\.(module|root|cwd)`),
	regexp.MustCompile(`^terraform\.(workspace|env)`),
	regexp.MustCompile(`^self\.`), // self.<attr> inside provisioner blocks
	regexp.MustCompile(`^each\.(key|value)`),
	regexp.MustCompile(`^count\.index`),
	// `dynamic "<label>" { ... }` blocks introduce an iteration symbol
	// equal to the label, so `dynamic "statement" { ... }` produces
	// `statement.value` / `statement.key` references. The label names
	// are arbitrary user identifiers; the suffix is what marks the
	// reference as Terraform iteration dispatch.
	regexp.MustCompile(`^[a-z_][a-z0-9_]*\.(value|key)$`),
	// `for x in <expr> : ...` and `[for x in <expr> : x.<attr>]`
	// introduce a single-letter (or short) iteration variable used as
	// `<iter>.<attr>`. Anchored to a single lowercase letter followed
	// by a dot so user identifiers like `aws_instance.x` are not
	// swept up by accident.
	regexp.MustCompile(`^[a-z]\.[a-z_]`),
	// Terraform meta-arguments arriving as bare leaves.
	regexp.MustCompile(`^count$`),
	regexp.MustCompile(`^for_each$`),
	regexp.MustCompile(`^depends_on$`),
	regexp.MustCompile(`^lifecycle$`),
	regexp.MustCompile(`^dynamic$`),
	regexp.MustCompile(`^provisioner$`),
	regexp.MustCompile(`^connection$`),
	// Terraform built-in function names (full catalog).
	// Numeric.
	regexp.MustCompile(`^abs$`),
	regexp.MustCompile(`^ceil$`),
	regexp.MustCompile(`^floor$`),
	regexp.MustCompile(`^log$`),
	regexp.MustCompile(`^max$`),
	regexp.MustCompile(`^min$`),
	regexp.MustCompile(`^parseint$`),
	regexp.MustCompile(`^pow$`),
	regexp.MustCompile(`^signum$`),
	// String.
	regexp.MustCompile(`^chomp$`),
	regexp.MustCompile(`^format$`),
	regexp.MustCompile(`^formatlist$`),
	regexp.MustCompile(`^indent$`),
	regexp.MustCompile(`^join$`),
	regexp.MustCompile(`^lower$`),
	regexp.MustCompile(`^regex$`),
	regexp.MustCompile(`^regexall$`),
	regexp.MustCompile(`^replace$`),
	regexp.MustCompile(`^split$`),
	regexp.MustCompile(`^strrev$`),
	regexp.MustCompile(`^substr$`),
	regexp.MustCompile(`^title$`),
	regexp.MustCompile(`^trim$`),
	regexp.MustCompile(`^trimprefix$`),
	regexp.MustCompile(`^trimsuffix$`),
	regexp.MustCompile(`^trimspace$`),
	regexp.MustCompile(`^upper$`),
	regexp.MustCompile(`^startswith$`),
	regexp.MustCompile(`^endswith$`),
	// Collection.
	regexp.MustCompile(`^alltrue$`),
	regexp.MustCompile(`^anytrue$`),
	regexp.MustCompile(`^chunklist$`),
	regexp.MustCompile(`^coalesce$`),
	regexp.MustCompile(`^coalescelist$`),
	regexp.MustCompile(`^compact$`),
	regexp.MustCompile(`^concat$`),
	regexp.MustCompile(`^contains$`),
	regexp.MustCompile(`^distinct$`),
	regexp.MustCompile(`^element$`),
	regexp.MustCompile(`^flatten$`),
	regexp.MustCompile(`^index$`),
	regexp.MustCompile(`^keys$`),
	regexp.MustCompile(`^length$`),
	regexp.MustCompile(`^list$`),
	regexp.MustCompile(`^lookup$`),
	regexp.MustCompile(`^map$`),
	regexp.MustCompile(`^matchkeys$`),
	regexp.MustCompile(`^merge$`),
	regexp.MustCompile(`^one$`),
	regexp.MustCompile(`^range$`),
	regexp.MustCompile(`^reverse$`),
	regexp.MustCompile(`^setintersection$`),
	regexp.MustCompile(`^setproduct$`),
	regexp.MustCompile(`^setsubtract$`),
	regexp.MustCompile(`^setunion$`),
	regexp.MustCompile(`^slice$`),
	regexp.MustCompile(`^sort$`),
	regexp.MustCompile(`^sum$`),
	regexp.MustCompile(`^transpose$`),
	regexp.MustCompile(`^try$`),
	regexp.MustCompile(`^values$`),
	regexp.MustCompile(`^zipmap$`),
	regexp.MustCompile(`^nonsensitive$`),
	regexp.MustCompile(`^sensitive$`),
	// Encoding.
	regexp.MustCompile(`^base64decode$`),
	regexp.MustCompile(`^base64encode$`),
	regexp.MustCompile(`^base64gzip$`),
	regexp.MustCompile(`^csvdecode$`),
	regexp.MustCompile(`^jsondecode$`),
	regexp.MustCompile(`^jsonencode$`),
	regexp.MustCompile(`^textdecodebase64$`),
	regexp.MustCompile(`^textencodebase64$`),
	regexp.MustCompile(`^urlencode$`),
	regexp.MustCompile(`^yamldecode$`),
	regexp.MustCompile(`^yamlencode$`),
	// Filesystem / template.
	regexp.MustCompile(`^abspath$`),
	regexp.MustCompile(`^basename$`),
	regexp.MustCompile(`^dirname$`),
	regexp.MustCompile(`^pathexpand$`),
	regexp.MustCompile(`^file$`),
	regexp.MustCompile(`^fileexists$`),
	regexp.MustCompile(`^fileset$`),
	regexp.MustCompile(`^filebase64$`),
	regexp.MustCompile(`^templatefile$`),
	regexp.MustCompile(`^templatestring$`),
	// Date/time.
	regexp.MustCompile(`^formatdate$`),
	regexp.MustCompile(`^plantimestamp$`),
	regexp.MustCompile(`^timeadd$`),
	regexp.MustCompile(`^timecmp$`),
	regexp.MustCompile(`^timestamp$`),
	// Hash/crypto.
	regexp.MustCompile(`^base64sha256$`),
	regexp.MustCompile(`^base64sha512$`),
	regexp.MustCompile(`^bcrypt$`),
	regexp.MustCompile(`^filebase64sha256$`),
	regexp.MustCompile(`^filebase64sha512$`),
	regexp.MustCompile(`^filemd5$`),
	regexp.MustCompile(`^filesha1$`),
	regexp.MustCompile(`^filesha256$`),
	regexp.MustCompile(`^filesha512$`),
	regexp.MustCompile(`^md5$`),
	regexp.MustCompile(`^rsadecrypt$`),
	regexp.MustCompile(`^sha1$`),
	regexp.MustCompile(`^sha256$`),
	regexp.MustCompile(`^sha512$`),
	regexp.MustCompile(`^uuid$`),
	regexp.MustCompile(`^uuidv5$`),
	// IP/network.
	regexp.MustCompile(`^cidrhost$`),
	regexp.MustCompile(`^cidrnetmask$`),
	regexp.MustCompile(`^cidrsubnet$`),
	regexp.MustCompile(`^cidrsubnets$`),
	// Type conversion.
	regexp.MustCompile(`^can$`),
	regexp.MustCompile(`^tobool$`),
	regexp.MustCompile(`^tolist$`),
	regexp.MustCompile(`^tomap$`),
	regexp.MustCompile(`^tonumber$`),
	regexp.MustCompile(`^toset$`),
	regexp.MustCompile(`^tostring$`),
	regexp.MustCompile(`^type$`),
	// Variable-shaped leaves that arrive bare from the extractor when
	// an interpolation is just `var.foo` (no further .attr) — already
	// covered by `^var\.` etc above; the bare `var`, `local`, `module`
	// leaves (rare, e.g. via dynamic blocks) round it out.
	regexp.MustCompile(`^var$`),
	regexp.MustCompile(`^local$`),
	regexp.MustCompile(`^module$`),
	regexp.MustCompile(`^data$`),
	regexp.MustCompile(`^output$`),
	// Provider-prefixed resource / data refs that miss the same-file
	// structural-ref bind (declared in a sibling file of the same
	// module). The major Terraform provider prefixes — any new
	// provider follows the same `<prefix>_<resource_type>.<name>`
	// shape, so anchoring on the prefix keeps the catalog stable
	// without enumerating every provider.
	regexp.MustCompile(`^aws_[a-z0-9_]+\.`),
	regexp.MustCompile(`^azurerm_[a-z0-9_]+\.`),
	regexp.MustCompile(`^azuread_[a-z0-9_]+\.`),
	regexp.MustCompile(`^google_[a-z0-9_]+\.`),
	regexp.MustCompile(`^kubernetes_[a-z0-9_]+\.`),
	regexp.MustCompile(`^helm_[a-z0-9_]+\.`),
	regexp.MustCompile(`^oci_[a-z0-9_]+\.`),
	regexp.MustCompile(`^null_[a-z0-9_]+\.`),
	regexp.MustCompile(`^random_[a-z0-9_]+\.`),
	regexp.MustCompile(`^tls_[a-z0-9_]+\.`),
	regexp.MustCompile(`^template_[a-z0-9_]+\.`),
	regexp.MustCompile(`^archive_[a-z0-9_]+\.`),
	regexp.MustCompile(`^external_[a-z0-9_]+\.`),
	regexp.MustCompile(`^http_[a-z0-9_]+\.`),
	regexp.MustCompile(`^vault_[a-z0-9_]+\.`),
	regexp.MustCompile(`^datadog_[a-z0-9_]+\.`),
	regexp.MustCompile(`^cloudflare_[a-z0-9_]+\.`),
	regexp.MustCompile(`^github_[a-z0-9_]+\.`),
	regexp.MustCompile(`^gitlab_[a-z0-9_]+\.`),
	regexp.MustCompile(`^digitalocean_[a-z0-9_]+\.`),
	regexp.MustCompile(`^linode_[a-z0-9_]+\.`),
	regexp.MustCompile(`^alicloud_[a-z0-9_]+\.`),
	regexp.MustCompile(`^tencentcloud_[a-z0-9_]+\.`),
	regexp.MustCompile(`^hcp_[a-z0-9_]+\.`),
	regexp.MustCompile(`^consul_[a-z0-9_]+\.`),
	regexp.MustCompile(`^nomad_[a-z0-9_]+\.`),
	regexp.MustCompile(`^docker_[a-z0-9_]+\.`),
	// Bare provider names — emitted as IMPORTS ToID by the HCL
	// extractor for `provider "<name>"` blocks. Same rationale as the
	// resource-prefix patterns above: the provider plugin lives outside
	// the static graph and the dynamic bucket is the right disposition.
	regexp.MustCompile(`^aws$`),
	regexp.MustCompile(`^azurerm$`),
	regexp.MustCompile(`^azuread$`),
	regexp.MustCompile(`^google$`),
	regexp.MustCompile(`^kubernetes$`),
	regexp.MustCompile(`^helm$`),
	regexp.MustCompile(`^oci$`),
	regexp.MustCompile(`^null$`),
	regexp.MustCompile(`^random$`),
	regexp.MustCompile(`^tls$`),
	regexp.MustCompile(`^template$`),
	regexp.MustCompile(`^archive$`),
	regexp.MustCompile(`^external$`),
	regexp.MustCompile(`^http$`),
	regexp.MustCompile(`^vault$`),
	regexp.MustCompile(`^datadog$`),
	regexp.MustCompile(`^cloudflare$`),
	regexp.MustCompile(`^github$`),
	regexp.MustCompile(`^gitlab$`),
	regexp.MustCompile(`^digitalocean$`),
	regexp.MustCompile(`^linode$`),
	regexp.MustCompile(`^alicloud$`),
	regexp.MustCompile(`^tencentcloud$`),
	regexp.MustCompile(`^hcp$`),
	regexp.MustCompile(`^consul$`),
	regexp.MustCompile(`^nomad$`),
	regexp.MustCompile(`^docker$`),
	// Relative-path module sources (`source = "../../"`,
	// `source = "./modules/foo"`). The HCL extractor emits these as
	// raw IMPORTS ToIDs for module blocks. Local module paths could
	// be resolved to a sibling-directory entity in principle but the
	// pattern of `..`/`./` prefixes is unambiguously a path; tagging
	// Dynamic is consistent with how Python's relative-import paths
	// (`^\.+`) land in pythonDynamicPatterns.
	regexp.MustCompile(`^\.\.?/`),
	regexp.MustCompile(`^\.\.?$`),
	// Terraform registry module sources
	// (`registry.terraform.io/hashicorp/aws/version`). External
	// package not in the graph. The 3-segment registry short form
	// (`hashicorp/aws/version`) is deliberately NOT pattern-matched
	// here — it collides with markdown IMPORTS shaped the same way
	// (`docs/modules/vpc-endpoints`); the long form (with the
	// `registry.terraform.io/` host) is unambiguous.
	regexp.MustCompile(`^registry\.terraform\.io/`),
	regexp.MustCompile(`^git::`),
	regexp.MustCompile(`^github\.com/`),
	regexp.MustCompile(`^bitbucket\.org/`),
}

func init() {
	dynamicPatternsByLang["hcl"] = hclDynamicPatterns
	// "terraform" key is registered separately in dynamic_patterns_terraform.go
	// so that Terraform-specific additions (provider functions, registry sources,
	// git:: module refs) can extend the shared hclDynamicPatterns base without
	// polluting generic HCL (Packer, Vault, Consul) resolution.
}
