package javascript_test

import (
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// issue5487_server_actions_test.go — Next.js Server Actions (#5487, epic #5479).
//
// Proves the extractor recognises Server Actions in three forms and emits each as
// a SCOPE.Operation/server_action operation:
//   1. file-level `'use server'`        → every exported async fn is an action
//   2. function-level inline `'use server'`
//   3. wrapped `action()`-idiom         → `export const x = action(schema, handler)`
//      with the optional validation schema captured as `validation_schema`.
// Includes a negative: a plain `const x = foo()` must NOT be misread as an action.

// extractNext runs the nextjs extractor and returns the full EntityRecords (so we
// can inspect Properties like validation_schema / action_wrapper).
func extractNext(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	return extractFull(t, "custom_js_nextjs", extreg.FileInput{Path: path, Language: "typescript", Content: []byte(src)})
}

func serverAction(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Subtype == "server_action" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func TestNextjs5487FileLevelUseServerTwoExports(t *testing.T) {
	src := `'use server'
export async function createPost(form: FormData) {}
export async function deletePost(id: string) {}
`
	ents := extractNext(t, "app/posts/actions.ts", src)
	for _, name := range []string{"createPost", "deletePost"} {
		if serverAction(ents, name) == nil {
			t.Errorf("expected file-level 'use server' action %q", name)
		}
	}
}

func TestNextjs5487InlineUseServer(t *testing.T) {
	src := `export async function outer() {
  async function saveItem(data: FormData) {
    'use server'
    await db.save(data)
  }
  return saveItem
}
`
	ents := extractNext(t, "app/lib/util.ts", src)
	if serverAction(ents, "saveItem") == nil {
		t.Error("expected inline 'use server' action saveItem")
	}
}

func TestNextjs5487WrappedActionWithSchema(t *testing.T) {
	src := `import { action } from '@/lib/safe-action'
import { createPostSchema } from './schema'
export const createPost = action(createPostSchema, async (input) => {
  return db.posts.create(input)
})
`
	ents := extractNext(t, "app/actions/posts.ts", src)
	a := serverAction(ents, "createPost")
	if a == nil {
		t.Fatal("expected wrapped action createPost")
	}
	if got := a.Properties["validation_schema"]; got != "createPostSchema" {
		t.Errorf("validation_schema = %q, want createPostSchema", got)
	}
	if got := a.Properties["action_wrapper"]; got != "action" {
		t.Errorf("action_wrapper = %q, want action", got)
	}
}

func TestNextjs5487WrappedAuthActionNoSchema(t *testing.T) {
	src := `import { authAction } from '@/lib/safe-action'
export const logout = authAction(async () => {
  await session.destroy()
})
`
	ents := extractNext(t, "app/actions/auth.ts", src)
	a := serverAction(ents, "logout")
	if a == nil {
		t.Fatal("expected wrapped action logout")
	}
	if _, ok := a.Properties["validation_schema"]; ok {
		t.Errorf("logout has no schema arg; validation_schema should be unset, got %q", a.Properties["validation_schema"])
	}
	if got := a.Properties["action_wrapper"]; got != "authAction" {
		t.Errorf("action_wrapper = %q, want authAction", got)
	}
}

func TestNextjs5487PlainConstNotAction(t *testing.T) {
	// A plain `const x = foo()` with no 'use server' context and a non-wrapper
	// callee must NOT be misread as a Server Action.
	src := `const total = computeTotal(items)
export const formatter = makeFormatter(locale)
`
	ents := extractNext(t, "app/lib/helpers.ts", src)
	if serverAction(ents, "total") != nil || serverAction(ents, "formatter") != nil {
		t.Error("plain const initialised by a non-wrapper callee must not be a server_action")
	}
}
