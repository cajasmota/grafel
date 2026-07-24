package hcl

import (
	"path"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// ----------------------------------------------------------------
// Issue #4657 — module instantiation → INSTANTIATES edge + env tagging
// ----------------------------------------------------------------
//
// The IaC diagram showed the module DEFINITIONS (e.g. modules/worker-service,
// modules/sqs-queue — each with its aws_* resources) ISOLATED from the
// environment stacks (envs/{dev,staging,prod}/main.tf, each of which only
// renders the opaque module INSTANCES `module.worker` and `module.dispatch_queue`).
// The instance→definition link was unresolved: a relative-path module
// `source = "../../modules/worker-service"` lands in hclDynamicPatterns and is
// classified Dynamic, so dev/staging/prod never connect to the definitions and
// the definitions read as disconnected boxes. This was the bulk of the
// "N relation targets could not be resolved" footer.
//
// resolveModuleInstantiation fixes this for a `module "X" { source = "<rel>" }`
// block: it path-resolves the relative source against the instance file's
// directory to the module DEFINITION directory (repo-relative), stamps that
// directory + the env name + the raw source onto the module-instance entity,
// and emits an INSTANTIATES edge
//
//	module.X (instance) --INSTANTIATES--> <definition dir>
//
// The definition directory is not a single graph entity (a Terraform module is
// a directory of .tf files), so the ToID is a path marker the resolver leaves
// Dynamic; the dashboard (#4657 handlers_iac.go) joins it to the definition's
// resources by DIRECTORY — every resource emitted under <definition dir> shares
// that directory as its IaCResource.Module — and synthesises the rendered
// instance→definition edges from the stamped definition_dir. This connects each
// env to the worker-service / sqs-queue definitions and lets the env view
// project the definition's resources inline.
//
// The stamped properties are:
//
//	module_source   the raw `source` value (e.g. ../../modules/worker-service)
//	definition_dir  the repo-relative resolved definition directory
//	                (e.g. modules/worker-service); empty for registry/remote sources
//	env             the environment name derived from the instance file's
//	                directory (e.g. dev / staging / prod), used by the env tabs
//
// Generalization note: AWS CDK (Stack/Construct instantiation), Pulumi
// (ComponentResource), CloudFormation nested stacks (AWS::CloudFormation::Stack
// TemplateURL), and Bicep modules express the same instance→definition pattern.
// resolveModuleSourceDir / envFromPath are framework-agnostic; those extractors
// can reuse the same definition_dir / env stamping + dashboard directory-join
// once they emit a comparable source path (tracked as a follow-up).

// DefinitionDirMarkerPrefix prefixes the INSTANTIATES edge ToID so the resolved
// definition directory is an unambiguous Dynamic target (not mistaken for a
// registry/markdown import or an extractor bug). The dashboard strips it to
// recover the directory for the instance→definition resource join (#4657).
const DefinitionDirMarkerPrefix = "tfmodule-def:"

// resolveModuleInstantiation stamps the module-instance entity with the resolved
// definition directory, the env name, and the raw source, and returns an
// INSTANTIATES edge from the instance to its definition directory (when the
// source is a local relative path). selfRef is the instance's canonical ref
// (e.g. "module.worker"); source is the raw `source` attribute value; filePath
// is the instance file's repo-relative path.
func resolveModuleInstantiation(rec *types.EntityRecord, source, filePath, lang, selfRef string) []types.RelationshipRecord {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}
	// Stamp into Properties (which the dashboard reader exposes) AND Metadata
	// (kept for parity with the existing `source` key). Terraform's other
	// resource attributes are surfaced from Properties; module_source / env /
	// definition_dir join the same surface so the env tabs + instance→definition
	// projection (#4657) can read them.
	if rec.Properties == nil {
		rec.Properties = map[string]string{}
	}
	rec.Properties["module_source"] = source
	rec.Metadata["module_source"] = source
	if env := envFromPath(filePath); env != "" {
		rec.Properties["env"] = env
		rec.Metadata["env"] = env
	}

	defDir := resolveModuleSourceDir(source, filePath)
	if defDir == "" {
		// Registry / git / remote source — no local definition directory to
		// join against. The raw source is still recorded above.
		return nil
	}
	rec.Properties["definition_dir"] = defDir
	rec.Metadata["definition_dir"] = defDir

	return []types.RelationshipRecord{{
		FromID: extractor.BuildOperationStructuralRef(lang, filePath, selfRef),
		// The definition is a directory, not a single entity; emit the directory
		// as the target under an unambiguous `tfmodule-def:` marker prefix so the
		// resolver classifies it Dynamic (DefinitionDirMarkerPrefix in
		// internal/resolve/dynamic_patterns_hcl.go) rather than as an unresolved
		// extractor bug, and the dashboard (#4657) strips the prefix to recover
		// the directory and join it to the definition's resources by
		// IaCResource.Module.
		ToID: DefinitionDirMarkerPrefix + defDir,
		Kind: "INSTANTIATES",
		Properties: types.Props{
			{K: "definition_dir", V: defDir},
			{K: "language", V: lang},
			{K: "module_source", V: source},
		},
	}}
}

// resolveModuleSourceDir resolves a Terraform module `source` value to the
// repo-relative module DEFINITION directory. It returns "" for non-local
// sources (Terraform registry shorthand, git::, github.com/, http(s), etc.) for
// which there is no in-repo directory to join against.
//
// A local source is one starting with "./" or "../" (or "/" — an absolute
// in-repo path, rare but valid). The path is resolved against the directory of
// the instance file and cleaned, so
//
//	file=envs/dev/main.tf  source=../../modules/worker-service
//	  → modules/worker-service
func resolveModuleSourceDir(source, filePath string) string {
	if !isLocalModuleSource(source) {
		return ""
	}
	src := path.Clean(toSlash(source))
	if path.IsAbs(src) {
		// Absolute in-repo path — strip the leading slash to keep it
		// repo-relative and comparable to entity SourceFile dirs.
		return strings.TrimPrefix(src, "/")
	}
	dir := path.Dir(toSlash(filePath))
	resolved := path.Clean(path.Join(dir, src))
	// path.Clean can produce a leading ".." when the source escapes the repo
	// root; such a target cannot be joined to any in-repo resource, so drop it.
	if resolved == "." || strings.HasPrefix(resolved, "..") {
		return ""
	}
	return resolved
}

// isLocalModuleSource reports whether a module source is a local filesystem
// path (the only kind with an in-repo definition directory). Terraform registry
// sources ("namespace/name/provider"), git/github/bitbucket/mercurial sources,
// and http(s) archive sources are NOT local.
func isLocalModuleSource(source string) bool {
	s := strings.TrimSpace(source)
	if s == "" {
		return false
	}
	return strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") ||
		s == "." || s == ".." || strings.HasPrefix(s, "/")
}

// envFromPath derives an environment name from an instance file's path. The
// real acme-v3 layout is envs/{dev,staging,prod}/main.tf, so the env is the
// path segment immediately following an "envs" / "environments" / "env"
// directory. Returns "" when the path does not encode a recognisable env
// (e.g. the file is itself a module definition), in which case the dashboard
// treats the stack as env-less.
func envFromPath(filePath string) string {
	parts := strings.Split(strings.Trim(toSlash(filePath), "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		switch parts[i] {
		case "envs", "environments", "env":
			if seg := parts[i+1]; seg != "" {
				return seg
			}
		}
	}
	return ""
}

// toSlash normalises OS path separators to forward slashes so resolution is
// stable across POSIX and Windows callers (entity SourceFile is already
// slash-normalised via filepath.ToSlash at emit time).
func toSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}
