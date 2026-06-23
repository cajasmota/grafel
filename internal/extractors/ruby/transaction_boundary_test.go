// transaction_boundary_test.go — value-asserting tests for #3628 transaction
// boundary stamping on Ruby method entities.
package ruby_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/ruby"
	"github.com/cajasmota/grafel/internal/types"
)

func extractRubyTx(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("ruby")
	if !ok {
		t.Fatal("ruby extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: "account.rb", Content: []byte(src), Language: "ruby", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return got
}

func findRubyOp(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == "SCOPE.Operation" {
			return &ents[i]
		}
	}
	return nil
}

func TestRubyTx_TransactionDo(t *testing.T) {
	src := `
class Account
  def pay(amount)
    ActiveRecord::Base.transaction do
      debit!(amount)
      ledger.record!(amount)
    end
  end

  def balance
    @balance
  end
end
`
	ents := extractRubyTx(t, src)
	pay := findRubyOp(ents, "pay")
	if pay == nil {
		t.Fatal("pay entity not found")
	}
	if pay.Properties["transactional"] != "true" {
		t.Errorf("pay transactional = %q, want true", pay.Properties["transactional"])
	}
	if pay.Properties["tx_source"] != "rails_transaction" {
		t.Errorf("pay tx_source = %q, want rails_transaction", pay.Properties["tx_source"])
	}

	// Negative: a plain method must not be stamped.
	bal := findRubyOp(ents, "balance")
	if bal == nil {
		t.Fatal("balance entity not found")
	}
	if _, ok := bal.Properties["transactional"]; ok {
		t.Errorf("balance should not be stamped transactional")
	}
}

func TestRubyTx_ModelTransactionBlock(t *testing.T) {
	src := `
class Order
  def refund
    Order.transaction { update!(state: :refunded) }
  end
end
`
	ents := extractRubyTx(t, src)
	fn := findRubyOp(ents, "refund")
	if fn == nil {
		t.Fatal("refund entity not found")
	}
	if fn.Properties["transactional"] != "true" {
		t.Errorf("refund (Order.transaction { }) transactional = %q, want true", fn.Properties["transactional"])
	}
}
