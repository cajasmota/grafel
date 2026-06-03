// graphql_client_effects_4218_test.go — issue #4218 (epic #3872), verify-first.
//
// Proves the FRAMEWORK-BLIND, per-LANGUAGE effect sniffers (effects.go +
// effect_sinks_<lang>.go) fire on representative GraphQL-server resolver
// bodies for the records:
//
//   - lang.rust.framework.async-graphql   (effect_sinks_rust.go)
//   - lang.csharp.framework.hotchocolate  (effect_sinks_csharp.go)
//   - lang.kotlin.framework.graphql-kotlin (effect_sinks_kotlin.go)
//
// Each sniffer registers on the bare language slug with ZERO framework
// gating, so a GraphQL resolver in .rs / .cs / .kt source receives the same
// db_read / db_write / http_out / mutation detection as flagship siblings.
// These probes assert the EXACT effect kind attributed to the EXACT
// resolver method (never len>0), mirroring python_graphql_credit_3911_test.go
// and substrate_golang_graphql_gqlgen_test.go.
//
// PER-(record,effect) outcomes encoded below:
//
//	rust async-graphql : db_read+db_write+http_out → partial. mutation is
//	                     HONEST-MISSING — async-graphql resolvers take `&self`
//	                     and build/return new values; they do NOT write
//	                     `self.field`, so rustMutationRe (self.field = …) does
//	                     not fire. The negative probe documents this.
//	csharp hotchocolate: db_read+db_write+http_out+mutation → partial.
//	kotlin graphql-kotlin: db_read+db_write+http_out+mutation → partial.
//
// fs_effect is left missing on every GraphQL server (resolvers don't touch
// the filesystem) — no probe drives an fs sink.
//
// (gqlgen's db_read/db_write are already proven by
// TestSubstrate_Go_Gqlgen_EffectsAttribute; this file only adds the three
// non-Go GraphQL servers + the rust mutation negative.)
package substrate

import "testing"

// --- rust async-graphql -----------------------------------------------------

// asyncGraphqlEffSrc4218 is a representative async-graphql resolver impl
// block. `account` runs a sqlx SELECT (db_read); `create_order` runs a sqlx
// INSERT (db_write), an outbound reqwest POST (http_out), and builds a NEW
// local `order` (no self.field write → no mutation, by design).
const asyncGraphqlEffSrc4218 = `
use async_graphql::{Context, Object, Result};

struct QueryRoot;

#[Object]
impl QueryRoot {
    async fn account(&self, ctx: &Context<'_>, id: i32) -> Result<Account> {
        let pool = ctx.data::<PgPool>()?;
        let acct = sqlx::query_as!(Account, "SELECT * FROM accounts WHERE id = $1", id)
            .fetch_one(pool)
            .await?;
        Ok(acct)
    }
}

struct MutationRoot;

#[Object]
impl MutationRoot {
    async fn create_order(&self, ctx: &Context<'_>, amount: i32) -> Result<Order> {
        let pool = ctx.data::<PgPool>()?;
        sqlx::query!("INSERT INTO orders (amount) VALUES ($1)", amount)
            .execute(pool)
            .await?;
        let resp = reqwest::Client::new()
            .post("https://audit.example.com/log")
            .send()
            .await?;
        let order = Order { amount };
        Ok(order)
    }
}
`

// TestSubstrate_Rust_AsyncGraphql_EffectsAttribute proves db_read, db_write
// and http_out fire on async-graphql resolver bodies and attribute to the
// exact resolver method (receiver stripped by rustFuncHeaderRe). Credits
// db_effect (partial) and http_effect (partial) on lang.rust.framework.async-graphql.
func TestSubstrate_Rust_AsyncGraphql_EffectsAttribute(t *testing.T) {
	ms := sniffEffectsRust(asyncGraphqlEffSrc4218)
	if !hasEffectIn(ms, EffectDBRead, "account") {
		t.Errorf("effects: expected db_read (sqlx::query_as! SELECT) attributed to account, got %+v", ms)
	}
	if !hasEffectIn(ms, EffectDBWrite, "create_order") {
		t.Errorf("effects: expected db_write (sqlx::query! INSERT) attributed to create_order, got %+v", ms)
	}
	if !hasEffectIn(ms, EffectHTTPOut, "create_order") {
		t.Errorf("effects: expected http_out (reqwest .post().send().await) attributed to create_order, got %+v", ms)
	}
}

// TestSubstrate_Rust_AsyncGraphql_MutationDoesNotFire is the HONEST NEGATIVE:
// async-graphql resolvers take `&self` and return freshly-built values, so no
// `self.field = …` write exists. rustMutationRe therefore produces no mutation
// effect → mutation_effect STAYS MISSING for lang.rust.framework.async-graphql.
func TestSubstrate_Rust_AsyncGraphql_MutationDoesNotFire(t *testing.T) {
	ms := sniffEffectsRust(asyncGraphqlEffSrc4218)
	for _, m := range ms {
		if m.Effect == EffectMutation {
			t.Errorf("expected NO mutation effect (&self resolvers build new values, no self.field write); got %+v", m)
		}
	}
}

// --- csharp hotchocolate ----------------------------------------------------

