package kotlin_test

import "testing"

// kotlinx_serialization_test.go — value-asserting tests for the
// kotlinx.serialization DTO extractor (record
// lang.kotlin.framework.kotlinx-serialization, cell dto_extraction → full).

const kxSerializableSrc = `
package com.example.dto

import kotlinx.serialization.Serializable
import kotlinx.serialization.SerialName
import kotlinx.serialization.Required
import kotlinx.serialization.Transient
import kotlinx.serialization.Polymorphic

@Serializable
data class User(
    val id: Long,
    @SerialName("user_name") val name: String,
    val age: Int = 0,
    val nickname: String? = null,
    @Required val email: String,
    @Transient val cached: String = "",
    @Polymorphic val payload: Any
)

class NotSerializable(val x: Int)
`

func TestKotlinxSerialization_DTOFields(t *testing.T) {
	ents := extract(t, "custom_kotlin_kotlinx_serialization", fi("User.kt", "kotlin", kxSerializableSrc))

	dto := findEntity(ents, "SCOPE.Schema", "User")
	if dto == nil {
		t.Fatalf("[kx] expected DTO entity User, got %v", ents)
	}
	if dto.Subtype != "dto" {
		t.Errorf("[kx] User subtype = %q, want dto", dto.Subtype)
	}
	if dto.Props["serializable"] != "true" {
		t.Errorf("[kx] User serializable = %q, want true", dto.Props["serializable"])
	}

	// id: Long, non-nullable, wire name == field name.
	if got := dto.Props["prop.id.type"]; got != "Long" {
		t.Errorf("[kx] prop.id.type = %q, want Long", got)
	}
	if got := dto.Props["prop.id.nullable"]; got != "false" {
		t.Errorf("[kx] prop.id.nullable = %q, want false", got)
	}
	if got := dto.Props["prop.id.wire_name"]; got != "id" {
		t.Errorf("[kx] prop.id.wire_name = %q, want id", got)
	}

	// name: @SerialName("user_name") wire override.
	if got := dto.Props["prop.name.wire_name"]; got != "user_name" {
		t.Errorf("[kx] prop.name.wire_name = %q, want user_name", got)
	}
	if got := dto.Props["prop.name.type"]; got != "String" {
		t.Errorf("[kx] prop.name.type = %q, want String", got)
	}

	// age: Int with default 0.
	if got := dto.Props["prop.age.type"]; got != "Int" {
		t.Errorf("[kx] prop.age.type = %q, want Int", got)
	}
	if got := dto.Props["prop.age.default"]; got != "0" {
		t.Errorf("[kx] prop.age.default = %q, want 0", got)
	}

	// nickname: nullable String.
	if got := dto.Props["prop.nickname.nullable"]; got != "true" {
		t.Errorf("[kx] prop.nickname.nullable = %q, want true", got)
	}
	if got := dto.Props["prop.nickname.type"]; got != "String?" {
		t.Errorf("[kx] prop.nickname.type = %q, want String?", got)
	}

	// email: @Required.
	if got := dto.Props["prop.email.required"]; got != "true" {
		t.Errorf("[kx] prop.email.required = %q, want true", got)
	}

	// cached: @Transient (excluded from wire payload).
	if got := dto.Props["prop.cached.transient"]; got != "true" {
		t.Errorf("[kx] prop.cached.transient = %q, want true", got)
	}

	// payload: @Polymorphic.
	if got := dto.Props["prop.payload.polymorphic"]; got != "true" {
		t.Errorf("[kx] prop.payload.polymorphic = %q, want true", got)
	}
}

func TestKotlinxSerialization_OnlySerializableClasses(t *testing.T) {
	ents := extract(t, "custom_kotlin_kotlinx_serialization", fi("User.kt", "kotlin", kxSerializableSrc))
	// NotSerializable must NOT be emitted — extractor is @Serializable-gated.
	if findEntity(ents, "SCOPE.Schema", "NotSerializable") != nil {
		t.Error("[kx] NotSerializable should not be emitted (no @Serializable)")
	}
}

func TestKotlinxSerialization_ClassSerialName(t *testing.T) {
	src := `
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerialName

@Serializable
@SerialName("event_v2")
data class Event(val kind: String)
`
	ents := extract(t, "custom_kotlin_kotlinx_serialization", fi("Event.kt", "kotlin", src))
	dto := findEntity(ents, "SCOPE.Schema", "Event")
	if dto == nil {
		t.Fatalf("[kx] expected Event DTO, got %v", ents)
	}
	if got := dto.Props["serial_name"]; got != "event_v2" {
		t.Errorf("[kx] Event serial_name = %q, want event_v2", got)
	}
}

func TestKotlinxSerialization_NoSerializable(t *testing.T) {
	src := `
package com.example
data class Plain(val a: Int)
`
	ents := extract(t, "custom_kotlin_kotlinx_serialization", fi("Plain.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("[kx] expected 0 entities without @Serializable, got %d", len(ents))
	}
}

func TestKotlinxSerialization_EmptySource(t *testing.T) {
	ents := extract(t, "custom_kotlin_kotlinx_serialization", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("[kx] expected 0 entities for empty file, got %d", len(ents))
	}
}
