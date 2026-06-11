package substrate

// effect_sinks_python_queryset_read_4691_test.go — receiver-typed read reach
// for the Python effect sniffer (#4691, follow-up to #4668/#4694).
//
// #4694 bare-matched the DISTINCTIVE queryset read verbs (filter/exclude/...)
// but kept the builtin-colliding terminals (.get/.first/.all/.count/.values/...)
// gated to the `.objects.<verb>` manager form to avoid dict.get/list.count
// false positives. #4691 closes that gap with lightweight receiver typing:
// when a receiver is known to hold a QuerySet/Manager, its ambiguous read
// terminals ARE db_read; on an untyped receiver they STAY non-read.

import "testing"

// A: a queryset bound to a local, then a `.get(pk=...)` terminal on it.
func TestPythonQuerysetTypedGet_4691(t *testing.T) {
	src := `
def fetch(pk):
    qs = Article.objects.filter(active=True)
    obj = qs.get(pk=pk)
    return obj
`
	by := groupByEffect(sniffEffectsPython(src))
	mustHave(t, by, EffectDBRead, "fetch")
}

// B: inherent queryset handle chained directly to an ambiguous terminal.
func TestPythonGetQuerysetFirst_4691(t *testing.T) {
	src := `
class ContractRepository:
    def latest(self):
        return self.get_queryset().first()
    def head(self):
        return self.queryset.count()
`
	by := groupByEffect(sniffEffectsPython(src))
	mustHave(t, by, EffectDBRead, "latest")
	mustHave(t, by, EffectDBRead, "head")
}

// C (negative, MUST stay pure): builtin .get on a dict and .count on a Mock/
// list — receivers that are NOT queryset-typed — earn no db_read credit.
func TestPythonAmbiguousVerbsNoFalsePositive_4691(t *testing.T) {
	src := `
def lookup(key):
    d = {}
    return d.get(key)

def tally(items):
    m = Mock()
    return m.count()
`
	by := groupByEffect(sniffEffectsPython(src))
	mustNotHave(t, by, EffectDBRead, "lookup")
	mustNotHave(t, by, EffectDBRead, "tally")
}

// D: chained reassignment propagates typing; terminal read on the reassigned
// name is still credited (feeds the controller→service propagation).
func TestPythonChainedQuerysetReassign_4691(t *testing.T) {
	src := `
def search(active, ids):
    qs = Article.objects.all()
    qs = qs.exclude(id__in=ids)
    return qs.exists()
`
	by := groupByEffect(sniffEffectsPython(src))
	mustHave(t, by, EffectDBRead, "search")
}

// get_object_or_404 / get_list_or_404 are distinctive shortcut reads.
func TestPythonShortcutReadHelpers_4691(t *testing.T) {
	src := `
def detail(pk):
    return get_object_or_404(Article, pk=pk)

def listing():
    return get_list_or_404(Article)
`
	by := groupByEffect(sniffEffectsPython(src))
	mustHave(t, by, EffectDBRead, "detail")
	mustHave(t, by, EffectDBRead, "listing")
}
