package engine

import "regexp"

// Compiled patterns for endpoint pagination detection (see
// http_endpoint_pagination.go). Kept in one file so the (cheap) compile cost is
// paid once at package init.
var (
	// pyParamDeclRe matches a Python parameter declaration `name: Type = ...` or
	// bare `name,` in a (possibly multi-line) function signature. Group 1 is the
	// identifier. We only later keep the ones that are pagination-shaped.
	pyParamDeclRe = regexp.MustCompile(`(?m)[\(,]\s*([A-Za-z_][A-Za-z0-9_]*)\s*[:=,\)]`)

	// djangoPaginatorRe matches `Paginator(<qs>, <n>)` — the canonical Django
	// core paginator constructor.
	djangoPaginatorRe = regexp.MustCompile(`\bPaginator\s*\(`)

	// fastapiPaginateRe matches a fastapi-pagination `paginate(` call (the
	// library's page-style helper).
	fastapiPaginateRe = regexp.MustCompile(`\bpaginate\s*\(`)

	// springPageableParamRe matches a `Pageable <name>` handler parameter
	// (optionally annotated). Anchored on the word boundary so it does not match
	// `PageableXxx`.
	springPageableParamRe = regexp.MustCompile(`\bPageable\b\s+[A-Za-z_]`)

	// springPageReturnRe matches a `Page<...>` or `Slice<...>` return type
	// (Spring Data's paginated result wrappers).
	springPageReturnRe = regexp.MustCompile(`\b(?:Page|Slice)\s*<`)

	// jsQueryDotRe matches `req.query.<name>` / `request.query.<name>` /
	// `ctx.query.<name>` reads. Group 1 is the param name.
	jsQueryDotRe = regexp.MustCompile(`(?:req|request|ctx)\.query\.([A-Za-z_][A-Za-z0-9_]*)`)

	// jsQueryBracketRe matches `req.query["<name>"]` / `req.query['<name>']`.
	jsQueryBracketRe = regexp.MustCompile(`(?:req|request|ctx)\.query\[\s*["']([A-Za-z_][A-Za-z0-9_]*)["']\s*\]`)

	// jsQueryDestructureRe matches `const { a, b } = req.query`. Group 1 is the
	// brace contents.
	jsQueryDestructureRe = regexp.MustCompile(`\{\s*([^}]*?)\s*\}\s*=\s*(?:req|request|ctx)\.query\b`)

	// sequelizeOrPrismaTakeRe / sequelizeOrPrismaSkipRe match Prisma `take:` /
	// `skip:` keys (also used by some query builders).
	sequelizeOrPrismaTakeRe = regexp.MustCompile(`\btake\s*:`)
	sequelizeOrPrismaSkipRe = regexp.MustCompile(`\bskip\s*:`)

	// sequelizeLimitRe / sequelizeOffsetRe match Sequelize `limit:` / `offset:`
	// option keys (findAll({ limit, offset })).
	sequelizeLimitRe  = regexp.MustCompile(`\blimit\s*:`)
	sequelizeOffsetRe = regexp.MustCompile(`\boffset\s*:`)

	// prismaCursorRe matches a Prisma `.cursor(` / `cursor:` keyset selector.
	prismaCursorRe = regexp.MustCompile(`\bcursor\s*[:\(]`)

	// schemaNameRe pulls `"name"` keys out of a JSON parameter_schema blob.
	schemaNameRe = regexp.MustCompile(`"([A-Za-z_][A-Za-z0-9_]*)"\s*:`)
)
