package substrate

import (
	"sort"
	"testing"
)

func TestEffectRegistry_T1Languages(t *testing.T) {
	for _, lang := range []string{"jsts", "python", "java", "go"} {
		if EffectSnifferFor(lang) == nil {
			t.Errorf("expected effect sniffer registered for %q", lang)
		}
	}
}

func TestEffectRegistry_T2Languages(t *testing.T) {
	for _, lang := range []string{"ruby", "php", "rust", "csharp", "kotlin", "elixir", "scala", "c-cpp"} {
		if EffectSnifferFor(lang) == nil {
			t.Errorf("expected effect sniffer registered for %q", lang)
		}
	}
}

func TestEffectRegistry_T3Languages(t *testing.T) {
	// Languages that have concrete effect-sink sniffers in Phase 1A T3.
	// The others are not_applicable (hardware langs, pure-FP, no corpus).
	for _, lang := range []string{"dart", "swift", "nim", "crystal", "zig", "solidity", "svelte", "vue", "astro"} {
		if EffectSnifferFor(lang) == nil {
			t.Errorf("expected T3 effect sniffer registered for %q", lang)
		}
	}
}

func TestEffectSet_AddUnion(t *testing.T) {
	var s EffectSet
	if !s.IsEmpty() {
		t.Fatal("zero EffectSet should be empty")
	}
	s.Add(EffectHTTPOut, 1.0, "fetch")
	if !s.Has(EffectHTTPOut) {
		t.Errorf("expected http_out present after Add")
	}
	if got := s.Confidence(EffectHTTPOut); got != 1.0 {
		t.Errorf("Confidence(http_out) = %v, want 1.0", got)
	}
	var other EffectSet
	other.Add(EffectDBRead, 0.8, "orm.read")
	s.Union(other)
	if !s.Has(EffectDBRead) {
		t.Errorf("expected db_read after Union")
	}
	// Add() with lower confidence should not lower the stored value.
	s.Add(EffectHTTPOut, 0.5, "fetch")
	if got := s.Confidence(EffectHTTPOut); got != 1.0 {
		t.Errorf("max-confidence semantics violated: got %v", got)
	}
}

func TestEffectSet_UnionScaled_DropsByHop(t *testing.T) {
	var direct EffectSet
	direct.Add(EffectDBRead, 1.0, "cursor.execute(SELECT)")
	var transitive EffectSet
	transitive.UnionScaled(direct, 0.95)
	c := transitive.Confidence(EffectDBRead)
	if c >= 1.0 || c <= 0.9 {
		t.Errorf("UnionScaled(scale=0.95) confidence = %v, want in (0.9, 1.0)", c)
	}
}

