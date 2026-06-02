// transaction_boundary_test.go — value-asserting tests for #3628 transaction
// boundary stamping on JS/TS function/method entities.
package javascript_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func findJSOp(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == "SCOPE.Operation" {
			return &ents[i]
		}
	}
	return nil
}

func TestJSTx_SequelizeTransaction(t *testing.T) {
	src := []byte(`
async function transfer(from, to, amt) {
  await sequelize.transaction(async (t) => {
    await Account.decrement({ balance: amt }, { where: { id: from }, transaction: t });
    await Account.increment({ balance: amt }, { where: { id: to }, transaction: t });
  });
}
function plain(x) { return x + 1; }
`)
	ents := extractAtPath(t, src, "javascript", "svc.js")
	fn := findJSOp(ents, "transfer")
	if fn == nil {
		t.Fatal("transfer entity not found")
	}
	if fn.Properties["transactional"] != "true" {
		t.Errorf("transfer transactional = %q, want true", fn.Properties["transactional"])
	}
	if fn.Properties["tx_source"] != "sequelize_transaction" {
		t.Errorf("transfer tx_source = %q, want sequelize_transaction", fn.Properties["tx_source"])
	}

	plain := findJSOp(ents, "plain")
	if plain == nil {
		t.Fatal("plain entity not found")
	}
	if _, ok := plain.Properties["transactional"]; ok {
		t.Errorf("plain should not be stamped transactional")
	}
}

func TestTSTx_TypeORMTransaction(t *testing.T) {
	src := []byte(`
class UserService {
  async save(user: User): Promise<void> {
    await this.dataSource.transaction(async (manager) => {
      await manager.save(user);
    });
  }
  async persist(manager: EntityManager, user: User): Promise<void> {
    await manager.save(user);
  }
}
`)
	ents := extractAtPath(t, src, "typescript", "user.service.ts")
	save := findJSOp(ents, "save")
	if save == nil {
		t.Fatal("save entity not found")
	}
	if save.Properties["transactional"] != "true" || save.Properties["tx_source"] != "typeorm_transaction" {
		t.Errorf("save not stamped typeorm_transaction: %v", save.Properties)
	}

	// Honesty boundary: persist only RECEIVES a manager, opens no transaction.
	persist := findJSOp(ents, "persist")
	if persist == nil {
		t.Fatal("persist entity not found")
	}
	if _, ok := persist.Properties["transactional"]; ok {
		t.Errorf("persist only receives manager, must not be stamped: %v", persist.Properties)
	}
}

func TestTSTx_PrismaInteractiveTransaction(t *testing.T) {
	src := []byte(`
async function checkout(prisma) {
  return await prisma.$transaction(async (tx) => {
    await tx.order.create({});
    await tx.payment.create({});
  });
}
`)
	ents := extractAtPath(t, src, "javascript", "checkout.js")
	fn := findJSOp(ents, "checkout")
	if fn == nil {
		t.Fatal("checkout entity not found")
	}
	if fn.Properties["transactional"] != "true" || fn.Properties["tx_source"] != "prisma_transaction" {
		t.Errorf("checkout not stamped prisma_transaction: %v", fn.Properties)
	}
}
