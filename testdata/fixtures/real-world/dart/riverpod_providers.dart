// Synthetic Dart fixture — exercises common Flutter/Dart stdlib patterns
// with Riverpod state management, dart:async, and dart:convert.
// License: MIT (synthetic)

import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

// ---------- Providers ----------

final userRepositoryProvider = Provider<UserRepository>((ref) => UserRepository());

final userListProvider = FutureProvider<List<User>>((ref) async {
  final repo = ref.watch(userRepositoryProvider);
  return repo.fetchAll();
});

final authStateProvider = StateNotifierProvider<AuthNotifier, AuthState>(
  (ref) => AuthNotifier(),
);

final settingsProvider = StateProvider<AppSettings>((ref) => const AppSettings());

// ---------- Models ----------

class User {
  final String id;
  final String name;
  final String email;

  const User({required this.id, required this.name, required this.email});

  factory User.fromJson(Map<String, dynamic> json) => User(
        id: json['id'] as String,
        name: json['name'] as String,
        email: json['email'] as String,
      );

  Map<String, dynamic> toJson() => {'id': id, 'name': name, 'email': email};

  @override
  String toString() => 'User(id: $id, name: $name)';

  @override
  bool operator ==(Object other) =>
      identical(this, other) || other is User && runtimeType == other.runtimeType && id == other.id;

  @override
  int get hashCode => id.hashCode;
}

class AppSettings {
  final bool darkMode;
  final String locale;

  const AppSettings({this.darkMode = false, this.locale = 'en'});

  AppSettings copyWith({bool? darkMode, String? locale}) =>
      AppSettings(darkMode: darkMode ?? this.darkMode, locale: locale ?? this.locale);
}

// ---------- Repository ----------

class UserRepository {
  final _users = <User>[];

  Future<List<User>> fetchAll() async {
    await Future.delayed(const Duration(milliseconds: 100));
    return List.unmodifiable(_users);
  }

  Future<User?> findById(String id) async {
    return _users.firstWhere((u) => u.id == id, orElse: () => throw StateError('not found'));
  }

  Future<void> save(User user) async {
    final index = _users.indexWhere((u) => u.id == user.id);
    if (index >= 0) {
      _users[index] = user;
    } else {
      _users.add(user);
    }
  }

  Future<void> delete(String id) async {
    _users.removeWhere((u) => u.id == id);
  }

  String serialize(List<User> users) => jsonEncode(users.map((u) => u.toJson()).toList());

  List<User> deserialize(String json) {
    final list = jsonDecode(json) as List<dynamic>;
    return list.map((e) => User.fromJson(e as Map<String, dynamic>)).toList();
  }
}

// ---------- Auth ----------

enum AuthStatus { unauthenticated, loading, authenticated, error }

class AuthState {
  final AuthStatus status;
  final User? user;
  final String? errorMessage;

  const AuthState({
    this.status = AuthStatus.unauthenticated,
    this.user,
    this.errorMessage,
  });

  AuthState copyWith({AuthStatus? status, User? user, String? errorMessage}) =>
      AuthState(
        status: status ?? this.status,
        user: user ?? this.user,
        errorMessage: errorMessage ?? this.errorMessage,
      );
}

class AuthNotifier extends StateNotifier<AuthState> {
  AuthNotifier() : super(const AuthState());

  Future<void> login(String email, String password) async {
    state = state.copyWith(status: AuthStatus.loading);
    try {
      await Future.delayed(const Duration(seconds: 1));
      final user = User(id: '1', name: 'Test', email: email);
      state = state.copyWith(status: AuthStatus.authenticated, user: user);
    } catch (e) {
      state = state.copyWith(status: AuthStatus.error, errorMessage: e.toString());
    }
  }

  void logout() {
    state = const AuthState();
  }
}

// ---------- Utilities ----------

class JsonCache {
  final _cache = <String, String>{};

  void put(String key, Object value) {
    _cache[key] = jsonEncode(value);
  }

  T? get<T>(String key, T Function(dynamic) fromJson) {
    final raw = _cache[key];
    if (raw == null) return null;
    return fromJson(jsonDecode(raw));
  }

  void invalidate(String key) {
    _cache.remove(key);
  }

  void clear() {
    _cache.clear();
  }
}

class StreamController<T> {
  final _controller = StreamController<T>.broadcast();

  Stream<T> get stream => _controller.stream;

  void add(T event) {
    _controller.add(event);
  }

  Future<void> close() async {
    await _controller.close();
  }
}

Uint8List encodeUtf8(String s) => utf8.encode(s);
String decodeUtf8(Uint8List bytes) => utf8.decode(bytes);
