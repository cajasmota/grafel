// Tests for the Phase 2A payload-shape sniffers — T3 languages (#2777).
// One canonical test per language that ships a sniffer, verifying the
// inline-literal cases the drift detector relies on. Mirrors
// payload_shapes_test.go (T1) and payload_shapes_t2_test.go (T2).
//
// Languages with N/A status are not tested here (no sniffer to test).
package substrate

import (
	"reflect"
	"testing"
)

func TestPayloadShapesSwift_VaporDecodable(t *testing.T) {
	const src = `
import Vapor

struct CreateUser: Content {
    var name: String
    var email: String
    var phone: String?
}

func createUser(req: Request) throws -> EventLoopFuture<User> {
    let body = try req.content.decode(CreateUser.self)
    return User.create(body: body)
}
`
	shapes := sniffPayloadShapesSwift(src)
	req := findShape(shapes, "createUser", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected swift Vapor request shape; got %+v", shapes)
	}
	want := []string{"email", "name", "phone"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("swift request fields: want %v got %v", want, got)
	}
	// phone is String? → Optional=true.
	for _, f := range req.Fields {
		if f.Name == "phone" && !f.Optional {
			t.Errorf("phone should be Optional=true; got %+v", f)
		}
	}
	if req.Confidence != 1.0 {
		t.Errorf("swift request confidence: want 1.0 got %v", req.Confidence)
	}
}

func TestPayloadShapesSwift_ResponseReturn(t *testing.T) {
	const src = `
import Vapor

struct UserResponse: Content {
    var id: Int
    var name: String
}

func getUser(req: Request) throws -> UserResponse {
    return UserResponse(id: 1, name: "Alice")
}
`
	shapes := sniffPayloadShapesSwift(src)
	resp := findShape(shapes, "getUser", PayloadDirectionResponse, PayloadSideProducer)
	if resp == nil {
		t.Fatalf("expected swift Vapor response shape; got %+v", shapes)
	}
	want := []string{"id", "name"}
	if got := sortedNames(resp.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("swift response fields: want %v got %v", want, got)
	}
}

func TestPayloadShapesSolidity_ExternalFunction(t *testing.T) {
	const src = `
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Token {
    function transfer(address to, uint256 amount) external returns (bool) {
        return true;
    }

    function approve(address spender, uint256 amount) public returns (bool) {
        return true;
    }

    function internalHelper(uint256 x) internal returns (uint256) {
        return x;
    }
}
`
	shapes := sniffPayloadShapesSolidity(src)
	transfer := findShape(shapes, "transfer", PayloadDirectionRequest, PayloadSideProducer)
	if transfer == nil {
		t.Fatalf("expected solidity external function shape; got %+v", shapes)
	}
	want := []string{"amount", "to"}
	if got := sortedNames(transfer.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("solidity transfer fields: want %v got %v", want, got)
	}
	// Verify types are captured.
	for _, f := range transfer.Fields {
		if f.Name == "to" && f.Type != "address" {
			t.Errorf("to field type: want address got %q", f.Type)
		}
		if f.Name == "amount" && f.Type != "uint256" {
			t.Errorf("amount field type: want uint256 got %q", f.Type)
		}
	}
	// internalHelper should not be captured (not external/public).
	helper := findShape(shapes, "internalHelper", PayloadDirectionRequest, PayloadSideProducer)
	if helper != nil {
		t.Errorf("internalHelper should not be captured (internal): %+v", helper)
	}
	// approve (public) should be captured.
	approve := findShape(shapes, "approve", PayloadDirectionRequest, PayloadSideProducer)
	if approve == nil {
		t.Fatalf("expected solidity public function shape; got %+v", shapes)
	}
}

func TestPayloadShapesCrystal_KemalJSONParams(t *testing.T) {
	const src = `
require "kemal"

def create_user(env)
  name = env.params.json["name"]
  email = env.params.json["email"]
  role = env.params.body["role"]
  env.response.print { id: 1, name: name }
end
`
	shapes := sniffPayloadShapesCrystal(src)
	req := findShape(shapes, "create_user", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected crystal Kemal request shape; got %+v", shapes)
	}
	want := []string{"email", "name", "role"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("crystal request fields: want %v got %v", want, got)
	}
	resp := findShape(shapes, "create_user", PayloadDirectionResponse, PayloadSideProducer)
	if resp == nil {
		t.Fatalf("expected crystal response shape; got %+v", shapes)
	}
	wantR := []string{"id", "name"}
	if got := sortedNames(resp.Fields); !reflect.DeepEqual(got, wantR) {
		t.Errorf("crystal response fields: want %v got %v", wantR, got)
	}
}

