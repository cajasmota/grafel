// Source: https://github.com/vapor/vapor/tree/main/Sources/Vapor (synthetic based on real Vapor patterns) | License: MIT

import Vapor
import Fluent

struct TodoController: RouteCollection {
    func boot(routes: RoutesBuilder) throws {
        let todos = routes.grouped("todos")
        todos.get(use: index)
        todos.post(use: create)
        todos.group(":todoID") { todo in
            todo.get(use: show)
            todo.put(use: update)
            todo.delete(use: delete)
        }
    }

    @Sendable func index(req: Request) async throws -> [TodoDTO] {
        let todos = try await Todo.query(on: req.db)
            .filter(\.$user.$id == req.auth.require(UserPayload.self).userID)
            .sort(\.$createdAt, .descending)
            .paginate(for: req)
        return todos.items.map { $0.toDTO() }
    }

    @Sendable func show(req: Request) async throws -> TodoDTO {
        guard let todo = try await Todo.find(req.parameters.get("todoID"), on: req.db) else {
            throw Abort(.notFound)
        }
        return todo.toDTO()
    }

    @Sendable func create(req: Request) async throws -> Response {
        try CreateTodoRequest.validate(content: req)
        let input = try req.content.decode(CreateTodoRequest.self)
        let user = try req.auth.require(UserPayload.self)

        let todo = Todo(
            title: input.title,
            isComplete: false,
            userID: user.userID
        )
        try await todo.save(on: req.db)

        return try await todo.toDTO().encodeResponse(status: .created, for: req)
    }

    @Sendable func update(req: Request) async throws -> TodoDTO {
        guard let todo = try await Todo.find(req.parameters.get("todoID"), on: req.db) else {
            throw Abort(.notFound)
        }

        let input = try req.content.decode(UpdateTodoRequest.self)
        if let title = input.title { todo.title = title }
        if let isComplete = input.isComplete { todo.isComplete = isComplete }

        try await todo.update(on: req.db)
        return todo.toDTO()
    }

    @Sendable func delete(req: Request) async throws -> HTTPStatus {
        guard let todo = try await Todo.find(req.parameters.get("todoID"), on: req.db) else {
            throw Abort(.notFound)
        }
        try await todo.delete(on: req.db)
        return .noContent
    }
}

// MARK: - Model
final class Todo: Model, Content, @unchecked Sendable {
    static let schema = "todos"

    @ID(key: .id)
    var id: UUID?

    @Field(key: "title")
    var title: String

    @Field(key: "is_complete")
    var isComplete: Bool

    @Parent(key: "user_id")
    var user: User

    @Timestamp(key: "created_at", on: .create)
    var createdAt: Date?

    @Timestamp(key: "updated_at", on: .update)
    var updatedAt: Date?

    init() {}

    init(id: UUID? = nil, title: String, isComplete: Bool, userID: UUID) {
        self.id = id
        self.title = title
        self.isComplete = isComplete
        self.$user.id = userID
    }

    func toDTO() -> TodoDTO {
        TodoDTO(
            id: self.id,
            title: self.title,
            isComplete: self.isComplete,
            createdAt: self.createdAt
        )
    }
}

// MARK: - DTOs
struct TodoDTO: Content {
    var id: UUID?
    var title: String
    var isComplete: Bool
    var createdAt: Date?
}

struct CreateTodoRequest: Content, Validatable {
    var title: String

    static func validations(_ validations: inout Validations) {
        validations.add("title", as: String.self, is: !.empty && .count(1...200))
    }
}

struct UpdateTodoRequest: Content {
    var title: String?
    var isComplete: Bool?
}

// MARK: - Migration
struct CreateTodo: AsyncMigration {
    func prepare(on database: Database) async throws {
        try await database.schema("todos")
            .id()
            .field("title", .string, .required)
            .field("is_complete", .bool, .required, .custom("DEFAULT FALSE"))
            .field("user_id", .uuid, .required, .references("users", "id", onDelete: .cascade))
            .field("created_at", .datetime)
            .field("updated_at", .datetime)
            .create()
    }

    func revert(on database: Database) async throws {
        try await database.schema("todos").delete()
    }
}
