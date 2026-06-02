package engine

import "testing"

// TestSwiftClient_AlamofireRequest covers Alamofire `AF.request("...")` (GET
// default) and `AF.request("...", method: .post)`, asserting the SPECIFIC
// canonical paths + verbs and FETCHES edges (value-asserting; host stripped).
func TestSwiftClient_AlamofireRequest(t *testing.T) {
	src := `
import Alamofire

func fetchUsers() {
    AF.request("https://api.example.com/v1/users")
}

func login(email: String) {
    AF.request("/auth/login", method: .post)
}
`
	ids, rels := runDetectWithRels(t, "swift", "Sources/ApiClient.swift", src)
	requireContains(t, ids, []string{
		"http:GET:/v1/users",
		"http:POST:/auth/login",
	}, "alamofire")
	requireFetches(t, rels, "http:GET:/v1/users", "alamofire")
	requireFetches(t, rels, "http:POST:/auth/login", "alamofire")
}

// TestSwiftClient_AlamofireInterpolated covers an Alamofire path with a Swift
// `\(id)` interpolation → `{param}` placeholder and a PUT verb, asserting the
// exact path /inspections/{param}.
func TestSwiftClient_AlamofireInterpolated(t *testing.T) {
	src := `
import Alamofire

func updateInspection(id: String) {
    session.request("/inspections/\(id)", method: .put)
}
`
	ids, _ := runDetectWithRels(t, "swift", "Sources/Inspections.swift", src)
	requireContains(t, ids, []string{"http:PUT:/inspections/{param}"}, "alamofire-interp")
}

// TestSwiftClient_URLSession covers URLSession via `URL(string: "...")` with a
// `request.httpMethod = "POST"` assignment, asserting POST /auth/login (host
// stripped) and a GET default for a bare dataTask URL.
func TestSwiftClient_URLSession(t *testing.T) {
	src := `
import Foundation

func loadUsers() {
    let url = URL(string: "https://api.example.com/v1/users")!
    URLSession.shared.dataTask(with: url).resume()
}

func signIn(email: String) {
    var request = URLRequest(url: URL(string: "/auth/login")!)
    request.httpMethod = "POST"
    URLSession.shared.dataTask(with: request).resume()
}
`
	ids, rels := runDetectWithRels(t, "swift", "Sources/Networking.swift", src)
	requireContains(t, ids, []string{
		"http:GET:/v1/users",
		"http:POST:/auth/login",
	}, "urlsession")
	requireFetches(t, rels, "http:POST:/auth/login", "urlsession")
}

// TestSwiftClient_NoMatch asserts a Swift file with no HTTP-client call produces
// no http synthetic.
func TestSwiftClient_NoMatch(t *testing.T) {
	src := `
import SwiftUI

struct ContentView: View {
    var body: some View {
        Text("hello")
    }
}
`
	ids, _ := runDetectWithRels(t, "swift", "Sources/ContentView.swift", src)
	for _, id := range ids {
		if len(id) >= 5 && id[:5] == "http:" {
			t.Errorf("urlsession-no-match: unexpected http synthetic %q", id)
		}
	}
}