func TestSniffEffectsJSTS_PrimitiveCoverage(t *testing.T) {
	const src = `
import fs from "fs/promises";
import axios from "axios";

export async function loadAndPost(path) {
  const data = await fs.readFile(path, "utf8");
  await fs.writeFile(path + ".bak", data);
  const res = await fetch("https://api.example.com/things");
  await axios.post("/x", res);
  return res;
}

class Repo {
  setUser(u) {
    this.user = u;
  }
  async list() {
    return await this.model.findAll();
  }
  async save(x) {
    return await this.model.create(x);
  }
}
`
	got := sniffEffectsJSTS(src)
	if len(got) == 0 {
		t.Fatal("expected matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectHTTPOut, "loadAndPost")
	mustHave(t, byEffect, EffectFSRead, "loadAndPost")
	mustHave(t, byEffect, EffectFSWrite, "loadAndPost")
	mustHave(t, byEffect, EffectMutation, "setUser")
	mustHave(t, byEffect, EffectDBRead, "list")
	mustHave(t, byEffect, EffectDBWrite, "save")
}

func TestSniffEffectsPython_PrimitiveCoverage(t *testing.T) {
	const src = `
import requests
import os

class UserService:
    def fetch(self, uid):
        r = requests.get("https://api.example.com/u")
        return r.json()

    def load_users(self):
        return User.objects.filter(active=True)

    def save_user(self, u):
        u.save()

    def write_log(self, msg):
        with open("log.txt", "w") as f:
            f.write(msg)

    def assign(self, x):
        self.x = x

def read_config():
    with open("config.json") as f:
        return f.read()
`
	got := sniffEffectsPython(src)
	if len(got) == 0 {
		t.Fatal("expected python matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectHTTPOut, "fetch")
	mustHave(t, byEffect, EffectDBRead, "load_users")
	mustHave(t, byEffect, EffectDBWrite, "save_user")
	mustHave(t, byEffect, EffectFSWrite, "write_log")
	mustHave(t, byEffect, EffectFSRead, "read_config")
	mustHave(t, byEffect, EffectMutation, "assign")
}

func TestSniffEffectsJava_PrimitiveCoverage(t *testing.T) {
	const src = `
package x;

import java.nio.file.Files;

public class UserService {
    private RestTemplate restTemplate;
    private EntityManager em;

    public User load(Long id) {
        return em.find(User.class, id);
    }

    public void save(User u) {
        em.persist(u);
    }

    public String callRemote() {
        return restTemplate.getForObject("https://x", String.class);
    }

    public byte[] readFile(java.nio.file.Path p) throws Exception {
        return Files.readAllBytes(p);
    }

    public void writeFile(java.nio.file.Path p, byte[] data) throws Exception {
        Files.write(p, data);
    }

    public void setX(String x) {
        this.x = x;
    }
}
`
	got := sniffEffectsJava(src)
	if len(got) == 0 {
		t.Fatal("expected java matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectDBRead, "load")
	mustHave(t, byEffect, EffectDBWrite, "save")
	mustHave(t, byEffect, EffectHTTPOut, "callRemote")
	mustHave(t, byEffect, EffectFSRead, "readFile")
	mustHave(t, byEffect, EffectFSWrite, "writeFile")
	mustHave(t, byEffect, EffectMutation, "setX")
}

func TestSniffEffectsGo_PrimitiveCoverage(t *testing.T) {
	const src = `
package x

import (
	"net/http"
	"os"
)

type Repo struct { Name string }

func (r *Repo) Load(id int) (*User, error) {
	rows, err := db.Query("SELECT * FROM users WHERE id = ?", id)
	_ = rows
	return nil, err
}

func (r *Repo) Save(u *User) error {
	_, err := db.Exec("INSERT INTO users (name) VALUES (?)", u.Name)
	return err
}

func CallRemote() (*http.Response, error) {
	return http.Get("https://x")
}

func ReadConfig() ([]byte, error) {
	return os.ReadFile("config.json")
}

func WriteLog(b []byte) error {
	return os.WriteFile("log.txt", b, 0o644)
}

func (r *Repo) SetName(n string) {
	r.Name = n
}
`
	got := sniffEffectsGo(src)
	if len(got) == 0 {
		t.Fatal("expected go matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectDBRead, "Load")
	mustHave(t, byEffect, EffectDBWrite, "Save")
	mustHave(t, byEffect, EffectHTTPOut, "CallRemote")
	mustHave(t, byEffect, EffectFSRead, "ReadConfig")
	mustHave(t, byEffect, EffectFSWrite, "WriteLog")
	mustHave(t, byEffect, EffectMutation, "SetName")
}

func TestSniffEffectsRuby_PrimitiveCoverage(t *testing.T) {
	const src = `
require "net/http"

class UserService
  def call_remote
    Net::HTTP.get(URI("https://api.example.com/u"))
  end

  def load_users
    User.where(active: true)
  end

  def save_user(u)
    u.save!
  end

  def write_log(msg)
    File.write("log.txt", msg)
  end

  def read_config
    File.read("config.json")
  end

  def assign(x)
    @x = x
  end
end
`
	got := sniffEffectsRuby(src)
	if len(got) == 0 {
		t.Fatal("expected ruby matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectHTTPOut, "call_remote")
	mustHave(t, byEffect, EffectDBRead, "load_users")
	mustHave(t, byEffect, EffectDBWrite, "save_user")
	mustHave(t, byEffect, EffectFSWrite, "write_log")
	mustHave(t, byEffect, EffectFSRead, "read_config")
	mustHave(t, byEffect, EffectMutation, "assign")
}

func TestSniffEffectsPHP_PrimitiveCoverage(t *testing.T) {
	const src = `<?php
class UserService {
    public function callRemote() {
        $c = new GuzzleHttp\Client();
        return $c->get('https://api.example.com/u');
    }

    public function loadUsers() {
        return User::where('active', true)->get();
    }

    public function saveUser($u) {
        $u->save();
    }

    public function readConfig() {
        return file_get_contents('config.json');
    }

    public function writeLog($msg) {
        file_put_contents('log.txt', $msg);
    }

    public function assign($x) {
        $this->x = $x;
    }
}
`
	got := sniffEffectsPHP(src)
	if len(got) == 0 {
		t.Fatal("expected php matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectHTTPOut, "callRemote")
	mustHave(t, byEffect, EffectDBRead, "loadUsers")
	mustHave(t, byEffect, EffectDBWrite, "saveUser")
	mustHave(t, byEffect, EffectFSRead, "readConfig")
	mustHave(t, byEffect, EffectFSWrite, "writeLog")
	mustHave(t, byEffect, EffectMutation, "assign")
}

func TestSniffEffectsRust_PrimitiveCoverage(t *testing.T) {
	const src = `
use std::fs;

pub struct Svc { name: String }

impl Svc {
    pub async fn call_remote(&self) {
        let _ = reqwest::get("https://x").await;
    }

    pub async fn load_users(&self, pool: &Pool) {
        let _ = sqlx::query!("SELECT * FROM users").fetch_all(pool).await;
    }

    pub async fn save_user(&self, pool: &Pool) {
        let _ = sqlx::query!("INSERT INTO users (name) VALUES ($1)", "x").execute(pool).await;
    }

    pub fn read_config(&self) -> String {
        std::fs::read_to_string("config.json").unwrap()
    }

    pub fn write_log(&self, b: &[u8]) {
        std::fs::write("log.txt", b).unwrap();
    }

    pub fn set_name(&mut self, n: String) {
        self.name = n;
    }
}
`
	got := sniffEffectsRust(src)
	if len(got) == 0 {
		t.Fatal("expected rust matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectHTTPOut, "call_remote")
	mustHave(t, byEffect, EffectDBRead, "load_users")
	mustHave(t, byEffect, EffectDBWrite, "save_user")
	mustHave(t, byEffect, EffectFSRead, "read_config")
	mustHave(t, byEffect, EffectFSWrite, "write_log")
	mustHave(t, byEffect, EffectMutation, "set_name")
}

func TestSniffEffectsCSharp_PrimitiveCoverage(t *testing.T) {
	const src = `
using System.IO;
using System.Net.Http;

public class UserService {
    private HttpClient _httpClient = new HttpClient();
    public string name;

    public async Task<string> CallRemote() {
        return await _httpClient.GetStringAsync("https://x");
    }

    public async Task<List<User>> LoadUsers(DbContext ctx) {
        return await ctx.Users.Where(u => u.Active).ToListAsync();
    }

    public async Task SaveUser(DbContext ctx, User u) {
        ctx.Users.Add(u);
        await ctx.SaveChangesAsync();
    }

    public string ReadConfig() {
        return File.ReadAllText("config.json");
    }

    public void WriteLog(string msg) {
        File.WriteAllText("log.txt", msg);
    }

    public void SetName(string n) {
        this.name = n;
    }
}
`
	got := sniffEffectsCSharp(src)
	if len(got) == 0 {
		t.Fatal("expected csharp matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectHTTPOut, "CallRemote")
	mustHave(t, byEffect, EffectDBRead, "LoadUsers")
	mustHave(t, byEffect, EffectDBWrite, "SaveUser")
	mustHave(t, byEffect, EffectFSRead, "ReadConfig")
	mustHave(t, byEffect, EffectFSWrite, "WriteLog")
	mustHave(t, byEffect, EffectMutation, "SetName")
}

func TestSniffEffectsKotlin_PrimitiveCoverage(t *testing.T) {
	const src = `
import java.io.File
import java.nio.file.Files

class UserService {
    var name: String = ""

    suspend fun callRemote(client: HttpClient): String {
        return client.get("https://x")
    }

    fun loadUsers(em: EntityManager): List<User> {
        return em.createQuery("from User").resultList as List<User>
    }

    fun saveUser(em: EntityManager, u: User) {
        em.persist(u)
    }

    fun readConfig(): String {
        return File("config.json").readText()
    }

    fun writeLog(msg: String) {
        File("log.txt").writeText(msg)
    }

    fun setName(n: String) {
        this.name = n
    }
}
`
	got := sniffEffectsKotlin(src)
	if len(got) == 0 {
		t.Fatal("expected kotlin matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectHTTPOut, "callRemote")
	mustHave(t, byEffect, EffectDBRead, "loadUsers")
	mustHave(t, byEffect, EffectDBWrite, "saveUser")
	mustHave(t, byEffect, EffectFSRead, "readConfig")
	mustHave(t, byEffect, EffectFSWrite, "writeLog")
	mustHave(t, byEffect, EffectMutation, "setName")
}

func TestSniffEffectsElixir_PrimitiveCoverage(t *testing.T) {
	const src = `
defmodule UserService do
  def call_remote do
    HTTPoison.get("https://x")
  end

  def load_users do
    Repo.all(User)
  end

  def save_user(u) do
    Repo.insert(u)
  end

  def read_config do
    File.read!("config.json")
  end

  def write_log(msg) do
    File.write!("log.txt", msg)
  end

  def update_cache(k, v) do
    Agent.update(:cache, fn s -> Map.put(s, k, v) end)
  end
end
`
	got := sniffEffectsElixir(src)
	if len(got) == 0 {
		t.Fatal("expected elixir matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectHTTPOut, "call_remote")
	mustHave(t, byEffect, EffectDBRead, "load_users")
	mustHave(t, byEffect, EffectDBWrite, "save_user")
	mustHave(t, byEffect, EffectFSRead, "read_config")
	mustHave(t, byEffect, EffectFSWrite, "write_log")
	mustHave(t, byEffect, EffectMutation, "update_cache")
}

func TestSniffEffectsScala_PrimitiveCoverage(t *testing.T) {
	const src = `
import scala.io.Source
import java.nio.file.Files

class UserService {
  var name: String = ""

  def callRemote(): Unit = {
    basicRequest.get(uri"https://x").send(backend)
  }

  def loadUsers(em: EntityManager): List[User] = {
    em.createQuery("from User").getResultList.asInstanceOf[List[User]]
  }

  def saveUser(em: EntityManager, u: User): Unit = {
    em.persist(u)
  }

  def readConfig(): String = {
    Source.fromFile("config.json").mkString
  }

  def writeLog(msg: String): Unit = {
    Files.writeString(java.nio.file.Paths.get("log.txt"), msg)
  }

  def setName(n: String): Unit = {
    this.name = n
  }
}
`
	got := sniffEffectsScala(src)
	if len(got) == 0 {
		t.Fatal("expected scala matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectHTTPOut, "callRemote")
	mustHave(t, byEffect, EffectDBRead, "loadUsers")
	mustHave(t, byEffect, EffectDBWrite, "saveUser")
	mustHave(t, byEffect, EffectFSRead, "readConfig")
	mustHave(t, byEffect, EffectFSWrite, "writeLog")
	mustHave(t, byEffect, EffectMutation, "setName")
}

func TestSniffEffectsCCPP_PrimitiveCoverage(t *testing.T) {
	const src = `
#include <cstdio>
#include <curl/curl.h>
#include <libpq-fe.h>

class UserService {
public:
    int count;

    void call_remote() {
        CURL *c = curl_easy_init();
        curl_easy_setopt(c, CURLOPT_URL, "https://x");
        curl_easy_perform(c);
    }

    void load_users(PGconn *conn) {
        PGresult *r = PQexec(conn, "SELECT * FROM users");
        (void)r;
    }

    void save_user(PGconn *conn) {
        PGresult *r = PQexec(conn, "INSERT INTO users (name) VALUES ('x')");
        (void)r;
    }

    void read_config() {
        FILE *f = fopen("config.json", "r");
        (void)f;
    }

    void write_log(const char *msg) {
        FILE *f = fopen("log.txt", "w");
        (void)f;
        (void)msg;
    }

    void set_count(int n) {
        this->count = n;
    }
};
`
	got := sniffEffectsCCPP(src)
	if len(got) == 0 {
		t.Fatal("expected c-cpp matches; got none")
	}
	byEffect := groupByEffect(got)
	mustHave(t, byEffect, EffectHTTPOut, "call_remote")
	mustHave(t, byEffect, EffectDBRead, "load_users")
	mustHave(t, byEffect, EffectDBWrite, "save_user")
	mustHave(t, byEffect, EffectFSRead, "read_config")
	mustHave(t, byEffect, EffectFSWrite, "write_log")
	mustHave(t, byEffect, EffectMutation, "set_count")
}

func groupByEffect(ms []EffectMatch) map[Effect]map[string]bool {
	out := map[Effect]map[string]bool{}
	for _, m := range ms {
		if out[m.Effect] == nil {
			out[m.Effect] = map[string]bool{}
		}
		out[m.Effect][m.Function] = true
	}
	return out
}

func mustHave(t *testing.T, by map[Effect]map[string]bool, eff Effect, fn string) {
	t.Helper()
	if by[eff] == nil || !by[eff][fn] {
		fns := make([]string, 0, len(by[eff]))
		for k := range by[eff] {
			fns = append(fns, k)
		}
		sort.Strings(fns)
		t.Errorf("expected effect %q on function %q; got functions %v", eff, fn, fns)
	}
}
