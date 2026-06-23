// Package javascript — issue #2874 Angular Internals (B) cells:
// rxjs_pattern_detection + guard_interceptor_recognition.
//
// Proves the two implementation cells with the hand-written
// testdata/angular_internals/rxjs_guards.ts fixture:
//   - RxJS .pipe(operators) → rxjs_pipeline + TRANSFORMS edges per operator
//   - RxJS .subscribe(...)   → rxjs_subscription + SUBSCRIBES_TO edge
//   - new Subject/BehaviorSubject → rxjs_subject
//   - `| async` inline template → rxjs_async_pipe component flag
//   - class CanActivate guards / HttpInterceptor → angular_role + IMPLEMENTS
//   - functional CanActivateFn / HttpInterceptorFn → angular_guard/_interceptor
package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func extractAngularFixture(t *testing.T) []types.EntityRecord {
	t.Helper()
	path := filepath.Join("testdata", "angular_internals", "rxjs_guards.ts")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tstsx.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ext, _ := extreg.Get("typescript")
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path: path, Content: content, Language: "typescript", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func bySubtype(ents []types.EntityRecord, kind, subtype string) []types.EntityRecord {
	var out []types.EntityRecord
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Subtype == subtype {
			out = append(out, ents[i])
		}
	}
	return out
}

func TestIssue2874_RxjsPatternDetection(t *testing.T) {
	ents := extractAngularFixture(t)

	// rxjs_pipeline: the FeedComponent.load() .pipe(map, switchMap, filter, …).
	pipelines := bySubtype(ents, "SCOPE.Operation", "rxjs_pipeline")
	if len(pipelines) == 0 {
		t.Fatalf("expected at least one rxjs_pipeline operation; got %s", dumpKinds(ents))
	}
	var feedPipe *types.EntityRecord
	for i := range pipelines {
		if pipelines[i].Properties["operators"] != "" {
			feedPipe = &pipelines[i]
			break
		}
	}
	if feedPipe == nil {
		t.Fatalf("no rxjs_pipeline carried an operators list")
	}
	for _, want := range []string{"map", "switchMap", "filter", "catchError", "takeUntil"} {
		if !hasRel(feedPipe.Relationships, "TRANSFORMS", "rxjs:operator:"+want) {
			t.Errorf("pipeline missing TRANSFORMS → rxjs:operator:%s; ops=%q rels=%v",
				want, feedPipe.Properties["operators"], feedPipe.Relationships)
		}
	}
	if feedPipe.Properties["framework"] != "angular" {
		t.Errorf("pipeline framework = %q, want angular", feedPipe.Properties["framework"])
	}

	// rxjs_subscription: feed$.subscribe(...).
	subs := bySubtype(ents, "SCOPE.Operation", "rxjs_subscription")
	if len(subs) == 0 {
		t.Fatalf("expected a rxjs_subscription operation")
	}
	if !hasRel(subs[0].Relationships, "SUBSCRIBES_TO", "rxjs:observable:feed$") {
		t.Errorf("subscription missing SUBSCRIBES_TO → rxjs:observable:feed$; rels=%v", subs[0].Relationships)
	}

	// rxjs_subject: new Subject() + new BehaviorSubject().
	subjects := bySubtype(ents, "SCOPE.Operation", "rxjs_subject")
	gotSubjectKinds := map[string]bool{}
	for i := range subjects {
		gotSubjectKinds[subjects[i].Properties["subject_kind"]] = true
	}
	for _, want := range []string{"Subject", "BehaviorSubject"} {
		if !gotSubjectKinds[want] {
			t.Errorf("expected rxjs_subject for %s; got %v", want, gotSubjectKinds)
		}
	}

	// async-pipe flag on the component.
	feed := findBySubtype(ents, "FeedComponent", "angular_component")
	if feed == nil {
		t.Fatalf("expected FeedComponent angular_component")
	}
	if feed.Properties["rxjs_async_pipe"] != "true" {
		t.Errorf("FeedComponent rxjs_async_pipe = %q, want true", feed.Properties["rxjs_async_pipe"])
	}
}

