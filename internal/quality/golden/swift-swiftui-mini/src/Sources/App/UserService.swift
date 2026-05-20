import Foundation
import Combine

// UserService — URLSession + Combine networking exercising:
//   URLSession.shared.dataTaskPublisher
//   .map, .decode, .mapError, .eraseToAnyPublisher
//   Result type flatMap / map

class UserService {
    private let baseURL = URL(string: "https://api.example.com")!
    var usersPublisher: AnyPublisher<[User], Error> {
        return fetchAll()
    }

    func fetchAll() -> AnyPublisher<[User], Error> {
        let url = baseURL.appendingPathComponent("users")
        return URLSession.shared.dataTaskPublisher(for: url)
            .map(\.data)
            .decode(type: [User].self, decoder: JSONDecoder())
            .mapError { $0 as Error }
            .eraseToAnyPublisher()
    }

    func fetchUser(id: UUID) -> AnyPublisher<User, Error> {
        let url = baseURL
            .appendingPathComponent("users")
            .appendingPathComponent(id.uuidString)
        return URLSession.shared.dataTaskPublisher(for: url)
            .map(\.data)
            .decode(type: User.self, decoder: JSONDecoder())
            .mapError { $0 as Error }
            .eraseToAnyPublisher()
    }

    func createUser(_ user: User) -> AnyPublisher<User, Error> {
        var request = URLRequest(url: baseURL.appendingPathComponent("users"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = user.encode()
        return URLSession.shared.dataTaskPublisher(for: request)
            .map(\.data)
            .decode(type: User.self, decoder: JSONDecoder())
            .mapError { $0 as Error }
            .eraseToAnyPublisher()
    }
}
