// Entry-points sniffer proving tests for Swift (Phase 1B T3).
//
// Cells proven to partial (sniffer fires but full requires framework-specific
// test corpus for Vapor lifecycle hooks):
//   - reachability_analysis  (entry_points sniffer fires on exported fns)
//   - dead_code_detection    (seeds the BFS from entry points)
package substrate

import "testing"

func TestSwiftEntryPoints_AtMain(t *testing.T) {
	src := `
@main
struct MyApp {
    static func main() {
        print("running")
    }
}
`
	eps := sniffSwiftEntryPoints(src)
	found := false
	for _, ep := range eps {
		if ep.Kind == EntryKindCLIMain && ep.Ident == "__swift_main__" {
			found = true
		}
	}
	if !found {
		t.Error("expected cli_main entry for @main attribute")
	}
}

func TestSwiftEntryPoints_StaticMain(t *testing.T) {
	src := `
struct CLIRunner {
    static func main() throws {
        try run()
    }
}
`
	eps := sniffSwiftEntryPoints(src)
	found := false
	for _, ep := range eps {
		if ep.Kind == EntryKindCLIMain && ep.Ident == "main" {
			found = true
		}
	}
	if !found {
		t.Error("expected cli_main entry for static func main()")
	}
}

func TestSwiftEntryPoints_VaporLifecycle(t *testing.T) {
	src := `
func configure(_ app: Application) throws {
    app.databases.use(.postgres(configuration: .init(url: env("DATABASE_URL")!)), as: .psql)
}

func boot(_ app: Application) throws {
    try routes(app)
}
`
	eps := sniffSwiftEntryPoints(src)
	var lifecycleNames []string
	for _, ep := range eps {
		if ep.Kind == EntryKindFrameworkLifecycle {
			lifecycleNames = append(lifecycleNames, ep.Ident)
		}
	}
	if len(lifecycleNames) == 0 {
		t.Error("expected framework_lifecycle entries for configure/boot")
	}
	hasConfigure := false
	hasBoot := false
	for _, n := range lifecycleNames {
		if n == "configure" {
			hasConfigure = true
		}
		if n == "boot" {
			hasBoot = true
		}
	}
	if !hasConfigure {
		t.Error("expected lifecycle entry for configure")
	}
	if !hasBoot {
		t.Error("expected lifecycle entry for boot")
	}
}

func TestSwiftEntryPoints_XCTest(t *testing.T) {
	src := `
final class UserTests: XCTestCase {
    override func setUp() async throws {}
    override func tearDown() async throws {}

    func testCreateUser() async throws {
        XCTAssertNotNil(user)
    }

    func testDeleteUser() throws {
        XCTAssertTrue(deleted)
    }
}
`
	eps := sniffSwiftEntryPoints(src)
	testNames := map[string]bool{}
	for _, ep := range eps {
		if ep.Kind == EntryKindTestEntry {
			testNames[ep.Ident] = true
		}
	}
	if !testNames["setUp"] {
		t.Error("expected test_entry for setUp")
	}
	if !testNames["testCreateUser"] {
		t.Error("expected test_entry for testCreateUser")
	}
	if !testNames["testDeleteUser"] {
		t.Error("expected test_entry for testDeleteUser")
	}
}

func TestSwiftEntryPoints_LibraryExport(t *testing.T) {
	src := `
public func createUser(name: String) -> User {
    return User(name: name)
}

open func configureApp(_ app: Application) throws {
    try app.autoMigrate().wait()
}

private func internal helper() {}
`
	eps := sniffSwiftEntryPoints(src)
	exports := map[string]bool{}
	for _, ep := range eps {
		if ep.Kind == EntryKindLibraryExport {
			exports[ep.Ident] = true
		}
	}
	if !exports["createUser"] {
		t.Error("expected library_export for public func createUser")
	}
	if !exports["configureApp"] {
		t.Error("expected library_export for open func configureApp")
	}
}

func TestSwiftEntryPoints_VaporRun(t *testing.T) {
	src := `
import Vapor

var env = try Environment.detect()
let app = try Application(env)
defer { app.shutdown() }
try configure(app)
try app.run()
`
	eps := sniffSwiftEntryPoints(src)
	found := false
	for _, ep := range eps {
		if ep.Kind == EntryKindFrameworkLifecycle && ep.Ident == "app.run" {
			found = true
		}
	}
	if !found {
		t.Error("expected framework_lifecycle entry for app.run()")
	}
}

func TestSwiftEntryPoints_Empty(t *testing.T) {
	eps := sniffSwiftEntryPoints("")
	if len(eps) != 0 {
		t.Errorf("expected no entries for empty input, got %d", len(eps))
	}
}
