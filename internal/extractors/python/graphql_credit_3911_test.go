// Value-asserting probes for #3911: the python type-system and config-consumer
// extractors are LANGUAGE-dispatched (they run for every .py file via the
// registered "python" extractor, with zero framework gating). These probes run
// the real end-to-end python extractor over graphene + ariadne source and
// assert that type-system annotations and DEPENDS_ON_CONFIG edges genuinely
// fire — so the Type System + config_consumption cells can be credited
// honestly for graphene/ariadne (mirroring strawberry, which cites the same
// internal/extractors/python/{types.go,config_consumer.go}).
package python_test

import (
	"strings"
	"testing"
)

// grapheneTypesSrc: a graphene module that also carries a plain dataclass DTO
// and an Enum — the type-system primitives the extractor annotates regardless
// of framework. account_id read via os.environ proves config consumption.
const grapheneTypesSrc = `
import os
import enum
from dataclasses import dataclass
import graphene
from .models import Account

class Role(enum.Enum):
    ADMIN = "admin"
    VIEWER = "viewer"

@dataclass
class AccountDTO:
    id: int
    name: str
    role: Role

class AccountType(graphene.ObjectType):
    id = graphene.ID()
    name = graphene.String()

class Query(graphene.ObjectType):
    account = graphene.Field(AccountType, account_id=graphene.ID(required=True))

    def resolve_account(self, info, account_id):
        region = os.environ.get("ACCOUNT_REGION")
        return Account.objects.get(pk=account_id, region=region)
`

// ariadneTypesSrc: an ariadne resolver module with a dataclass + settings read.
const ariadneTypesSrc = `
from django.conf import settings
from dataclasses import dataclass
from ariadne import QueryType
from .models import Account

query = QueryType()

@dataclass
class OrderDTO:
    id: int
    amount: int

@query.field("account")
def resolve_account(obj, info, account_id):
    page_size = settings.GRAPHQL_PAGE_SIZE
    return Account.objects.all()[:page_size]
`

// TestGraphene_TypeSystemFires proves type_extraction + enum_extraction:
// the dataclass DTO and the Enum get annotated by the language-dispatched
// type-system pass (internal/extractors/python/types.go).
func TestGraphene_TypeSystemFires(t *testing.T) {
	ents := extractPy(t, grapheneTypesSrc, "graphql/schema.py")

	dto := findClass(ents, "AccountDTO")
	if dto == nil {
		t.Fatal("AccountDTO class entity not found")
	}
	if got := dto.Properties["pattern_type"]; got != "dataclass" {
		t.Fatalf("AccountDTO pattern_type = %q, want dataclass", got)
	}

	role := findClass(ents, "Role")
	if role == nil {
		t.Fatal("Role class entity not found")
	}
	if got := role.Properties["pattern_type"]; got != "enum" {
		t.Fatalf("Role pattern_type = %q, want enum", got)
	}
	if got := role.Properties["enum_members"]; got != "ADMIN, VIEWER" {
		t.Fatalf("Role enum_members = %q, want \"ADMIN, VIEWER\"", got)
	}
}

// TestGraphene_ConfigConsumptionFires proves config_consumption: os.environ.get
// inside a graphene resolver emits a DEPENDS_ON_CONFIG edge from the resolver.
func TestGraphene_ConfigConsumptionFires(t *testing.T) {
	ents := extractPy(t, grapheneTypesSrc, "graphql/schema.py")
	var sawEnvEdge bool
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "DEPENDS_ON_CONFIG" && strings.Contains(r.Properties.Get("keys"), "ACCOUNT_REGION") {
				sawEnvEdge = true
			}
		}
	}
	if !sawEnvEdge {
		t.Fatal("graphene: expected a DEPENDS_ON_CONFIG edge for os.environ.get(\"ACCOUNT_REGION\")")
	}
}

// TestAriadne_TypeSystemAndConfigFire proves type_extraction (dataclass) AND
// config_consumption (settings.X) fire on ariadne source.
func TestAriadne_TypeSystemAndConfigFire(t *testing.T) {
	ents := extractPy(t, ariadneTypesSrc, "graphql/resolvers.py")

	dto := findClass(ents, "OrderDTO")
	if dto == nil {
		t.Fatal("OrderDTO class entity not found")
	}
	if got := dto.Properties["pattern_type"]; got != "dataclass" {
		t.Fatalf("OrderDTO pattern_type = %q, want dataclass", got)
	}

	var sawSettingsEdge bool
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "DEPENDS_ON_CONFIG" &&
				r.Properties.Get("config_name") == "settings" &&
				strings.Contains(r.Properties.Get("keys"), "GRAPHQL_PAGE_SIZE") {
				sawSettingsEdge = true
			}
		}
	}
	if !sawSettingsEdge {
		t.Fatal("ariadne: expected a DEPENDS_ON_CONFIG edge for settings.GRAPHQL_PAGE_SIZE")
	}
}
