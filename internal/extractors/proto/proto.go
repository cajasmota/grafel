// Package proto implements the tree-sitter–based extractor for Protocol Buffer source files.
//
// Extracted entities:
//   - service  → Kind="SCOPE.Schema", Subtype="service"
//   - rpc      → Kind="SCOPE.Schema", Subtype="rpc"
//   - message  → Kind="SCOPE.Schema", Subtype="message"
//   - enum     → Kind="SCOPE.Schema", Subtype="enum"
//
// Uses the protobuf grammar from smacker/go-tree-sitter.
// Registers itself via init() and is imported by registry_gen.go.
package proto

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("proto", &Extractor{})
}

// Extractor implements extractor.Extractor for Protocol Buffers.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "proto" }

// Extract walks the tree-sitter CST and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if file.Tree == nil || len(file.Content) == 0 {
		return nil, nil
	}

	var entities []types.EntityRecord
	walkProto(file.Tree.RootNode(), file, &entities)
	return entities, nil
}

func nodeText(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
}

func childByType(node *sitter.Node, types_ ...string) *sitter.Node {
	set := make(map[string]bool, len(types_))
	for _, t := range types_ {
		set[t] = true
	}
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch != nil && set[ch.Type()] {
			return ch
		}
	}
	return nil
}

func walkProto(node *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "service":
		if rec, ok := buildService(node, file); ok {
			*out = append(*out, rec)
		}
		// Walk inside service for rpc nodes.
		for i := range node.ChildCount() {
			ch := node.Child(int(i))
			if ch != nil && ch.Type() == "rpc" {
				if rec, ok := buildRPC(ch, file); ok {
					*out = append(*out, rec)
				}
			}
		}
		return // Don't recurse further into service — already handled.
	case "message":
		if rec, ok := buildMessage(node, file); ok {
			*out = append(*out, rec)
		}
	case "enum":
		if rec, ok := buildEnum(node, file); ok {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walkProto(node.Child(int(i)), file, out)
	}
}

func buildService(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	nameNode := childByType(node, "service_name")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := strings.TrimSpace(nodeText(nameNode, file.Content))
	// service_name wraps identifier
	if ident := childByType(nameNode, "identifier"); ident != nil {
		name = nodeText(ident, file.Content)
	}
	if name == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Service",
		Subtype:            "service",
		SourceFile:         file.Path,
		Language:           "protobuf",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "service " + name,
		EnrichmentRequired: false,
	}, true
}

func buildRPC(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	// rpc: rpc <rpc_name> ( <msg_type> ) returns ( <msg_type> ) ;
	nameNode := childByType(node, "rpc_name")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := strings.TrimSpace(nodeText(nameNode, file.Content))
	if ident := childByType(nameNode, "identifier"); ident != nil {
		name = nodeText(ident, file.Content)
	}
	if name == "" {
		return types.EntityRecord{}, false
	}

	// Collect request and response types.
	var msgTypes []string
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch != nil && ch.Type() == "message_or_enum_type" {
			msgTypes = append(msgTypes, nodeText(ch, file.Content))
		}
	}
	reqType, respType := "?", "?"
	if len(msgTypes) >= 1 {
		reqType = msgTypes[0]
	}
	if len(msgTypes) >= 2 {
		respType = msgTypes[1]
	}

	sig := fmt.Sprintf("rpc %s(%s) returns (%s)", name, reqType, respType)
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            "endpoint",
		SourceFile:         file.Path,
		Language:           "protobuf",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		EnrichmentRequired: false,
		// Set type=rpc explicitly so buildOutputDoc doesn't override with "endpoint".
		// Python golden uses type=rpc, subtype=endpoint for RPC entities.
		Properties: map[string]string{"type": "rpc"},
		Relationships: []types.RelationshipRecord{
			{FromID: file.Path, ToID: reqType, Kind: "IMPORTS"},
			{FromID: file.Path, ToID: respType, Kind: "IMPORTS"},
		},
	}, true
}

func buildMessage(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	nameNode := childByType(node, "message_name")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := strings.TrimSpace(nodeText(nameNode, file.Content))
	if ident := childByType(nameNode, "identifier"); ident != nil {
		name = nodeText(ident, file.Content)
	}
	if name == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Schema",
		Subtype:            "message",
		SourceFile:         file.Path,
		Language:           "protobuf",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "message " + name,
		EnrichmentRequired: false,
	}, true
}

func buildEnum(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	nameNode := childByType(node, "enum_name")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := strings.TrimSpace(nodeText(nameNode, file.Content))
	if ident := childByType(nameNode, "identifier"); ident != nil {
		name = nodeText(ident, file.Content)
	}
	if name == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Schema",
		Subtype:            "enum",
		SourceFile:         file.Path,
		Language:           "protobuf",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "enum " + name,
		EnrichmentRequired: false,
	}, true
}
