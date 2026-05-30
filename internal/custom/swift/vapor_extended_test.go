package swift_test

// Tests for the vapor_extended extractor covering auth, validation,
// middleware, testing, observability, and type alias capabilities.

import "testing"

func TestVaporExtendedAuth(t *testing.T) {
	src := `
struct UserAuthenticator: BearerAuthenticator {
    func authenticate(bearer: BearerAuthorization, for request: Request) async throws -> User? {
        return try await User.query(on: request.db).filter(\.$token == bearer.token).first()
    }
}

func protectedRoute(_ req: Request) async throws -> String {
    let user = try req.auth.require(User.self)
    return user.name
}
`
	ents := extract(t, "custom_swift_vapor_extended", fi("auth.swift", "swift", src))
	if !containsEntity(ents, "SCOPE.Pattern", "UserAuthenticator") {
		t.Error("expected UserAuthenticator authenticator pattern")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "auth:guard") {
		t.Error("expected auth:guard pattern from req.auth.require")
	}
}

func TestVaporExtendedValidation(t *testing.T) {
	src := `
struct CreateUserRequest: Content, Validatable {
    var name: String
    var email: String

    static func validations(_ validations: inout Validations) {
        validations.add("name", as: String.self, is: !.empty)
        validations.add("email", as: String.self, is: .email)
    }
}

func createUser(_ req: Request) async throws -> User {
    let dto = try req.content.decode(CreateUserRequest.self)
    return User(name: dto.name, email: dto.email)
}
`
	ents := extract(t, "custom_swift_vapor_extended", fi("create_user.swift", "swift", src))
	if !containsEntity(ents, "SCOPE.Schema", "CreateUserRequest") {
		t.Error("expected CreateUserRequest validatable schema")
	}
	if !containsEntity(ents, "SCOPE.Schema", "dto:CreateUserRequest") {
		t.Error("expected dto:CreateUserRequest from content.decode")
	}
}

func TestVaporExtendedMiddleware(t *testing.T) {
	src := `
func routes(_ app: Application) throws {
    let protected = app.grouped(TokenAuthMiddleware(), RateLimitMiddleware())
    protected.get("profile") { req async throws -> User in
        return try req.auth.require(User.self)
    }
}
`
	ents := extract(t, "custom_swift_vapor_extended", fi("routes.swift", "swift", src))
	// Should find middleware patterns from grouped
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "middleware" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one middleware pattern from grouped call")
	}
}

func TestVaporExtendedTesting(t *testing.T) {
	src := `
import XCTest
@testable import App

final class UserControllerTests: XCTestCase {
    var app: Application!

    override func setUp() async throws {
        app = try await Application.testable()
    }

    override func tearDown() async throws {
        app.shutdown()
    }

    func testCreateUser() async throws {
        try await app.test(.POST, "users") { req async in
            try req.content.encode(["name": "Alice"])
        } afterResponse: { res async in
            XCTAssertEqual(res.status, .created)
        }
    }
}
`
	ents := extract(t, "custom_swift_vapor_extended", fi("UserControllerTests.swift", "swift", src))
	if !containsEntity(ents, "SCOPE.Component", "UserControllerTests") {
		t.Error("expected UserControllerTests test_suite component")
	}
	if !containsEntity(ents, "SCOPE.Operation", "testCreateUser") {
		t.Error("expected testCreateUser test_function operation")
	}
	if !containsEntity(ents, "SCOPE.Component", "vapor:testable_app") {
		t.Error("expected vapor:testable_app from Application.testable()")
	}
}

func TestVaporExtendedObservabilityLog(t *testing.T) {
	src := `
func getUser(_ req: Request) async throws -> User {
    req.logger.info("Fetching user")
    req.logger.error("User not found")
    return try await User.find(req.parameters.get("id"), on: req.db)!
}
`
	ents := extract(t, "custom_swift_vapor_extended", fi("get_user.swift", "swift", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "log_call" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected log_call pattern from req.logger.info")
	}
}

func TestVaporExtendedObservabilityMetric(t *testing.T) {
	src := `
import Metrics

let requestCounter = Counter(label: "http_requests_total")
let responseTimer = Timer(label: "http_response_duration")
`
	ents := extract(t, "custom_swift_vapor_extended", fi("metrics.swift", "swift", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "metric" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected metric pattern from Counter(label:)")
	}
}

func TestVaporExtendedTypeAlias(t *testing.T) {
	src := `
public typealias UserID = UUID
typealias EventHandler = (Request) async throws -> Response
internal typealias DB = Database
`
	ents := extract(t, "custom_swift_vapor_extended", fi("aliases.swift", "swift", src))
	if !containsEntity(ents, "SCOPE.Component", "UserID") {
		t.Error("expected UserID typealias component")
	}
	if !containsEntity(ents, "SCOPE.Component", "EventHandler") {
		t.Error("expected EventHandler typealias component")
	}
	if !containsEntity(ents, "SCOPE.Component", "DB") {
		t.Error("expected DB typealias component")
	}
}

func TestVaporExtendedNoMatch(t *testing.T) {
	src := `let x = 42`
	ents := extract(t, "custom_swift_vapor_extended", fi("simple.swift", "swift", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
