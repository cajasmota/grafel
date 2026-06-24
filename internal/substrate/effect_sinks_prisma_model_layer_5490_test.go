package substrate

// effect_sinks_prisma_model_layer_5490_test.go — the Prisma data-access layer
// (#5490). A model module (`*.server.ts`) exports functions that wrap the Prisma
// client; each delegate call is credited to its enclosing function as a
// db_read / db_write effect with a MODEL-bearing sink tag (`prisma.read:User`),
// so the data-access flow is queryable by model and ties to the #5489 model
// entity. The receiver gate (`prisma`/`db`/`tx`) keeps an unrelated `.create()`
// from being misread as a Prisma data-access effect.

import "testing"

// modelSink returns the set of model-bearing prisma sink tags carried by fn for
// effect eff (e.g. "prisma.read:User"). Used to assert the model attribute.
func prismaModelSinks(ms []EffectMatch, fn string, eff Effect) map[string]bool {
	out := map[string]bool{}
	for _, m := range ms {
		if m.Function == fn && m.Effect == eff {
			out[m.Sink] = true
		}
	}
	return out
}

// The data-access layer: models/user/user.server.ts. Read fns (findMany/count),
// write fns (create/update/delete), and a $transaction. Each effect lands on the
// right function with the right model in the sink tag.
const prismaUserServerTS = `
import { prisma } from '~/db.server';

export async function getUsers() {
  return prisma.user.findMany({ where: { active: true } });
}

export async function countUsers() {
  return prisma.user.count();
}

export async function createUser(data) {
  return prisma.user.create({ data });
}

export async function updateUser(id, data) {
  return prisma.user.update({ where: { id }, data });
}

export async function deleteUser(id) {
  return prisma.user.delete({ where: { id } });
}

export async function transferPost(postId, toUserId) {
  return prisma.$transaction(async (tx) => {
    await tx.post.update({ where: { id: postId }, data: { authorId: toUserId } });
    return tx.user.findUnique({ where: { id: toUserId } });
  });
}
`

func TestPrismaModelLayerReadEffects_5490(t *testing.T) {
	ms := sniffEffectsJSTS(prismaUserServerTS)
	by := groupByEffect(ms)
	mustHave(t, by, EffectDBRead, "getUsers")
	mustHave(t, by, EffectDBRead, "countUsers")

	// Model attribute is captured in the sink tag.
	if got := prismaModelSinks(ms, "getUsers", EffectDBRead); !got["prisma.read:User"] {
		t.Errorf("getUsers: expected sink prisma.read:User, got %v", got)
	}
	if got := prismaModelSinks(ms, "countUsers", EffectDBRead); !got["prisma.read:User"] {
		t.Errorf("countUsers: expected sink prisma.read:User, got %v", got)
	}
}

func TestPrismaModelLayerWriteEffects_5490(t *testing.T) {
	ms := sniffEffectsJSTS(prismaUserServerTS)
	by := groupByEffect(ms)
	mustHave(t, by, EffectDBWrite, "createUser")
	mustHave(t, by, EffectDBWrite, "updateUser")
	mustHave(t, by, EffectDBWrite, "deleteUser")

	for _, fn := range []string{"createUser", "updateUser", "deleteUser"} {
		if got := prismaModelSinks(ms, fn, EffectDBWrite); !got["prisma.write:User"] {
			t.Errorf("%s: expected sink prisma.write:User, got %v", fn, got)
		}
	}
}

// $transaction: the interactive-transaction callback handle (`tx`) is a Prisma
// receiver, so the inner tx.post.update() write and tx.user.findUnique() read
// are both credited to the enclosing function, each with its own model.
func TestPrismaModelLayerTransaction_5490(t *testing.T) {
	ms := sniffEffectsJSTS(prismaUserServerTS)
	by := groupByEffect(ms)
	mustHave(t, by, EffectDBWrite, "transferPost") // tx.post.update
	mustHave(t, by, EffectDBRead, "transferPost")  // tx.user.findUnique

	if got := prismaModelSinks(ms, "transferPost", EffectDBWrite); !got["prisma.write:Post"] {
		t.Errorf("transferPost write: expected sink prisma.write:Post, got %v", got)
	}
	if got := prismaModelSinks(ms, "transferPost", EffectDBRead); !got["prisma.read:User"] {
		t.Errorf("transferPost read: expected sink prisma.read:User, got %v", got)
	}
}

// db.<model> receiver (the other canonical Prisma client name) is gated in too.
func TestPrismaModelLayerDBReceiver_5490(t *testing.T) {
	src := `
import { db } from '~/db.server';
export async function listPosts() {
  return db.post.findMany();
}
export async function publish(id) {
  return db.post.update({ where: { id }, data: { published: true } });
}
`
	ms := sniffEffectsJSTS(src)
	if got := prismaModelSinks(ms, "listPosts", EffectDBRead); !got["prisma.read:Post"] {
		t.Errorf("listPosts: expected sink prisma.read:Post, got %v", got)
	}
	if got := prismaModelSinks(ms, "publish", EffectDBWrite); !got["prisma.write:Post"] {
		t.Errorf("publish: expected sink prisma.write:Post, got %v", got)
	}
}

// Negative: a non-Prisma receiver calling .create()/.update() must NOT be
// credited with a Prisma model-bearing sink. (The generic orm.write bare-match
// may still fire — that's existing cross-ORM behavior — but the receiver-gated
// prisma.* sink, which carries the model and the data-access semantics, must
// not.) This is the receiver gate that stops unrelated `.create()` from being
// misread as Prisma data access.
func TestPrismaModelLayerNonPrismaReceiverNotCredited_5490(t *testing.T) {
	src := `
import { EventEmitter } from 'events';
export function makeBus() {
  const bus = new EventEmitter();
  bus.create({ kind: 'thing' });        // not a prisma receiver
  return widgetFactory.update({ x: 1 }); // not a prisma receiver
}
`
	ms := sniffEffectsJSTS(src)
	for _, m := range ms {
		if m.Function == "makeBus" && (m.Sink == "prisma.write:Bus" || m.Sink == "prisma.write:WidgetFactory" ||
			m.Sink == "prisma.read:Bus" || m.Sink == "prisma.read:WidgetFactory") {
			t.Errorf("non-prisma receiver was misread as a Prisma data-access effect: %+v", m)
		}
		// No prisma.* model sink at all should be attributed here.
		if m.Function == "makeBus" && len(m.Sink) >= 7 && m.Sink[:7] == "prisma." {
			t.Errorf("unexpected prisma model sink on non-prisma receiver: %+v", m)
		}
	}
}
