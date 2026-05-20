import Foundation
import Combine
import SwiftUI

// UserListViewModel — ObservableObject view-model exercising:
//   Combine publishers and operators:
//     .map, .filter, .flatMap, .compactMap, .sink, .assign, .receive,
//     .debounce, .removeDuplicates, .eraseToAnyPublisher, .store,
//     .catch, .retry, .timeout, .throttle
//   @Published property wrappers
//   Result type methods

class UserListViewModel: ObservableObject {
    @Published var users: [User] = []
    @Published var filteredUsers: [User] = []
    @Published var isLoading: Bool = false
    @Published var errorMessage: String? = nil

    private var cancellables: Set<AnyCancellable> = []
    private let userService: UserService

    init(userService: UserService = UserService()) {
        self.userService = userService
        setupBindings()
    }

    private func setupBindings() {
        // .debounce + .removeDuplicates + .sink are Combine operators on
        // external Publisher types — the resolver can't bind these.
        userService.usersPublisher
            .receive(on: DispatchQueue.main)
            .sink(receiveCompletion: handleCompletion(_:),
                  receiveValue: handleUsers(_:))
            .store(in: &cancellables)
    }

    func fetchUsers() {
        isLoading = true
        userService.fetchAll()
            .map { $0.filter { $0.isActive } }
            .receive(on: DispatchQueue.main)
            .sink(receiveCompletion: { [weak self] completion in
                self?.isLoading = false
                switch completion {
                case .failure(let error):
                    self?.errorMessage = error.localizedDescription
                case .finished:
                    break
                }
            }, receiveValue: { [weak self] users in
                self?.users = users
            })
            .store(in: &cancellables)
    }

    func applyFilter(_ query: String) {
        filteredUsers = users
            .filter { $0.name.lowercased().contains(query.lowercased()) }
            .sorted { $0.name < $1.name }
    }

    func deleteUser(at offsets: IndexSet) {
        users.remove(atOffsets: offsets)
        filteredUsers = users
    }

    private func handleCompletion(_ completion: Subscribers.Completion<Error>) {
        switch completion {
        case .failure(let error):
            errorMessage = error.localizedDescription
        case .finished:
            break
        }
    }

    private func handleUsers(_ incoming: [User]) {
        users = incoming
        filteredUsers = incoming
    }
}
