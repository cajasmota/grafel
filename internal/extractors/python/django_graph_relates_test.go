// django_graph_relates_test.go — value-asserting tests for the GRAPH_RELATES
// model↔model edges (with cardinality) emitted alongside Django relational
// REFERENCES edges.

package python_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// djangoGraphRelates returns the first GRAPH_RELATES edge whose ToID contains
// the given target class name and whose cardinality matches, or nil.
func djangoGraphRelates(ents []types.EntityRecord, targetClass string) *types.RelationshipRecord {
	for ei := range ents {
		for ri := range ents[ei].Relationships {
			r := &ents[ei].Relationships[ri]
			if r.Kind != string(types.RelationshipKindGraphRelates) {
				continue
			}
			// ToID is a structural-ref ending in ":<ClassName>".
			if len(r.ToID) >= len(targetClass) &&
				r.ToID[len(r.ToID)-len(targetClass):] == targetClass {
				return r
			}
		}
	}
	return nil
}

func TestDjangoGraphRelatesForeignKey(t *testing.T) {
	src := `from django.db import models

class Author(models.Model):
    name = models.CharField(max_length=200)

class Book(models.Model):
    title = models.CharField(max_length=500)
    author = models.ForeignKey(Author, on_delete=models.CASCADE)
    tags = models.ManyToManyField('Tag')
    cover = models.OneToOneField('Cover', on_delete=models.CASCADE)
`
	ents := extractDjango(t, src)

	fk := djangoGraphRelates(ents, "Author")
	if fk == nil {
		t.Fatal("expected GRAPH_RELATES Book → Author (ForeignKey)")
	}
	if fk.Properties["cardinality"] != "many_to_one" {
		t.Errorf("ForeignKey cardinality: want many_to_one, got %q", fk.Properties["cardinality"])
	}

	m2m := djangoGraphRelates(ents, "Tag")
	if m2m == nil || m2m.Properties["cardinality"] != "many_to_many" {
		t.Errorf("expected GRAPH_RELATES Book → Tag many_to_many, got %v", m2m)
	}

	o2o := djangoGraphRelates(ents, "Cover")
	if o2o == nil || o2o.Properties["cardinality"] != "one_to_one" {
		t.Errorf("expected GRAPH_RELATES Book → Cover one_to_one, got %v", o2o)
	}
}

// A non-relational field (CharField) must not emit a GRAPH_RELATES edge.
func TestDjangoGraphRelatesScalarFieldNoEdge(t *testing.T) {
	src := `from django.db import models

class Author(models.Model):
    name = models.CharField(max_length=200)
    age = models.IntegerField()
`
	ents := extractDjango(t, src)
	for ei := range ents {
		for _, r := range ents[ei].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				t.Errorf("unexpected GRAPH_RELATES edge for scalar-only model: %v", r)
			}
		}
	}
}
