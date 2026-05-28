package substrate

import "testing"

// taint_sites_jsts_loader_params_test.go — issue #2858 (gap surfaced by #2868):
// the meta-framework loader / data-function route-parameter source
// (`params.id`, `params['slug']`) must be recognised as a taint source, not
// just `req.*`-shaped access.

func TestTaintSniffer_JSTS_LoaderParamsSource(t *testing.T) {
	// SvelteKit-style server load + Remix-style loader both read params off the
	// destructured loader argument.
	src := `
export async function load({ params }) {
  const post = await db.query("SELECT * FROM posts WHERE id = " + params.id)
  return { post }
}
export async function loader({ params }) {
  const slug = params['slug']
  return slug
}
`
	got := sniffTaintJSTS(src)
	var sources int
	for _, m := range got {
		if m.Kind == TaintKindSource && m.Primitive == "loader params.*" {
			sources++
		}
	}
	if sources < 2 {
		t.Errorf("expected >=2 loader params.* sources (params.id + params['slug']), got %d; all=%+v", sources, got)
	}
}

func TestTaintSniffer_JSTS_LoaderParamsNoFalsePositive(t *testing.T) {
	// `searchParams` / `useParams` must NOT match the bare-params rule (the `p`
	// is preceded by a word char, so there is no word boundary).
	src := `
const sp = url.searchParams.get('q')
const { id } = useParams()
`
	got := sniffTaintJSTS(src)
	for _, m := range got {
		if m.Kind == TaintKindSource && m.Primitive == "loader params.*" {
			t.Errorf("unexpected loader params.* false positive: %+v", m)
		}
	}
}
