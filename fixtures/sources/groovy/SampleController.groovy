// Sample Groovy controller — golden fixture source.
package com.example.api

class UserController {

    private List<Map> users = [
        [id: 1, name: 'Alice', email: 'alice@example.com']
    ]
    private int nextId = 2

    def index() {
        return [users: users]
    }

    def show(int id) {
        def user = users.find { it.id == id }
        if (!user) {
            return [error: 'Not found']
        }
        return user
    }

    def create(String name, String email) {
        def user = [id: nextId++, name: name, email: email]
        users << user
        return user
    }

    def delete(int id) {
        def before = users.size()
        users.removeAll { it.id == id }
        return users.size() < before
    }

    private boolean validateEmail(String email) {
        return email ==~ /^[^@]+@[^@]+\.[^@]+$/
    }
}

def handleRequest(String method, String path, UserController controller) {
    switch ([method, path]) {
        case ['GET', '/health']:
            return [status: 'ok']
        case ['GET', '/users']:
            return controller.index()
        default:
            return [error: 'not found']
    }
}
