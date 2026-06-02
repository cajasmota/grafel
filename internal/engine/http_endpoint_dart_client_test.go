package engine

import "testing"

// TestDartClient_DioLiteralGetPost covers the canonical Dio forms
// `dio.get("/users")` and `dio.post("/auth/login")`, asserting the SPECIFIC
// canonical paths + verbs and the FETCHES edges from the enclosing functions
// (value-asserting; not len>0).
func TestDartClient_DioLiteralGetPost(t *testing.T) {
	src := `
import 'package:dio/dio.dart';

class ApiClient {
  final Dio dio = Dio();

  Future<List<User>> fetchUsers() async {
    return dio.get("/users");
  }

  Future<Token> login(String email) async {
    return dio.post("/auth/login", data: {"email": email});
  }
}
`
	ids, rels := runDetectWithRels(t, "dart", "lib/api_client.dart", src)
	requireContains(t, ids, []string{
		"http:GET:/users",
		"http:POST:/auth/login",
	}, "dio-literal")
	requireFetches(t, rels, "http:GET:/users", "dio-literal")
	requireFetches(t, rels, "http:POST:/auth/login", "dio-literal")
}

// TestDartClient_DioInterpolatedPath covers a Dio path with a Dart
// string-interpolation `$id` → `{id}` placeholder, asserting the exact path
// /inspections/{id} (matching the backend route normalisation).
func TestDartClient_DioInterpolatedPath(t *testing.T) {
	src := `
import 'package:dio/dio.dart';

Future<Inspection> getInspection(String id) async {
  return dio.get("/inspections/$id");
}

Future<void> updateUser(User user) async {
  await dio.put("/users/${user.id}");
}
`
	ids, _ := runDetectWithRels(t, "dart", "lib/inspections.dart", src)
	requireContains(t, ids, []string{
		"http:GET:/inspections/{id}",
		"http:PUT:/users/{param}",
	}, "dio-interp")
}

// TestDartClient_HttpUriParse covers the package:http form
// `http.get(Uri.parse("https://api.example.com/v1/users"))` and a POST,
// asserting that the host is stripped to leave the producer route /v1/users.
func TestDartClient_HttpUriParse(t *testing.T) {
	src := `
import 'package:http/http.dart' as http;

Future<void> loadProfile() async {
  final res = await http.get(Uri.parse("https://api.example.com/v1/users"));
}

Future<void> signIn(String email) async {
  await http.post(Uri.parse("/auth/login"), body: {"email": email});
}
`
	ids, rels := runDetectWithRels(t, "dart", "lib/http_api.dart", src)
	requireContains(t, ids, []string{
		"http:GET:/v1/users",
		"http:POST:/auth/login",
	}, "http-uriparse")
	requireFetches(t, rels, "http:GET:/v1/users", "http-uriparse")
}

// TestDartClient_NoMatch asserts a Dart file with no HTTP-client call produces
// no http synthetic.
func TestDartClient_NoMatch(t *testing.T) {
	src := `
import 'package:flutter/material.dart';

class HomeScreen extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return const Text("hello");
  }
}
`
	ids, _ := runDetectWithRels(t, "dart", "lib/home_screen.dart", src)
	for _, id := range ids {
		if len(id) >= 5 && id[:5] == "http:" {
			t.Errorf("dio-no-match: unexpected http synthetic %q", id)
		}
	}
}