func TestPayloadShapesNim_PrologueQueryParams(t *testing.T) {
	const src = `
import prologue

proc createUser(ctx: Context) {.async.} =
  let name = ctx.getQueryParam("name")
  let email = ctx.getQueryParam("email")
  let role = ctx.getFormParam("role")
`
	shapes := sniffPayloadShapesNim(src)
	req := findShape(shapes, "createUser", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected nim Prologue request shape; got %+v", shapes)
	}
	want := []string{"email", "name", "role"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("nim request fields: want %v got %v", want, got)
	}
}

func TestPayloadShapesFSharp_GiraffebindJson(t *testing.T) {
	const src = `
open Giraffe

type CreateUser = {
    Name: string
    Email: string
    Phone: string option
}

let createUserHandler : HttpHandler =
    fun next ctx ->
        task {
            let! dto = ctx.BindJsonAsync<CreateUser>()
            return! Successful.OK dto next ctx
        }

let webApp =
    choose [
        POST >=> route "/users" >=> bindJson<CreateUser> createUserHandler
    ]
`
	shapes := sniffPayloadShapesFSharp(src)
	req := findShape(shapes, "webApp", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		// Try alternate attribution — bindJson is at module level.
		for _, s := range shapes {
			if s.Direction == PayloadDirectionRequest && s.Side == PayloadSideProducer {
				req = &s
				break
			}
		}
	}
	if req == nil {
		t.Fatalf("expected fsharp Giraffe request shape; got %+v", shapes)
	}
	want := []string{"Email", "Name", "Phone"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("fsharp request fields: want %v got %v", want, got)
	}
	// Phone is option → Optional=true.
	for _, f := range req.Fields {
		if f.Name == "Phone" && !f.Optional {
			t.Errorf("Phone should be Optional=true; got %+v", f)
		}
	}
}

func TestPayloadShapesClojure_RingDestructuring(t *testing.T) {
	const src = `
(ns app.handlers
  (:require [ring.util.response :as response]))

(defn create-user [request]
  (let [body (:body request)
        {:keys [name email]} body]
    (response/ok {:id 1 :name name :email email})))
`
	shapes := sniffPayloadShapesClojure(src)
	req := findShape(shapes, "create-user", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected clojure Ring request shape; got %+v", shapes)
	}
	wantReq := []string{"email", "name"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, wantReq) {
		t.Errorf("clojure request fields: want %v got %v", wantReq, got)
	}
	resp := findShape(shapes, "create-user", PayloadDirectionResponse, PayloadSideProducer)
	if resp == nil {
		t.Fatalf("expected clojure Ring response shape; got %+v", shapes)
	}
	wantResp := []string{"email", "id", "name"}
	if got := sortedNames(resp.Fields); !reflect.DeepEqual(got, wantResp) {
		t.Errorf("clojure response fields: want %v got %v", wantResp, got)
	}
}

func TestPayloadShapesVue_ScriptBlock(t *testing.T) {
	const src = `<template>
  <form @submit.prevent="submit">
    <input v-model="form.name" />
  </form>
</template>
<script setup lang="ts">
import { ref } from 'vue';
import axios from 'axios';

async function submit() {
  await axios.post('/api/users', { name: 'Alice', email: 'a@b.com' });
}
</script>`
	shapes := sniffPayloadShapesMarkupScript(src)
	// Should have at least one consumer shape from axios.post.
	var consumer *PayloadShape
	for i := range shapes {
		if shapes[i].Side == PayloadSideConsumer {
			consumer = &shapes[i]
			break
		}
	}
	if consumer == nil {
		t.Fatalf("expected vue markup consumer shape; got %+v", shapes)
	}
	want := []string{"email", "name"}
	if got := sortedNames(consumer.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("vue consumer fields: want %v got %v", want, got)
	}
	// Lines should be offset into the original markup.
	if consumer.Line < 7 {
		t.Errorf("consumer line should be offset into markup (script starts line 6): got %d", consumer.Line)
	}
}

func TestPayloadShapesSvelte_ScriptBlock(t *testing.T) {
	const src = `<script lang="ts">
  import axios from 'axios';

  async function login() {
    await axios.post('/api/login', { username: 'x', password: 'y' });
  }
</script>
<main>hello</main>`
	shapes := sniffPayloadShapesMarkupScript(src)
	var consumer *PayloadShape
	for i := range shapes {
		if shapes[i].Side == PayloadSideConsumer {
			consumer = &shapes[i]
			break
		}
	}
	if consumer == nil {
		t.Fatalf("expected svelte consumer shape; got %+v", shapes)
	}
	want := []string{"password", "username"}
	if got := sortedNames(consumer.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("svelte consumer fields: want %v got %v", want, got)
	}
	if consumer.EndpointHint != "/api/login" {
		t.Errorf("svelte consumer endpoint hint: want /api/login got %q", consumer.EndpointHint)
	}
}
