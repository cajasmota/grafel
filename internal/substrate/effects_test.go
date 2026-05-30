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

// TestSniffEffectsPython_IOHeavySinks is the #2804 regression: a Celery
// task / DRF action that performs S3 (boto3), env reads, and raw DB
// access must NOT be classified pure. Mirrors the shapes from
// core/tasks/ecb_pdf_pipeline.py and core/views/contract_viewset.py that
// previously reported {} / 0.3 / pure.
func TestSniffEffectsPython_IOHeavySinks(t *testing.T) {
	const src = `
import os
import boto3
import mysql.connector

class Pipeline:
    def run_job(self, payload):
        s3 = boto3.client(
            "s3",
            aws_access_key_id=os.getenv("AWS_ACCESS_KEY_ID"),
            region_name=os.environ.get("AWS_REGION"),
        )
        s3.download_file(payload["bucket"], payload["key"], "/tmp/x.pdf")
        s3.upload_fileobj(open("/tmp/x.pdf", "rb"), "out", "y.pdf")

    def write_controller(self, row):
        conn = mysql.connector.connect(host=os.environ["DB_HOST"])
        cur = conn.cursor()
        cur.execute(insert_sql, row)
        conn.commit()

    def read_with_psycopg(self):
        c = psycopg2.connect("dsn")
        cur = c.cursor()
        cur.execute("SELECT 1")
`
	got := sniffEffectsPython(src)
	if len(got) == 0 {
		t.Fatal("expected python matches; got none")
	}
	by := groupByEffect(got)
	// boto3 client + S3 ops cross the network → http_out.
	mustHave(t, by, EffectHTTPOut, "run_job")
	// os.getenv / os.environ.get / os.environ[...] → env_read.
	mustHave(t, by, EffectEnvRead, "run_job")
	mustHave(t, by, EffectEnvRead, "write_controller")
	// raw DB-API driver connect/cursor → db_read; commit → db_write.
	mustHave(t, by, EffectDBRead, "write_controller")
	mustHave(t, by, EffectDBWrite, "write_controller")
	mustHave(t, by, EffectDBRead, "read_with_psycopg")
}

// TestSniffEffectsPython_MongoSinks is the #3440 (ask 4) regression:
// the rewrite-agent reported core.tasks._get_me_inspections as
// {effect_source: "pure", 0.3} despite calling inspections_cls.aggregate(
// pipeline) — a Mongo read. pymongo/motor collection methods lack the
// Django `.objects.` prefix so they bypassed pyDBReadRe. They must now
// register db_read (reads) / db_write (mutators), and a genuinely pure
// helper must stay pure (negative assertion).
func TestSniffEffectsPython_MongoSinks(t *testing.T) {
	const src = `
class InspectionRepo:
    def _get_me_inspections(self, building_id):
        pipeline = [{"$match": {"building": building_id}}]
        return list(inspections_cls.aggregate(pipeline))

    def fetch_one(self, oid):
        return self.coll.find_one({"_id": oid})

    def fetch_many(self, q):
        return self.coll.find(q)

    def how_many(self, q):
        return self.coll.count_documents(q)

    def insert(self, doc):
        self.coll.insert_one(doc)

    def patch(self, oid, doc):
        self.coll.update_one({"_id": oid}, {"$set": doc})

    def upsert(self, oid, doc):
        self.coll.find_one_and_update({"_id": oid}, {"$set": doc}, upsert=True)

    def bulk(self, ops):
        self.coll.bulk_write(ops)

    def pure_add(self, a, b):
        total = a + b
        return total
`
	got := sniffEffectsPython(src)
	if len(got) == 0 {
		t.Fatal("expected python matches; got none")
	}
	by := groupByEffect(got)
	// The exact bug: .aggregate(pipeline) on a plain collection handle.
	mustHave(t, by, EffectDBRead, "_get_me_inspections")
	mustHave(t, by, EffectDBRead, "fetch_one")
	mustHave(t, by, EffectDBRead, "fetch_many")
	mustHave(t, by, EffectDBRead, "how_many")
	// Mutators classify as db_write.
	mustHave(t, by, EffectDBWrite, "insert")
	mustHave(t, by, EffectDBWrite, "patch")
	mustHave(t, by, EffectDBWrite, "upsert")
	mustHave(t, by, EffectDBWrite, "bulk")
	// A pure helper stays pure: no db effect of either kind.
	mustNotHave(t, by, EffectDBRead, "pure_add")
	mustNotHave(t, by, EffectDBWrite, "pure_add")
}

