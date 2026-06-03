// Payload-shape sniffer proving tests for Dart (Phase 2A T3 — #4035).
//
// Cells proven from missing → partial:
//   - request_shape_extraction  (Dio/http post body → consumer request shape)
//   - response_shape_extraction (X.fromJson of a @JsonSerializable DTO →
//     consumer response shape)
//
// VALUE-ASSERTING: asserts the SPECIFIC field set + direction + side on
// the SPECIFIC Dart construct, plus negatives (plain class → no DTO; no
// post body → no request shape).
package substrate

import "testing"

func fieldSet(fields []PayloadField) map[string]PayloadField {
	m := map[string]PayloadField{}
	for _, f := range fields {
		m[f.Name] = f
	}
	return m
}

func TestDartPayload_JsonSerializableDTO_ResponseShape(t *testing.T) {
	src := `
import 'package:json_annotation/json_annotation.dart';

@JsonSerializable()
class User {
  final int id;
  final String name;
  final String? email;

  User({required this.id, required this.name, this.email});

  factory User.fromJson(Map<String, dynamic> json) => _$UserFromJson(json);
  Map<String, dynamic> toJson() => _$UserToJson(this);
}

Future<User> fetchUser() async {
  final resp = await dio.get('/users/1');
  return User.fromJson(resp.data);
}
`
	shapes := sniffPayloadShapesDart(src)
	var resp *PayloadShape
	for i := range shapes {
		if shapes[i].Direction == PayloadDirectionResponse {
			resp = &shapes[i]
		}
	}
	if resp == nil {
		t.Fatalf("expected a response shape from User.fromJson, got %+v", shapes)
	}
	if resp.Side != PayloadSideConsumer {
		t.Errorf("expected consumer side, got %q", resp.Side)
	}
	if resp.Function != "fetchUser" {
		t.Errorf("expected shape attributed to fetchUser, got %q", resp.Function)
	}
	fs := fieldSet(resp.Fields)
	if len(fs) != 3 {
		t.Errorf("expected exactly 3 fields {id,name,email}, got %d: %+v", len(fs), resp.Fields)
	}
	if f, ok := fs["id"]; !ok || f.Type != "int" {
		t.Errorf("expected field id:int, got %+v", f)
	}
	if f, ok := fs["name"]; !ok || f.Type != "String" {
		t.Errorf("expected field name:String, got %+v", f)
	}
	if f, ok := fs["email"]; !ok || !f.Optional || f.Type != "String" {
		t.Errorf("expected field email:String optional, got %+v", f)
	}
}

func TestDartPayload_DioPostBody_RequestShape(t *testing.T) {
	src := `
Future<void> createUser(String name, String email) async {
  await dio.post('/users', data: {'name': name, 'email': email});
}
`
	shapes := sniffPayloadShapesDart(src)
	var req *PayloadShape
	for i := range shapes {
		if shapes[i].Direction == PayloadDirectionRequest {
			req = &shapes[i]
		}
	}
	if req == nil {
		t.Fatalf("expected a request shape from dio.post body, got %+v", shapes)
	}
	if req.Side != PayloadSideConsumer {
		t.Errorf("expected consumer side, got %q", req.Side)
	}
	if req.VerbHint != "POST" {
		t.Errorf("expected VerbHint POST, got %q", req.VerbHint)
	}
	if req.Function != "createUser" {
		t.Errorf("expected attribution to createUser, got %q", req.Function)
	}
	fs := fieldSet(req.Fields)
	if len(fs) != 2 {
		t.Errorf("expected exactly 2 fields {name,email}, got %d: %+v", len(fs), req.Fields)
	}
	if _, ok := fs["name"]; !ok {
		t.Error("expected request field name")
	}
	if _, ok := fs["email"]; !ok {
		t.Error("expected request field email")
	}
}

func TestDartPayload_ToJsonBody_RequestShape(t *testing.T) {
	src := `
@JsonSerializable()
class User {
  final int id;
  final String name;
  factory User.fromJson(Map<String, dynamic> json) => _$UserFromJson(json);
  Map<String, dynamic> toJson() => _$UserToJson(this);
}

Future<void> saveUser(User user) async {
  await dio.put('/users/1', data: user.toJson());
}
`
	shapes := sniffPayloadShapesDart(src)
	var req *PayloadShape
	for i := range shapes {
		if shapes[i].Direction == PayloadDirectionRequest {
			req = &shapes[i]
		}
	}
	if req == nil {
		t.Fatalf("expected a request shape from user.toJson() body, got %+v", shapes)
	}
	if req.VerbHint != "PUT" {
		t.Errorf("expected VerbHint PUT, got %q", req.VerbHint)
	}
	fs := fieldSet(req.Fields)
	if _, ok := fs["id"]; !ok {
		t.Errorf("expected DTO field id from user.toJson(), got %+v", req.Fields)
	}
	if _, ok := fs["name"]; !ok {
		t.Errorf("expected DTO field name from user.toJson(), got %+v", req.Fields)
	}
}

// Negative: a plain class with NO @JsonSerializable / @freezed annotation
// is not a wire DTO — fromJson on it must yield no response shape.
func TestDartPayload_Negative_PlainClassNoDTO(t *testing.T) {
	src := `
class Widget {
  final int count;
  Widget(this.count);
}

Future<Widget> load() async {
  final resp = await dio.get('/w');
  return Widget.fromJson(resp.data);
}
`
	shapes := sniffPayloadShapesDart(src)
	for _, s := range shapes {
		if s.Direction == PayloadDirectionResponse {
			t.Errorf("plain (un-annotated) class must not yield a DTO response shape, got %+v", s)
		}
	}
}

// Negative: a GET with no body must not produce a request shape.
func TestDartPayload_Negative_GetNoBody(t *testing.T) {
	src := `
Future<void> ping() async {
  await dio.get('/health');
}
`
	shapes := sniffPayloadShapesDart(src)
	if len(shapes) != 0 {
		t.Errorf("GET with no body must yield no shapes, got %+v", shapes)
	}
}

func TestDartPayload_Empty(t *testing.T) {
	if s := sniffPayloadShapesDart(""); len(s) != 0 {
		t.Errorf("expected no shapes for empty input, got %d", len(s))
	}
}
