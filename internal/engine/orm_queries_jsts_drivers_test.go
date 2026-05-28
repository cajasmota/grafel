// Tests for the raw-DB-driver + query-builder attribution pass (#2862).
//
// Strategy mirrors orm_queries_test.go: run the full detector against a
// small hand-written fixture per driver/ORM and assert on the emitted
// QUERIES edges (caller → Class:<resource>, operation, orm). The fixtures
// are realistic call shapes for each driver's public API; each one proves
// the corresponding registry cell flip to `full`.
package engine

import "testing"

// --- postgres (pg / node-postgres) --------------------------------------

func TestDriver_PostgresPgQuery(t *testing.T) {
	src := `import { Pool } from 'pg'
const pool = new Pool()
export async function listUsers() {
  return pool.query('SELECT id, name FROM users WHERE active = $1', [true])
}
export async function addUser(name) {
  return pool.query('INSERT INTO users (name) VALUES ($1)', [name])
}
`
	edges := detectORM(t, "typescript", "src/db/users.ts", src)
	e := assertEdgeExists(t, edges, "Function:listUsers", "Class:User", "find")
	if e.ORM != "postgres" {
		t.Errorf("expected orm=postgres, got %q", e.ORM)
	}
	assertEdgeExists(t, edges, "Function:addUser", "Class:User", "create")
}

// --- mysql / mysql2 ------------------------------------------------------

func TestDriver_MySQL2Query(t *testing.T) {
	src := `const mysql = require('mysql2/promise')
const conn = await mysql.createConnection({})
async function getOrders() {
  const [rows] = await conn.execute('SELECT * FROM orders o JOIN items i ON i.order_id = o.id')
  return rows
}
async function delOrder(id) {
  await conn.query('DELETE FROM orders WHERE id = ?', [id])
}
`
	edges := detectORM(t, "javascript", "src/orders.js", src)
	e := assertEdgeExists(t, edges, "Function:getOrders", "Class:Order", "find")
	if e.ORM != "mysql2" {
		t.Errorf("expected orm=mysql2, got %q", e.ORM)
	}
	if e.IsJoin != "true" {
		t.Errorf("expected is_join=true for JOIN query, got %q", e.IsJoin)
	}
	assertEdgeExists(t, edges, "Function:delOrder", "Class:Order", "delete")
}

// --- sqlite3 / better-sqlite3 -------------------------------------------

func TestDriver_SQLitePrepare(t *testing.T) {
	src := `const Database = require('better-sqlite3')
const db = new Database('app.db')
function findProduct(id) {
  return db.prepare('SELECT * FROM products WHERE id = ?').get(id)
}
function updateProduct(id, name) {
  return db.prepare('UPDATE products SET name = ? WHERE id = ?').run(name, id)
}
`
	edges := detectORM(t, "javascript", "src/products.js", src)
	e := assertEdgeExists(t, edges, "Function:findProduct", "Class:Product", "find")
	if e.ORM != "better-sqlite3" {
		t.Errorf("expected orm=better-sqlite3, got %q", e.ORM)
	}
	assertEdgeExists(t, edges, "Function:updateProduct", "Class:Product", "update")
}

// --- mssql ---------------------------------------------------------------

func TestDriver_MSSQLQuery(t *testing.T) {
	src := `import sql from 'mssql'
const pool = await sql.connect(config)
export async function getInvoices() {
  return pool.request().query('SELECT TOP 10 * FROM invoices')
}
`
	edges := detectORM(t, "typescript", "src/invoices.ts", src)
	e := assertEdgeExists(t, edges, "Function:getInvoices", "Class:Invoice", "find")
	if e.ORM != "mssql" {
		t.Errorf("expected orm=mssql, got %q", e.ORM)
	}
}

// --- oracledb ------------------------------------------------------------

func TestDriver_OracleExecute(t *testing.T) {
	src := `const oracledb = require('oracledb')
async function getEmployees() {
  const connection = await oracledb.getConnection()
  return connection.execute('SELECT employee_id, name FROM employees')
}
`
	edges := detectORM(t, "javascript", "src/hr.js", src)
	e := assertEdgeExists(t, edges, "Function:getEmployees", "Class:Employee", "find")
	if e.ORM != "oracledb" {
		t.Errorf("expected orm=oracledb, got %q", e.ORM)
	}
}

