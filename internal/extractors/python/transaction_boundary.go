// transaction_boundary.go — #3628: stamp Python function/method entities that
// open a database transaction with Properties["transactional"]="true" (+
// tx_source) so the graph can answer "which operations are transactional?".
//
// Signals (see internal/txscope.DetectPython):
//   - Django:     @transaction.atomic decorator OR `with transaction.atomic():`
//                 block lexically inside the function body.
//   - SQLAlchemy: `with session.begin()` / `session.begin_nested()` /
//                 `engine.begin()`.
//
// HONESTY BOUNDARY — no transitive propagation. Only the function in whose own
// source span the atomic/begin construct lexically appears is stamped. A
// function that merely RECEIVES a session/connection and calls .add()/.flush()
// (without opening a transaction) is NOT stamped.
//
// The pass walks function_definition nodes — both bare and decorated — so the
// decorator-form `@transaction.atomic` (which lives on the wrapping
// decorated_definition) and the block-form `with transaction.atomic():` (in the
// body) are both seen by passing the decorator-inclusive node span to the
// detector.

package python

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/txscope"
	"github.com/cajasmota/grafel/internal/types"
)

// emitTransactionBoundaryProperties walks every function/method definition and
// stamps transaction properties on the matching SCOPE.Operation entity.
// Mutates *entities in place. Safe to call with nil/empty input.
func emitTransactionBoundaryProperties(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	walkTxDefs(root, "", file, entities)
}

func walkTxDefs(n ts.Node, parentClass string, file extractor.FileInput, entities *[]types.EntityRecord) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "class_definition":
		cls := ""
		if nameNode := n.ChildByFieldName("name"); nameNode != nil {
			cls = nodeText(nameNode, file.Content)
		}
		childCls := cls
		if parentClass != "" && cls != "" {
			childCls = parentClass + "." + cls
		}
		if body := n.ChildByFieldName("body"); body != nil {
			for i := 0; i < int(body.ChildCount()); i++ {
				walkTxDefs(body.Child(i), childCls, file, entities)
			}
		}
		return
	case "decorated_definition":
		inner := n.ChildByFieldName("definition")
		if inner != nil && inner.Type() == "function_definition" {
			// Pass the decorator-inclusive node (n) so @transaction.atomic on
			// the wrapper is seen alongside any body-level `with` construct.
			stampTxOnOperation(n, inner, parentClass, file, entities)
		}
		if inner != nil {
			walkTxDefs(inner, parentClass, file, entities)
		}
		return
	case "function_definition":
		stampTxOnOperation(n, n, parentClass, file, entities)
		// Recurse into the body to reach nested defs.
		if body := n.ChildByFieldName("body"); body != nil {
			for i := 0; i < int(body.ChildCount()); i++ {
				walkTxDefs(body.Child(i), parentClass, file, entities)
			}
		}
		return
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		walkTxDefs(n.Child(i), parentClass, file, entities)
	}
}

// stampTxOnOperation runs the txscope detector over spanNode's source (which
// may be the decorator-inclusive decorated_definition) and stamps the matching
// operation entity when a transaction boundary is found.
func stampTxOnOperation(spanNode, fnNode ts.Node, parentClass string, file extractor.FileInput, entities *[]types.EntityRecord) {
	nameNode := fnNode.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	methodName := nodeText(nameNode, file.Content)
	if methodName == "" {
		return
	}
	emittedName := methodName
	if parentClass != "" {
		emittedName = parentClass + "." + methodName
	}
	op := findOpByName(*entities, file.Path, emittedName)
	if op == nil {
		return
	}
	tx := txscope.DetectPython(nodeText(spanNode, file.Content))
	if !tx.Transactional {
		return
	}
	op.Properties = tx.Apply(op.Properties)
}
