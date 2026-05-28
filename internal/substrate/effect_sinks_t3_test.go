// Per-language smoke tests for the T3 (#2776) Phase 1A effect-sink sniffers.
// Each test asserts that the canonical sink primitives for that language are
// recognised and attributed to the correct function.
package substrate

import "testing"

// groupByEffect re-uses the helper from effects_test.go (same package).

func TestSniffEffectsDart_PrimitiveCoverage(t *testing.T) {
	const src = `
import 'package:http/http.dart' as http;
import 'package:sqflite/sqflite.dart';

class UserRepo {
  final Database db;

  Future<List<Map>> fetchUsers() async {
    return await db.query('users');
  }

  Future<void> saveUser(Map user) async {
    await db.insert('users', user);
    this.lastSaved = user;
  }

  Future<String> loadConfig() async {
    return await File('/etc/app.conf').readAsString();
  }

  Future<void> writeLog(String msg) async {
    await File('/var/log/app.log').writeAsString(msg);
  }

  Future<void> callAPI() async {
    final res = await http.get(Uri.parse("https://api.example.com/data"));
    this.cached = res.body;
  }
}
`
	got := sniffEffectsDart(src)
	if len(got) == 0 {
		t.Fatal("expected matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectDBRead, "fetchUsers")
	mustHave(t, byEffect, EffectDBWrite, "saveUser")
	mustHave(t, byEffect, EffectMutation, "saveUser")
	mustHave(t, byEffect, EffectFSRead, "loadConfig")
	mustHave(t, byEffect, EffectFSWrite, "writeLog")
	mustHave(t, byEffect, EffectHTTPOut, "callAPI")
}

func TestSniffEffectsSwift_PrimitiveCoverage(t *testing.T) {
	const src = `
import Foundation
import CoreData

class UserService {
    var context: NSManagedObjectContext!

    func fetchUsers() throws -> [User] {
        let request = NSFetchRequest<User>(entityName: "User")
        return try context.fetch(request)
    }

    func saveUser(_ user: User) throws {
        try context.save()
        self.lastSaved = user
    }

    func downloadData() async throws {
        let (data, _) = try await URLSession.shared.data(from: URL(string: "https://api.example.com")!)
        self.cache = data
    }

    func readConfig() throws -> String {
        return try String(contentsOfFile: "/etc/app.conf")
    }

    func writeLog(msg: String) throws {
        try msg.write(toFile: "/var/log/app.log", atomically: true, encoding: .utf8)
    }
}
`
	got := sniffEffectsSwift(src)
	if len(got) == 0 {
		t.Fatal("expected matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectDBRead, "fetchUsers")
	mustHave(t, byEffect, EffectDBWrite, "saveUser")
	mustHave(t, byEffect, EffectMutation, "saveUser")
	mustHave(t, byEffect, EffectHTTPOut, "downloadData")
	mustHave(t, byEffect, EffectFSRead, "readConfig")
	mustHave(t, byEffect, EffectFSWrite, "writeLog")
}

func TestSniffEffectsNim_PrimitiveCoverage(t *testing.T) {
	const src = `
import httpclient, db_sqlite, os

proc fetchData(): string =
  let client = newHttpClient()
  return client.get("https://api.example.com/data").body

proc queryUsers(db: DbConn): seq[Row] =
  return db.getAllRows(sql"SELECT * FROM users")

proc insertUser(db: DbConn, name: string) =
  db.exec(sql"INSERT INTO users(name) VALUES(?)", name)

proc loadConfig(): string =
  return readFile("/etc/app.conf")

proc writeLog(msg: string) =
  writeFile("/var/log/app.log", msg)
  result.count += 1
`
	got := sniffEffectsNim(src)
	if len(got) == 0 {
		t.Fatal("expected matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectHTTPOut, "fetchData")
	mustHave(t, byEffect, EffectDBRead, "queryUsers")
	mustHave(t, byEffect, EffectDBWrite, "insertUser")
	mustHave(t, byEffect, EffectFSRead, "loadConfig")
	mustHave(t, byEffect, EffectFSWrite, "writeLog")
}

func TestSniffEffectsCrystal_PrimitiveCoverage(t *testing.T) {
	const src = `
require "http/client"
require "db"

def fetch_data : String
  HTTP::Client.get("https://api.example.com").body
end

def query_users(db : DB::Database) : Array(DB::ResultSet)
  db.query("SELECT * FROM users")
end

def insert_user(db : DB::Database, name : String)
  db.exec("INSERT INTO users(name) VALUES(?)", name)
  @last_insert = name
end

def read_config : String
  File.read("/etc/app.conf")
end

def write_log(msg : String)
  File.write("/var/log/app.log", msg)
end
`
	got := sniffEffectsCrystal(src)
	if len(got) == 0 {
		t.Fatal("expected matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectHTTPOut, "fetch_data")
	mustHave(t, byEffect, EffectDBRead, "query_users")
	mustHave(t, byEffect, EffectDBWrite, "insert_user")
	mustHave(t, byEffect, EffectMutation, "insert_user")
	mustHave(t, byEffect, EffectFSRead, "read_config")
	mustHave(t, byEffect, EffectFSWrite, "write_log")
}

func TestSniffEffectsZig_PrimitiveCoverage(t *testing.T) {
	const src = `
const std = @import("std");

pub fn fetchData(allocator: std.mem.Allocator) ![]u8 {
    var client = std.http.Client{ .allocator = allocator };
    defer client.deinit();
    var req = try client.fetch(.{ .location = .{ .url = "https://api.example.com" } });
    return req.body;
}

pub fn readConfig(path: []const u8, allocator: std.mem.Allocator) ![]u8 {
    const file = try std.fs.cwd().openFile(path, .{});
    defer file.close();
    return file.readToEndAlloc(allocator, 4096);
}

pub fn writeLog(path: []const u8, msg: []const u8) !void {
    const file = try std.fs.cwd().createFile(path, .{});
    defer file.close();
    try file.writeAll(msg);
    self.count += 1;
}
`
	got := sniffEffectsZig(src)
	if len(got) == 0 {
		t.Fatal("expected matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectHTTPOut, "fetchData")
	mustHave(t, byEffect, EffectFSRead, "readConfig")
	mustHave(t, byEffect, EffectFSWrite, "writeLog")
}

func TestSniffEffectsSolidity_PrimitiveCoverage(t *testing.T) {
	const src = `
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IERC20 {
    function transfer(address to, uint256 amount) external returns (bool);
}

contract Vault {
    mapping(address => uint256) public balances;
    address public owner;

    function deposit(uint256 amount) external {
        balances[msg.sender] = balances[msg.sender] + amount;
    }

    function withdraw(address token, uint256 amount) external {
        IERC20(token).transfer(msg.sender, amount);
        balances[msg.sender] = balances[msg.sender] - amount;
    }

    function setOwner(address newOwner) external {
        owner = newOwner;
    }
}
`
	got := sniffEffectsSolidity(src)
	if len(got) == 0 {
		t.Fatal("expected matches; got none")
	}
	byEffect := groupByEffect(got)
	// Mapping writes and storage writes should be mutation.
	if _, hasMutation := byEffect[EffectMutation]; !hasMutation {
		t.Errorf("expected mutation effects for storage writes, got none")
	}
	// External call / transfer should map to http_out.
	if _, hasHTTP := byEffect[EffectHTTPOut]; !hasHTTP {
		t.Errorf("expected http_out for external CALL/transfer, got none")
	}
}

func TestSniffEffectsMarkupScript_Vue(t *testing.T) {
	const src = `<template>
  <div>{{ message }}</div>
</template>
<script setup lang="ts">
import axios from "axios";
import { ref } from "vue";

const message = ref("");

async function loadMessage() {
  const res = await axios.get("https://api.example.com/msg");
  message.value = res.data;
}

async function saveMessage(msg: string) {
  await axios.post("/api/msg", { msg });
}
</script>`
	got := sniffEffectsMarkupScript(src)
	if len(got) == 0 {
		t.Fatal("expected matches from Vue <script setup>; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectHTTPOut, "loadMessage")
	// Line numbers should be offset into the original file (script starts line 4).
	for _, m := range got {
		if m.Effect == EffectHTTPOut && m.Line < 4 {
			t.Errorf("http_out match line %d should be >= 4 (script block starts line 4)", m.Line)
		}
	}
}

func TestSniffEffectsMarkupScript_Svelte(t *testing.T) {
	const src = `<script>
  import { onMount } from "svelte";

  let users = [];

  onMount(async () => {
    const res = await fetch("/api/users");
    users = await res.json();
  });

  async function deleteUser(id) {
    await fetch("/api/users/" + id, { method: "DELETE" });
  }
</script>

<main>
  {#each users as user}
    <p>{user.name}</p>
  {/each}
</main>`
	got := sniffEffectsMarkupScript(src)
	if len(got) == 0 {
		t.Fatal("expected matches from Svelte <script>; got none")
	}
	byEffect := groupByEffect(got)
	if _, ok := byEffect[EffectHTTPOut]; !ok {
		t.Errorf("expected http_out (fetch) in Svelte script; got effects: %v", byEffect)
	}
}