func TestIssue2874_GuardInterceptorRecognition(t *testing.T) {
	ents := extractAngularFixture(t)

	// Class guard: AuthGuard implements CanActivate/CanActivateChild. The class
	// is @Injectable so it surfaces as angular_service with an angular_role +
	// IMPLEMENTS edge.
	authGuard := findBySubtype(ents, "AuthGuard", "angular_service")
	if authGuard == nil {
		t.Fatalf("expected AuthGuard angular_service; got %s", dumpKinds(ents))
	}
	if authGuard.Properties["angular_role"] != "guard" {
		t.Errorf("AuthGuard angular_role = %q, want guard", authGuard.Properties["angular_role"])
	}
	if !hasRel(authGuard.Relationships, "IMPLEMENTS", "CanActivate") {
		t.Errorf("AuthGuard missing IMPLEMENTS → CanActivate; rels=%v", authGuard.Relationships)
	}

	// Class resolver: FeedResolver implements Resolve<...> → guard role.
	resolver := findBySubtype(ents, "FeedResolver", "angular_service")
	if resolver == nil || resolver.Properties["angular_role"] != "guard" {
		t.Errorf("expected FeedResolver angular_role=guard; got %+v", resolver)
	}

	// Class interceptor: AuthInterceptor implements HttpInterceptor.
	interceptor := findBySubtype(ents, "AuthInterceptor", "angular_service")
	if interceptor == nil {
		t.Fatalf("expected AuthInterceptor angular_service")
	}
	if interceptor.Properties["angular_role"] != "interceptor" {
		t.Errorf("AuthInterceptor angular_role = %q, want interceptor", interceptor.Properties["angular_role"])
	}
	if !hasRel(interceptor.Relationships, "IMPLEMENTS", "HttpInterceptor") {
		t.Errorf("AuthInterceptor missing IMPLEMENTS → HttpInterceptor")
	}

	// Functional guard: adminGuard: CanActivateFn.
	adminGuard := findBySubtype(ents, "adminGuard", "angular_guard")
	if adminGuard == nil {
		t.Fatalf("expected adminGuard angular_guard (functional); got %s", dumpKinds(ents))
	}
	if adminGuard.Properties["functional"] != "true" || adminGuard.Properties["guard_type"] != "CanActivateFn" {
		t.Errorf("adminGuard props = %v", adminGuard.Properties)
	}

	// Functional interceptor: tokenInterceptor: HttpInterceptorFn.
	tokenInt := findBySubtype(ents, "tokenInterceptor", "angular_interceptor")
	if tokenInt == nil {
		t.Fatalf("expected tokenInterceptor angular_interceptor (functional)")
	}
	if tokenInt.Properties["guard_type"] != "HttpInterceptorFn" {
		t.Errorf("tokenInterceptor guard_type = %q, want HttpInterceptorFn", tokenInt.Properties["guard_type"])
	}
}

// TestIssue2874_RealData_Rxjs verifies rxjs_pattern_detection fires on the
// real-world Angular fixture (testdata/fixtures/real-world), which uses
// debounceTime/distinctUntilChanged/switchMap pipelines, .subscribe(), a
// `new Subject()` and `| async` template pipes — i.e. real-shaped Angular,
// not just the hand-written unit fixture.
func TestIssue2874_RealData_Rxjs(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "fixtures", "real-world", "typescript", "angular_component.ts")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("real-world angular fixture not present: %v", err)
	}
	parser, _ := tssmacker.New().NewParser(tssmacker.WrapLanguage(tstsx.GetLanguage()))
	defer parser.Close()
	tree, _ := parser.Parse(content)
	ext, _ := extreg.Get("typescript")
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path: path, Content: content, Language: "typescript", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	pipelines := bySubtype(ents, "SCOPE.Operation", "rxjs_pipeline")
	subs := bySubtype(ents, "SCOPE.Operation", "rxjs_subscription")
	subjects := bySubtype(ents, "SCOPE.Operation", "rxjs_subject")
	if len(pipelines) == 0 {
		t.Errorf("real-data: expected rxjs_pipeline operations; got %d", len(pipelines))
	}
	if len(subs) == 0 {
		t.Errorf("real-data: expected rxjs_subscription operations; got %d", len(subs))
	}
	if len(subjects) == 0 {
		t.Errorf("real-data: expected rxjs_subject operations; got %d", len(subjects))
	}
	t.Logf("real-data rxjs: pipelines=%d subscriptions=%d subjects=%d",
		len(pipelines), len(subs), len(subjects))
}
