// Sample Swift service — golden fixture source.
import Foundation

struct User: Codable {
    var id: Int
    var name: String
    var email: String
}

struct CreateUserRequest: Codable {
    let name: String
    let email: String
}

class UserService {
    private var users: [User] = [
        User(id: 1, name: "Alice", email: "alice@example.com")
    ]
    private var nextId = 2

    func findAll() -> [User] {
        return users
    }

    func findById(_ id: Int) -> User? {
        return users.first { $0.id == id }
    }

    func create(_ request: CreateUserRequest) -> User {
        let user = User(id: nextId, name: request.name, email: request.email)
        nextId += 1
        users.append(user)
        return user
    }

    func delete(_ id: Int) -> Bool {
        let before = users.count
        users.removeAll { $0.id == id }
        return users.count < before
    }
}

protocol Repository {
    associatedtype Entity
    associatedtype ID

    func findById(_ id: ID) -> Entity?
    func findAll() -> [Entity]
    func save(_ entity: Entity) -> Entity
    func delete(_ id: ID) -> Bool
}
