// config_consumer.go — supplemental pass that emits DEPENDS_ON_CONFIG edges
// from Spring / MicroProfile Java code that reads a configuration key to a
// shared config-key entity (issue #3641, epic #3625).
//
// Detected reader shapes (literal keys only — honest-partial):
//
//	@Value("${app.timeout}")                 → config:app.timeout  (field/param)
//	@Value("${app.timeout:30}")              → config:app.timeout  (default stripped)
//	@ConfigurationProperties(prefix="app")   → config:app          (class)
//	@ConfigurationProperties("app")          → config:app
//	env.getProperty("server.port")           → config:server.port  (method body)
//	environment.getProperty("server.port")   → config:server.port
//	@ConfigProperty(name="db.url")           → config:db.url       (MicroProfile)
//
// Field-level / class-level annotations attach the edge to the enclosing CLASS
// entity (the configuration bean), so the bean is the consumer node. Method
// body getProperty calls attach to the enclosing METHOD entity.
//
// Mirrors the Python config_consumer DEPENDS_ON_CONFIG shape at config-KEY
// granularity (one shared node per key) so config:<key>'s inbound edges are the
// config-change blast radius.

package java

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

var (
	// @Value("${app.timeout}") or @Value("${app.timeout:default}").
	// Captures the key, dropping any ":default" suffix.
	javaValueRe = regexp.MustCompile(`\$\{([A-Za-z0-9._\-]{1,256}?)(?::[^}]*)?\}`)
	// @ConfigProperty(name = "db.url") — MicroProfile.
	javaConfigPropertyRe = regexp.MustCompile(`name\s*=\s*"([A-Za-z0-9._\-]{1,256})"`)
)

// emitConfigConsumerEdges scans the file's class / method bodies for config-read
// shapes and appends config-key entities + DEPENDS_ON_CONFIG edges to entities.
//
// entities[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitConfigConsumerEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	var reads []extractor.ConfigRead

	var walk func(n ts.Node, enclosingClass, enclosingMethod string)
	walk = func(n ts.Node, enclosingClass, enclosingMethod string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "class_declaration", "record_declaration":
			cls := childFieldText(n, "name", file.Content)
			// Class-level @ConfigurationProperties(prefix="app").
			for _, key := range configurationPropertiesKeys(n, file.Content) {
				reads = append(reads, extractor.ConfigRead{Key: key, FromName: cls, Pattern: "configuration_properties"})
			}
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), cls, "")
				}
			}
			return
		case "method_declaration", "constructor_declaration":
			leaf := childFieldText(n, "name", file.Content)
			method := leaf
			if enclosingClass != "" && leaf != "" {
				method = enclosingClass + "." + leaf
			}
			// Annotations on the method/params (e.g. @Value on a constructor
			// param) name the enclosing CLASS bean as the consumer.
			for _, key := range valueAndConfigPropertyKeys(n, file.Content) {
				reads = append(reads, extractor.ConfigRead{Key: key, FromName: enclosingClass, Pattern: "value_annotation"})
			}
			if body := n.ChildByFieldName("body"); body != nil {
				walkMethodBody(body, file.Content, method, &reads)
			}
			return
		case "field_declaration":
			// @Value("${...}") / @ConfigProperty(name="...") on a field →
			// the enclosing class bean is the consumer.
			for _, key := range valueAndConfigPropertyKeys(n, file.Content) {
				reads = append(reads, extractor.ConfigRead{Key: key, FromName: enclosingClass, Pattern: "value_annotation"})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosingClass, enclosingMethod)
		}
	}
	walk(root, "", "")

	extractor.EmitConfigReads(entities, "java", reads)
}

// walkMethodBody scans a method body for env.getProperty("key") calls and
// appends the resolved reads to *reads under the method's Name.
func walkMethodBody(n ts.Node, src []byte, method string, reads *[]extractor.ConfigRead) {
	if n == nil {
		return
	}
	if n.Type() == "method_invocation" {
		if key := getPropertyKey(n, src); key != "" {
			*reads = append(*reads, extractor.ConfigRead{Key: key, FromName: method, Pattern: "get_property"})
		}
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		walkMethodBody(n.Child(i), src, method, reads)
	}
}

