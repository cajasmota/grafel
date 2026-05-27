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
