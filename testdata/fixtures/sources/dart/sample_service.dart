// Sample Dart service — golden fixture source.
class User {
  final int id;
  final String name;
  final String email;

  const User({required this.id, required this.name, required this.email});

  Map<String, dynamic> toJson() => {'id': id, 'name': name, 'email': email};
}

class CreateUserRequest {
  final String name;
  final String email;

  const CreateUserRequest({required this.name, required this.email});
}

class UserService {
  final List<User> _users = [const User(id: 1, name: 'Alice', email: 'alice@example.com')];
  int _nextId = 2;

  List<User> findAll() => List.unmodifiable(_users);

  User? findById(int id) {
    try {
      return _users.firstWhere((u) => u.id == id);
    } catch (_) {
      return null;
    }
  }

  User create(CreateUserRequest request) {
    final user = User(id: _nextId++, name: request.name, email: request.email);
    _users.add(user);
    return user;
  }

  bool delete(int id) {
    final before = _users.length;
    _users.removeWhere((u) => u.id == id);
    return _users.length < before;
  }
}

abstract class Repository<T, ID> {
  T? findById(ID id);
  List<T> findAll();
  T save(T entity);
  bool delete(ID id);
}