// --- cassandra-driver ----------------------------------------------------

func TestDriver_CassandraExecute(t *testing.T) {
	src := `const cassandra = require('cassandra-driver')
const client = new cassandra.Client({})
async function recentEvents() {
  return client.execute('SELECT * FROM events WHERE day = ?', [today])
}
async function logEvent(e) {
  return client.execute('INSERT INTO events (id, body) VALUES (?, ?)', [e.id, e.body])
}
`
	edges := detectORM(t, "javascript", "src/events.js", src)
	e := assertEdgeExists(t, edges, "Function:recentEvents", "Class:Event", "find")
	if e.ORM != "cassandra" {
		t.Errorf("expected orm=cassandra, got %q", e.ORM)
	}
	assertEdgeExists(t, edges, "Function:logEvent", "Class:Event", "create")
}

// --- mongodb (native driver) --------------------------------------------

func TestDriver_MongoDBCollection(t *testing.T) {
	src := `import { MongoClient } from 'mongodb'
const client = new MongoClient(uri)
const db = client.db('app')
export async function findUser(id) {
  return db.collection('users').findOne({ _id: id })
}
export async function addUser(u) {
  return db.collection('users').insertOne(u)
}
`
	edges := detectORM(t, "typescript", "src/mongo.ts", src)
	e := assertEdgeExists(t, edges, "Function:findUser", "Class:User", "find")
	if e.ORM != "mongodb" {
		t.Errorf("expected orm=mongodb, got %q", e.ORM)
	}
	assertEdgeExists(t, edges, "Function:addUser", "Class:User", "create")
}

// --- redis / ioredis -----------------------------------------------------

func TestDriver_RedisCommands(t *testing.T) {
	src := `import Redis from 'ioredis'
const redis = new Redis()
export async function getSession(id) {
  return redis.get('session:' + id)
}
export async function setSession(id, data) {
  return redis.set('session:' + id, data)
}
export async function dropSession(id) {
  return redis.del('session:' + id)
}
`
	edges := detectORM(t, "typescript", "src/sessions.ts", src)
	e := assertEdgeExists(t, edges, "Function:getSession", "Class:Session", "find")
	if e.ORM != "redis" {
		t.Errorf("expected orm=redis, got %q", e.ORM)
	}
	assertEdgeExists(t, edges, "Function:setSession", "Class:Session", "create")
	assertEdgeExists(t, edges, "Function:dropSession", "Class:Session", "delete")
}

// --- neo4j ---------------------------------------------------------------

func TestDriver_Neo4jRun(t *testing.T) {
	src := `import neo4j from 'neo4j-driver'
const driver = neo4j.driver(uri)
export async function findPerson(name) {
  const session = driver.session()
  return session.run('MATCH (p:Person {name: $name}) RETURN p', { name })
}
export async function createPerson(name) {
  const session = driver.session()
  return session.run('CREATE (p:Person {name: $name}) RETURN p', { name })
}
`
	edges := detectORM(t, "typescript", "src/graph.ts", src)
	e := assertEdgeExists(t, edges, "Function:findPerson", "Class:Person", "find")
	if e.ORM != "neo4j" {
		t.Errorf("expected orm=neo4j, got %q", e.ORM)
	}
	assertEdgeExists(t, edges, "Function:createPerson", "Class:Person", "create")
}

// --- dynamodb ------------------------------------------------------------

func TestDriver_DynamoDocClient(t *testing.T) {
	src := `import { DynamoDBDocumentClient, GetCommand } from '@aws-sdk/lib-dynamodb'
const docClient = DynamoDBDocumentClient.from(client)
export async function getItem(id) {
  return docClient.get({ TableName: 'Products', Key: { id } })
}
export async function putItem(item) {
  return docClient.put({ TableName: 'Products', Item: item })
}
`
	edges := detectORM(t, "typescript", "src/dynamo.ts", src)
	e := assertEdgeExists(t, edges, "Function:getItem", "Class:Product", "find")
	if e.ORM != "dynamodb" {
		t.Errorf("expected orm=dynamodb, got %q", e.ORM)
	}
	assertEdgeExists(t, edges, "Function:putItem", "Class:Product", "create")
}

