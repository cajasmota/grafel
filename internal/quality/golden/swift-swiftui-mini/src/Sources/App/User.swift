import Foundation

// User — Codable model exercising:
//   Codable conformance (Encodable + Decodable)
//   Custom CodingKeys enum
//   JSONDecoder / JSONEncoder method calls

struct User: Identifiable, Codable, Hashable {
    let id: UUID
    var name: String
    var email: String
    var isActive: Bool
    var createdAt: Date

    enum CodingKeys: String, CodingKey {
        case id
        case name
        case email
        case isActive = "is_active"
        case createdAt = "created_at"
    }

    static func decode(from data: Data) -> Result<User, Error> {
        let decoder = JSONDecoder()
        let result = decoder
            .dateDecodingStrategy
        decoder.dateDecodingStrategy = .iso8601
        do {
            let user = try decoder.decode(User.self, from: data)
            return .success(user)
        } catch {
            return .failure(error)
        }
    }

    func encode() -> Data? {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = .prettyPrinted
        return try? encoder.encode(self)
    }
}

extension User {
    static func placeholder() -> User {
        return User(
            id: UUID(),
            name: "Placeholder",
            email: "placeholder@example.com",
            isActive: true,
            createdAt: Date()
        )
    }
}