// TestSniffEffectsJSTS_MongoSinks covers the Mongoose / native-driver
// extras added in #3440 ask 4: .findById/.countDocuments/.distinct reads
// and the find-and-modify / .bulkWrite / .replaceOne write family. A pure
// helper must stay pure.
func TestSniffEffectsJSTS_MongoSinks(t *testing.T) {
	const src = `
class Repo {
  async byId(id) {
    return await this.model.findById(id);
  }
  async total() {
    return await this.model.countDocuments({});
  }
  async kinds() {
    return await this.model.distinct("kind");
  }
  async patch(id, doc) {
    return await this.model.findByIdAndUpdate(id, doc);
  }
  async swap(filter, doc) {
    return await this.model.replaceOne(filter, doc);
  }
  async batch(ops) {
    return await this.model.bulkWrite(ops);
  }
  pureAdd(a, b) {
    const total = a + b;
    return total;
  }
}
`
	got := sniffEffectsJSTS(src)
	if len(got) == 0 {
		t.Fatal("expected matches; got none")
	}
	by := groupByEffect(got)
	mustHave(t, by, EffectDBRead, "byId")
	mustHave(t, by, EffectDBRead, "total")
	mustHave(t, by, EffectDBRead, "kinds")
	mustHave(t, by, EffectDBWrite, "patch")
	mustHave(t, by, EffectDBWrite, "swap")
	mustHave(t, by, EffectDBWrite, "batch")
	mustNotHave(t, by, EffectDBRead, "pureAdd")
	mustNotHave(t, by, EffectDBWrite, "pureAdd")
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

// TestSniffEffectsPython_CeleryTaskBody is the proving fixture for
// #2982 (Celery substrate A-win cells). It verifies that the language-wide
// Python effect sniffer detects db_write, db_read, http_out, and env_read
// inside a @shared_task function body — confirming that the substrate
// db_effect, taint_source_detection, and taint_sink_detection cells are
// legitimately partial for lang.python.framework.celery.
func TestSniffEffectsPython_CeleryTaskBody(t *testing.T) {
	const src = `
from celery import shared_task
import os, boto3, psycopg2

@shared_task(bind=True, max_retries=3)
def process_invoice(self, invoice_id):
    conn = psycopg2.connect(os.getenv("DATABASE_URL"))
    cur = conn.cursor()
    cur.execute("SELECT * FROM invoices WHERE id = %s", (invoice_id,))
    row = cur.fetchone()
    s3 = boto3.client("s3", region_name=os.environ.get("AWS_REGION"))
    s3.upload_file("/tmp/invoice.pdf", "bucket", "key")
    cur.execute("UPDATE invoices SET processed = true WHERE id = %s", (invoice_id,))
    conn.commit()
`
	got := sniffEffectsPython(src)
	if len(got) == 0 {
		t.Fatal("#2982 proof: expected effect matches inside @shared_task body; got none")
	}
	by := groupByEffect(got)
	mustHave(t, by, EffectDBRead, "process_invoice")
	mustHave(t, by, EffectDBWrite, "process_invoice")
	mustHave(t, by, EffectHTTPOut, "process_invoice")
	mustHave(t, by, EffectEnvRead, "process_invoice")
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

// mustNotHave asserts that function fn does NOT carry effect eff — the
// negative guard for pure-function regressions (a genuinely pure helper
// must not pick up a spurious db effect from the Mongo sniffers).
func mustNotHave(t *testing.T, by map[Effect]map[string]bool, eff Effect, fn string) {
	t.Helper()
	if by[eff] != nil && by[eff][fn] {
		t.Errorf("expected function %q to NOT carry effect %q, but it did", fn, eff)
	}
}