// --- elasticsearch -------------------------------------------------------

func TestDriver_ElasticSearch(t *testing.T) {
	src := `import { Client } from '@elastic/elasticsearch'
const client = new Client({ node: 'http://localhost:9200' })
export async function searchArticles(q) {
  return client.search({ index: 'articles', query: { match: { body: q } } })
}
export async function indexArticle(doc) {
  return client.index({ index: 'articles', document: doc })
}
`
	edges := detectORM(t, "typescript", "src/search.ts", src)
	e := assertEdgeExists(t, edges, "Function:searchArticles", "Class:Article", "find")
	if e.ORM != "elastic" {
		t.Errorf("expected orm=elastic, got %q", e.ORM)
	}
	assertEdgeExists(t, edges, "Function:indexArticle", "Class:Article", "create")
}

// --- knex (query builder) ------------------------------------------------

func TestORM_KnexBuilder(t *testing.T) {
	src := `import knex from 'knex'
const db = knex(config)
export async function listAccounts() {
  return db('accounts').select('*').where({ active: true })
}
export async function addAccount(row) {
  return db.insert(row).into('accounts')
}
export async function dropAccount(id) {
  return db('accounts').where({ id }).delete()
}
`
	edges := detectORM(t, "typescript", "src/accounts.ts", src)
	e := assertEdgeExists(t, edges, "Function:listAccounts", "Class:Account", "find")
	if e.ORM != "knex" {
		t.Errorf("expected orm=knex, got %q", e.ORM)
	}
	assertEdgeExists(t, edges, "Function:addAccount", "Class:Account", "create")
	assertEdgeExists(t, edges, "Function:dropAccount", "Class:Account", "delete")
}

// --- mikro-orm (EntityManager) ------------------------------------------

func TestORM_MikroORMEntityManager(t *testing.T) {
	src := `import { MikroORM, EntityManager } from '@mikro-orm/core'
export async function findBook(em, id) {
  return em.findOne(Book, { id })
}
export async function allBooks(em) {
  return em.find(Book, {})
}
export async function removeBook(em, id) {
  return em.nativeDelete(Book, { id })
}
`
	edges := detectORM(t, "typescript", "src/books.ts", src)
	e := assertEdgeExists(t, edges, "Function:findBook", "Class:Book", "find")
	if e.ORM != "mikro-orm" {
		t.Errorf("expected orm=mikro-orm, got %q", e.ORM)
	}
	assertEdgeExists(t, edges, "Function:allBooks", "Class:Book", "find")
	assertEdgeExists(t, edges, "Function:removeBook", "Class:Book", "delete")
}

// --- objection (Model.query) --------------------------------------------

func TestORM_ObjectionModelQuery(t *testing.T) {
	src := `import { Model } from 'objection'
class Customer extends Model {}
export async function listCustomers() {
  return Customer.query().where('active', true)
}
export async function addCustomer(data) {
  return Customer.query().insert(data)
}
export async function removeCustomer(id) {
  return Customer.query().deleteById(id)
}
`
	edges := detectORM(t, "typescript", "src/customers.ts", src)
	e := assertEdgeExists(t, edges, "Function:listCustomers", "Class:Customer", "find")
	if e.ORM != "objection" {
		t.Errorf("expected orm=objection, got %q", e.ORM)
	}
	assertEdgeExists(t, edges, "Function:addCustomer", "Class:Customer", "create")
	assertEdgeExists(t, edges, "Function:removeCustomer", "Class:Customer", "delete")
}

// --- non-driver file emits nothing --------------------------------------

func TestDriver_NoEdgesOnPlainFile(t *testing.T) {
	src := `export function add(a: number, b: number) { return a + b }
export const log = (m: string) => console.log(m)
`
	edges := detectORM(t, "typescript", "src/util.ts", src)
	if len(edges) != 0 {
		t.Errorf("expected zero QUERIES edges on non-driver file, got %d: %+v", len(edges), edges)
	}
}
