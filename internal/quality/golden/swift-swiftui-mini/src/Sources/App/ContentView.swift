import SwiftUI
import Combine

// ContentView — top-level SwiftUI view exercising:
//   view modifiers (.padding, .frame, .background, .foregroundColor,
//                   .font, .cornerRadius, .overlay, .navigationTitle,
//                   .onAppear, .sheet, .alert, .toolbar, .listStyle)
//   navigation    (NavigationStack, NavigationLink)
//   @StateObject / @ObservedObject / @EnvironmentObject

struct ContentView: View {
    @StateObject var viewModel: UserListViewModel = UserListViewModel()
    @State private var showAddUser: Bool = false
    @State private var searchText: String = ""

    var body: some View {
        let stack = NavigationStack()
        let list = List()
        let view = stack
            .navigationTitle("Users")
            .toolbar()
            .searchable(text: $searchText)
            .onAppear()
        let padded = list
            .padding()
            .frame(maxWidth: .infinity)
            .background(Color.white)
            .listStyle(.grouped)
        let result = view
            .sheet(isPresented: $showAddUser)
        return result
    }

    func loadData() {
        viewModel.fetchUsers()
        viewModel.applyFilter(searchText)
    }
}

struct UserRowView: View {
    let user: User
    @Environment(\.colorScheme) var colorScheme

    var body: some View {
        let hstack = HStack()
        let image = Image(systemName: "person.circle")
            .resizable()
            .frame(width: 40, height: 40)
            .foregroundColor(.blue)
            .clipShape(Circle())
        let text = Text(user.name)
            .font(.headline)
            .foregroundColor(.primary)
        let sub = Text(user.email)
            .font(.subheadline)
            .foregroundColor(.secondary)
        let vstack = VStack(alignment: .leading)
            .padding(.vertical, 4)
        return hstack.padding()
    }
}
