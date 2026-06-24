package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #5362 — Clojure persistence-driver QUERIES topology. Proves next.jdbc /
// clojure.java.jdbc, HoneySQL, and Datomic/DataScript datalog query call sites
// attribute to the table/attribute-namespace they touch, in the same
// `caller → Class:<resource>` QUERIES-edge shape every other language emits.

// cljQueriesEdge returns the first QUERIES edge whose ToID is Class:<model> and
// (when op != "") whose operation matches, or nil.
func cljQueriesEdge(rels []types.RelationshipRecord, model, op, orm string) *types.RelationshipRecord {
	for i := range rels {
		r := rels[i]
		if r.Kind != string(types.RelationshipKindQueries) {
			continue
		}
		if r.ToID != "Class:"+model {
			continue
		}
		if op != "" && r.Properties["operation"] != op {
			continue
		}
		if orm != "" && r.Properties["orm"] != orm {
			continue
		}
		return &r
	}
	return nil
}

func requireCljQuery(t *testing.T, rels []types.RelationshipRecord, model, op, orm, label string) {
	t.Helper()
	if cljQueriesEdge(rels, model, op, orm) == nil {
		t.Errorf("%s: expected QUERIES edge to Class:%s op=%s orm=%s, got none (rels=%d)",
			label, model, op, orm, len(rels))
	}
}

// TestClojure_NextJDBC_SQLString covers the next.jdbc raw-SQL idiom where the
// table + verb are parsed out of the vector-wrapped SQL string.
func TestClojure_NextJDBC_SQLString(t *testing.T) {
	src := `(ns app.db
  (:require [next.jdbc :as jdbc]))

(defn list-users [ds]
  (jdbc/execute! ds ["SELECT * FROM users WHERE active = ?" true]))

(defn add-order [ds o]
  (jdbc/execute-one! ds ["INSERT INTO orders (sku) VALUES (?)" (:sku o)]))
`
	_, rels := runDetectWithRels(t, "clojure", "db.clj", src)
	requireCljQuery(t, rels, "User", "find", "next.jdbc", "nextjdbc-select")
	requireCljQuery(t, rels, "Order", "create", "next.jdbc", "nextjdbc-insert")
}

// TestClojure_NextJDBC_FriendlyFns covers the next.jdbc.sql friendly-fn idiom
// where the table is the keyword arg after the datasource.
func TestClojure_NextJDBC_FriendlyFns(t *testing.T) {
	src := `(ns app.repo
  (:require [next.jdbc.sql :as sql]))

(defn create-user [ds m] (sql/insert! ds :users m))
(defn touch-user  [ds m] (sql/update! ds :users m {:id 1}))
(defn drop-user   [ds]   (sql/delete! ds :users {:id 1}))
(defn fetch-users [ds]   (sql/find-by-keys ds :users {:active true}))
`
	_, rels := runDetectWithRels(t, "clojure", "repo.clj", src)
	requireCljQuery(t, rels, "User", "create", "next.jdbc", "friendly-insert")
	requireCljQuery(t, rels, "User", "update", "next.jdbc", "friendly-update")
	requireCljQuery(t, rels, "User", "delete", "next.jdbc", "friendly-delete")
	requireCljQuery(t, rels, "User", "find", "next.jdbc", "friendly-find")
}

// TestClojure_HoneySQL covers the HoneySQL data-DSL — :from for SELECT and the
// insert-into / update / delete-from DML clauses.
func TestClojure_HoneySQL(t *testing.T) {
	src := `(ns app.queries
  (:require [honey.sql :as sql]))

(defn active-users []
  (sql/format {:select [:*] :from [:users] :where [:= :active true]}))

(defn new-order [o]
  (sql/format {:insert-into :orders :values [o]}))

(defn bump-user [id]
  (sql/format {:update :users :set {:seen true} :where [:= :id id]}))

(defn purge-session [id]
  (sql/format {:delete-from :sessions :where [:= :id id]}))
`
	_, rels := runDetectWithRels(t, "clojure", "queries.clj", src)
	requireCljQuery(t, rels, "User", "find", "honeysql", "honey-select")
	requireCljQuery(t, rels, "Order", "create", "honeysql", "honey-insert")
	requireCljQuery(t, rels, "User", "update", "honeysql", "honey-update")
	requireCljQuery(t, rels, "Session", "delete", "honeysql", "honey-delete")
}

// TestClojure_Datalog covers Datomic/DataScript datalog queries — the queried
// attribute NAMESPACE is the resolved resource (honest partial: datalog has no
// single table). The reserved :db/ namespace is excluded.
func TestClojure_Datalog(t *testing.T) {
	src := `(ns app.datalog
  (:require [datomic.api :as d]))

(defn find-users [db]
  (d/q '[:find ?e
         :where [?e :user/email ?email]
                [?e :db/id ?id]]
       db))

(defn find-orders [db]
  (d/q '[:find ?o
         :where [?o :order/sku ?sku]]
       db))
`
	_, rels := runDetectWithRels(t, "clojure", "datalog.clj", src)
	requireCljQuery(t, rels, "User", "find", "datalog", "datalog-user")
	requireCljQuery(t, rels, "Order", "find", "datalog", "datalog-order")
	// :db/id is the reserved schema namespace — must NOT forge a Db resource.
	if cljQueriesEdge(rels, "Db", "", "datalog") != nil {
		t.Errorf("datalog: reserved :db/ namespace forged a resource edge")
	}
}

// TestClojure_NonDBNoEdges is the negative guard: a plain Clojure file with no
// persistence markers emits no QUERIES edges.
func TestClojure_NonDBNoEdges(t *testing.T) {
	src := `(ns app.util)
(defn greet [n] (str "hi " n))
`
	_, rels := runDetectWithRels(t, "clojure", "util.clj", src)
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindQueries) {
			t.Errorf("non-DB file forged a QUERIES edge: %+v", r)
		}
	}
}
