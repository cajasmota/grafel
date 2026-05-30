package cpp_test

// type_alias_test.go — fixture tests for type_alias.go

import "testing"

func TestTypeAliasTypedef(t *testing.T) {
	src := `typedef unsigned long size_t;`
	ents := extract(t, "custom_cpp_type_alias", fi("types.h", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "size_t") {
		t.Errorf("expected size_t type_alias entity, got %v", ents)
	}
}

func TestTypeAliasTypedefStruct(t *testing.T) {
	src := `typedef struct Node_ Node;`
	ents := extract(t, "custom_cpp_type_alias", fi("node.h", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "Node") {
		t.Errorf("expected Node type_alias from typedef struct, got %v", ents)
	}
}

func TestTypeAliasTypedefPointer(t *testing.T) {
	src := `typedef void (*Callback)(int);`
	ents := extract(t, "custom_cpp_type_alias", fi("callbacks.h", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "Callback") {
		t.Errorf("expected Callback type_alias from typedef function pointer, got %v", ents)
	}
}

func TestTypeAliasUsing(t *testing.T) {
	src := `using MyInt = int;`
	ents := extract(t, "custom_cpp_type_alias", fi("types.hpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "MyInt") {
		t.Errorf("expected MyInt type_alias from using declaration, got %v", ents)
	}
}

func TestTypeAliasUsingTemplate(t *testing.T) {
	src := `using StringVec = std::vector<std::string>;`
	ents := extract(t, "custom_cpp_type_alias", fi("types.hpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "StringVec") {
		t.Errorf("expected StringVec type_alias from using alias, got %v", ents)
	}
}

func TestTypeAliasTemplateUsing(t *testing.T) {
	src := `template<typename T> using Ptr = std::shared_ptr<T>;`
	ents := extract(t, "custom_cpp_type_alias", fi("types.hpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "Ptr") {
		t.Errorf("expected Ptr template alias from using declaration, got %v", ents)
	}
}

func TestTypeAliasC(t *testing.T) {
	// Also works for C files
	src := `typedef int (*compare_fn)(const void*, const void*);`
	ents := extract(t, "custom_cpp_type_alias", fi("compare.c", "c", src))
	if !containsEntity(ents, "SCOPE.Schema", "compare_fn") {
		t.Errorf("expected compare_fn type_alias in C file, got %v", ents)
	}
}

func TestTypeAliasNoMatch(t *testing.T) {
	src := `#include <vector>
int main() { return 0; }`
	ents := extract(t, "custom_cpp_type_alias", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestTypeAliasWrongLanguage(t *testing.T) {
	src := `typedef int MyInt;`
	ents := extract(t, "custom_cpp_type_alias", fi("main.py", "python", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}
