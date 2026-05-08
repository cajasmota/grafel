// Source: https://github.com/flutter/samples (synthetic based on real Flutter service patterns) | License: BSD-3-Clause

import 'dart:convert';
import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

class ApiException implements Exception {
  final int statusCode;
  final String message;

  const ApiException({required this.statusCode, required this.message});

  @override
  String toString() => 'ApiException($statusCode): $message';
}

class ApiService {
  static const String _baseUrl = String.fromEnvironment(
    'API_BASE_URL',
    defaultValue: 'https://api.example.com/v1',
  );

  final http.Client _client;
  final SharedPreferences _prefs;

  ApiService({required http.Client client, required SharedPreferences prefs})
      : _client = client,
        _prefs = prefs;

  String? get _authToken => _prefs.getString('auth_token');

  Map<String, String> get _headers => {
        'Content-Type': 'application/json',
        'Accept': 'application/json',
        if (_authToken != null) 'Authorization': 'Bearer $_authToken',
      };

  Future<T> get<T>(
    String path, {
    Map<String, String>? queryParams,
    required T Function(Map<String, dynamic>) fromJson,
  }) async {
    final uri = Uri.parse('$_baseUrl$path').replace(queryParameters: queryParams);

    try {
      final response = await _client.get(uri, headers: _headers);
      return _handleResponse(response, fromJson);
    } on SocketException {
      throw const ApiException(statusCode: 0, message: 'No internet connection');
    }
  }

  Future<T> post<T>(
    String path, {
    required Map<String, dynamic> body,
    required T Function(Map<String, dynamic>) fromJson,
  }) async {
    final uri = Uri.parse('$_baseUrl$path');

    try {
      final response = await _client.post(
        uri,
        headers: _headers,
        body: jsonEncode(body),
      );
      return _handleResponse(response, fromJson);
    } on SocketException {
      throw const ApiException(statusCode: 0, message: 'No internet connection');
    }
  }

  Future<T> put<T>(
    String path, {
    required Map<String, dynamic> body,
    required T Function(Map<String, dynamic>) fromJson,
  }) async {
    final uri = Uri.parse('$_baseUrl$path');

    try {
      final response = await _client.put(
        uri,
        headers: _headers,
        body: jsonEncode(body),
      );
      return _handleResponse(response, fromJson);
    } on SocketException {
      throw const ApiException(statusCode: 0, message: 'No internet connection');
    }
  }

  Future<void> delete(String path) async {
    final uri = Uri.parse('$_baseUrl$path');

    try {
      final response = await _client.delete(uri, headers: _headers);
      if (response.statusCode != 204 && response.statusCode != 200) {
        _throwFromResponse(response);
      }
    } on SocketException {
      throw const ApiException(statusCode: 0, message: 'No internet connection');
    }
  }

  T _handleResponse<T>(
    http.Response response,
    T Function(Map<String, dynamic>) fromJson,
  ) {
    if (response.statusCode >= 200 && response.statusCode < 300) {
      final json = jsonDecode(response.body) as Map<String, dynamic>;
      return fromJson(json);
    }
    _throwFromResponse(response);
  }

  Never _throwFromResponse(http.Response response) {
    String message = 'Unknown error';
    try {
      final body = jsonDecode(response.body) as Map<String, dynamic>;
      message = body['message'] as String? ?? message;
    } catch (_) {}
    throw ApiException(statusCode: response.statusCode, message: message);
  }

  Future<void> saveToken(String token) async {
    await _prefs.setString('auth_token', token);
  }

  Future<void> clearToken() async {
    await _prefs.remove('auth_token');
  }
}

// ChangeNotifier-based state management
class TodoProvider extends ChangeNotifier {
  final ApiService _api;

  TodoProvider(this._api);

  List<Todo> _todos = [];
  bool _isLoading = false;
  String? _error;

  List<Todo> get todos => List.unmodifiable(_todos);
  bool get isLoading => _isLoading;
  String? get error => _error;

  Future<void> fetchTodos() async {
    _isLoading = true;
    _error = null;
    notifyListeners();

    try {
      final result = await _api.get<List<Todo>>(
        '/todos',
        fromJson: (json) =>
            (json['items'] as List).map((e) => Todo.fromJson(e as Map<String, dynamic>)).toList(),
      );
      _todos = result;
    } on ApiException catch (e) {
      _error = e.message;
    } finally {
      _isLoading = false;
      notifyListeners();
    }
  }

  Future<void> createTodo(String title) async {
    final todo = await _api.post<Todo>(
      '/todos',
      body: {'title': title},
      fromJson: Todo.fromJson,
    );
    _todos = [todo, ..._todos];
    notifyListeners();
  }

  Future<void> toggleTodo(String id) async {
    final index = _todos.indexWhere((t) => t.id == id);
    if (index == -1) return;

    final updated = await _api.put<Todo>(
      '/todos/$id',
      body: {'isComplete': !_todos[index].isComplete},
      fromJson: Todo.fromJson,
    );
    _todos = List.from(_todos)..[index] = updated;
    notifyListeners();
  }
}

class Todo {
  final String id;
  final String title;
  final bool isComplete;

  const Todo({required this.id, required this.title, required this.isComplete});

  factory Todo.fromJson(Map<String, dynamic> json) => Todo(
        id: json['id'] as String,
        title: json['title'] as String,
        isComplete: json['isComplete'] as bool? ?? false,
      );
}