// hotChocolateEffSrc4218 is a representative HotChocolate resolver type.
// GetAccount runs an EF FirstOrDefaultAsync (db_read); CreateOrder runs EF
// AddAsync + SaveChangesAsync (db_write), writes this.LastAmount (mutation),
// and POSTs via an injected HttpClient (http_out).
const hotChocolateEffSrc4218 = `
using HotChocolate;
using System.Net.Http;
using System.Threading.Tasks;

[QueryType]
public class Query
{
    public async Task<Account> GetAccount(int id, [Service] AppDbContext db)
    {
        var acct = await db.Accounts.FirstOrDefaultAsync(a => a.Id == id);
        return acct;
    }
}

[MutationType]
public class Mutation
{
    private readonly HttpClient _httpClient;

    public int LastAmount { get; set; }

    public async Task<Order> CreateOrder(int amount, [Service] AppDbContext db)
    {
        var order = new Order { Amount = amount };
        await db.Orders.AddAsync(order);
        await db.SaveChangesAsync();
        this.LastAmount = amount;
        var resp = await _httpClient.PostAsync("https://audit.example.com/log", null);
        return order;
    }
}
`

// TestSubstrate_CSharp_HotChocolate_EffectsAttribute proves db_read, db_write,
// http_out and mutation fire on HotChocolate resolver bodies and attribute to
// the exact resolver method. Credits db_effect, mutation_effect, http_effect
// (all partial) on lang.csharp.framework.hotchocolate.
func TestSubstrate_CSharp_HotChocolate_EffectsAttribute(t *testing.T) {
	ms := sniffEffectsCSharp(hotChocolateEffSrc4218)
	if !hasEffectIn(ms, EffectDBRead, "GetAccount") {
		t.Errorf("effects: expected db_read (EF FirstOrDefaultAsync) attributed to GetAccount, got %+v", ms)
	}
	if !hasEffectIn(ms, EffectDBWrite, "CreateOrder") {
		t.Errorf("effects: expected db_write (EF AddAsync/SaveChangesAsync) attributed to CreateOrder, got %+v", ms)
	}
	if !hasEffectIn(ms, EffectHTTPOut, "CreateOrder") {
		t.Errorf("effects: expected http_out (HttpClient.PostAsync) attributed to CreateOrder, got %+v", ms)
	}
	if !hasEffectIn(ms, EffectMutation, "CreateOrder") {
		t.Errorf("effects: expected mutation (this.LastAmount = …) attributed to CreateOrder, got %+v", ms)
	}
}

// --- kotlin graphql-kotlin --------------------------------------------------

// graphqlKotlinEffSrc4218 is a representative graphql-kotlin resolver class.
// account runs a Spring-Data findById (db_read); createOrder runs save
// (db_write), writes this.lastAmount (mutation), and POSTs via an injected
// client (http_out).
const graphqlKotlinEffSrc4218 = `
import com.expediagroup.graphql.server.operations.Query
import com.expediagroup.graphql.server.operations.Mutation

class AccountQuery(private val repo: AccountRepository) : Query {
    suspend fun account(id: Long): Account {
        val acct = repo.findById(id)
        return acct
    }
}

class OrderMutation(
    private val repo: OrderRepository,
    private val client: HttpClient,
) : Mutation {
    var lastAmount: Int = 0

    fun createOrder(amount: Int): Order {
        val order = Order(amount = amount)
        repo.save(order)
        this.lastAmount = amount
        val resp = client.post("https://audit.example.com/log")
        return order
    }
}
`

// TestSubstrate_Kotlin_GraphqlKotlin_EffectsAttribute proves db_read, db_write,
// http_out and mutation fire on graphql-kotlin resolver bodies and attribute to
// the exact resolver method. Credits db_effect, mutation_effect, http_effect
// (all partial) on lang.kotlin.framework.graphql-kotlin.
func TestSubstrate_Kotlin_GraphqlKotlin_EffectsAttribute(t *testing.T) {
	ms := sniffEffectsKotlin(graphqlKotlinEffSrc4218)
	if !hasEffectIn(ms, EffectDBRead, "account") {
		t.Errorf("effects: expected db_read (Spring-Data findById) attributed to account, got %+v", ms)
	}
	if !hasEffectIn(ms, EffectDBWrite, "createOrder") {
		t.Errorf("effects: expected db_write (repo.save) attributed to createOrder, got %+v", ms)
	}
	if !hasEffectIn(ms, EffectHTTPOut, "createOrder") {
		t.Errorf("effects: expected http_out (client.post) attributed to createOrder, got %+v", ms)
	}
	if !hasEffectIn(ms, EffectMutation, "createOrder") {
		t.Errorf("effects: expected mutation (this.lastAmount = …) attributed to createOrder, got %+v", ms)
	}
}

// --- non-vacuousness guard --------------------------------------------------

// TestSubstrate_GraphqlServers_FSEffectStaysMissing proves fs_effect is
// honestly absent: none of the three resolver sources touches the filesystem,
// so no fs_read / fs_write effect is produced. This keeps fs_effect missing on
// all three GraphQL-server records and demonstrates the assertions above are
// non-vacuous (the sniffers report a SPECIFIC effect set, not everything).
func TestSubstrate_GraphqlServers_FSEffectStaysMissing(t *testing.T) {
	cases := map[string][]EffectMatch{
		"async-graphql":  sniffEffectsRust(asyncGraphqlEffSrc4218),
		"hotchocolate":   sniffEffectsCSharp(hotChocolateEffSrc4218),
		"graphql-kotlin": sniffEffectsKotlin(graphqlKotlinEffSrc4218),
	}
	for name, ms := range cases {
		for _, m := range ms {
			if m.Effect == EffectFSRead || m.Effect == EffectFSWrite {
				t.Errorf("%s: expected NO fs effect (resolvers don't touch FS); got %+v", name, m)
			}
		}
	}
}