// valueAndConfigPropertyKeys returns every config key declared by @Value or
// @ConfigProperty annotations attached anywhere under node (a field or method
// declaration). We scan the node's modifier text for annotations.
func valueAndConfigPropertyKeys(node ts.Node, src []byte) []string {
	var keys []string
	for _, ann := range findAnnotations(node, src) {
		name, argText := ann.name, ann.args
		switch name {
		case "Value":
			for _, m := range javaValueRe.FindAllStringSubmatch(argText, -1) {
				keys = append(keys, m[1])
			}
		case "ConfigProperty":
			if m := javaConfigPropertyRe.FindStringSubmatch(argText); m != nil {
				keys = append(keys, m[1])
			}
		}
	}
	return keys
}

// configurationPropertiesKeys returns the prefix declared by a class-level
// @ConfigurationProperties(prefix="app") / @ConfigurationProperties("app").
func configurationPropertiesKeys(classNode ts.Node, src []byte) []string {
	var keys []string
	for _, ann := range findAnnotations(classNode, src) {
		if ann.name != "ConfigurationProperties" {
			continue
		}
		// prefix = "app"  OR  value = "app"  OR  bare "app"
		if m := regexp.MustCompile(`(?:prefix|value)\s*=\s*"([A-Za-z0-9._\-]{1,256})"`).FindStringSubmatch(ann.args); m != nil {
			keys = append(keys, m[1])
			continue
		}
		if m := regexp.MustCompile(`^\s*"([A-Za-z0-9._\-]{1,256})"\s*$`).FindStringSubmatch(ann.args); m != nil {
			keys = append(keys, m[1])
		}
	}
	return keys
}

type javaAnnotation struct {
	name string
	args string
}

// findAnnotations walks the modifiers of node (only the immediate modifiers
// child, not nested declarations) and returns each annotation's simple name
// plus its raw argument text.
func findAnnotations(node ts.Node, src []byte) []javaAnnotation {
	var out []javaAnnotation
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() != "modifiers" {
			continue
		}
		for j := 0; j < int(child.ChildCount()); j++ {
			a := child.Child(j)
			switch a.Type() {
			case "annotation", "marker_annotation":
				nameNode := a.ChildByFieldName("name")
				argsNode := a.ChildByFieldName("arguments")
				name := ""
				if nameNode != nil {
					name = lastIdent(nodeText(nameNode, src))
				}
				args := ""
				if argsNode != nil {
					args = nodeText(argsNode, src)
				}
				out = append(out, javaAnnotation{name: name, args: args})
			}
		}
	}
	return out
}

// lastIdent returns the final dotted segment of a (possibly qualified)
// annotation name: "org.springframework.beans.factory.annotation.Value" → "Value".
func lastIdent(s string) string {
	if idx := strings.LastIndex(s, "."); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// getPropertyKey returns the literal key of an environment.getProperty("key")
// call, or "" when the call doesn't match or the key is non-literal.
func getPropertyKey(call ts.Node, src []byte) string {
	nameNode := call.ChildByFieldName("name")
	objNode := call.ChildByFieldName("object")
	argsNode := call.ChildByFieldName("arguments")
	if nameNode == nil || objNode == nil || argsNode == nil {
		return ""
	}
	if nodeText(nameNode, src) != "getProperty" {
		return ""
	}
	// Gate on a receiver that looks like a Spring Environment.
	recv := strings.ToLower(strings.TrimSpace(nodeText(objNode, src)))
	if recv != "env" && recv != "environment" && !strings.HasSuffix(recv, "environment") {
		return ""
	}
	// First argument must be a literal string.
	for i := 0; i < int(argsNode.NamedChildCount()); i++ {
		arg := argsNode.NamedChild(i)
		if arg.Type() == "string_literal" {
			return javaStripString(nodeText(arg, src))
		}
		// Stop at the first arg position; a non-literal first arg => dynamic.
		break
	}
	return ""
}

// javaStripString removes surrounding double-quotes from a Java string literal.
func javaStripString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
