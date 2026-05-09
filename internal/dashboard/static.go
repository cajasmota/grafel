package dashboard

import "embed"

// staticFS holds the placeholder SPA bundle. The real dashboard frontend
// will replace the contents of internal/dashboard/static/ once it lands;
// the embed directive does not need to change.
//
//go:embed static
var staticFS embed.FS
