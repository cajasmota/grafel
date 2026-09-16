using System.Threading.Tasks;

namespace AspNetCoreMini.Services
{
    public class UserService : IUserService
    {
        public async Task<User> GetByIdAsync(int id)
        {
            // stubbed: real impl would query a DbContext
            return null;
        }

        public async Task<User> CreateAsync(CreateUserRequest request)
        {
            return new User { Id = 1, Name = request.Name };
        }

        public async Task<bool> DeleteAsync(int id)
        {
            return true;
        }
    }

    // Issue #7068 — the ONLY `foreach` in the entire golden corpus.
    //
    // The C# extractor matched the node type `for_each_statement`, which the
    // grammar does not have, so no `foreach` loop variable was ever typed and
    // every call on one emitted a BARE-NAME CALLS target. Nothing in CI graded
    // that code path: the fix's only liveness evidence was a corpus measurement
    // that does not run here. These three declarations supply it.
    //
    // `entry.Touch()` MUST resolve to `AuditEntry.Touch` and MUST NOT appear as
    // a bare `Touch` — both directions are pinned in expected.json, because a
    // forbidden-row with no positive row cannot tell "foreach types correctly"
    // from "nothing is extracted at all".
    public class AuditEntry
    {
        public void Touch() {}
    }

    // THE COMPETITOR, and the reason this fixture can fail at all.
    //
    // With only ONE `Touch()` in the file, this fixture graded NOTHING. Measured,
    // not assumed: with the node-type literal reverted to the dead
    // `for_each_statement`, the extractor emitted the bare `Touch` — and the
    // gate still reported relationship_found 14/14 with 0 forbidden hits,
    // byte-identical to the fixed tree. The resolver's same-file leaf-name tier
    // had rewritten the bare `Touch` to `AuditEntry.Touch` because it was the
    // only candidate, laundering an untyped guess into a target that looks
    // exactly like a confident bind (#7071).
    //
    // A second same-named method makes that tier refuse, so the ONLY way to
    // reach `AuditEntry.Touch` is for the extractor to have typed the loop
    // variable. Verified in both directions.
    public class AuditNote
    {
        public void Touch() {}
    }

    // #7068 review round 3 — the F-A hazard, CI-gated.
    //
    // The foreach arm must not take a name some OTHER binding form already
    // bound. local_declaration_statement is the only binding form the extractor
    // collects, so an earlier revision guarded by consulting its own local-type
    // map — which answers "did we TYPE this name?", not "is this name TAKEN?".
    // A using statement was invisible to it, so Drain below emitted
    // AuditConn.Close as AuditEntry.Close: an Order-shaped fabrication on a type
    // that has no such method, replacing an edge that had been CORRECT.
    public class AuditConn : System.IDisposable
    {
        public void Close() {}
        public void Dispose() {}
    }

    public class AuditLog
    {
        public void Replay(System.Collections.Generic.List<AuditEntry> entries)
        {
            foreach (AuditEntry entry in entries)
            {
                entry.Touch();
            }
        }

        private AuditConn OpenConn() { return null; }

        // NOTE ON LEGALITY, because it is load-bearing and unverified: no C#
        // compiler exists in this environment. This rests on one rule — a
        // foreach variable's scope is its own statement, so the sibling using
        // statement may reuse the name `c` without CS0136. Every fixture in
        // this arm rests on that same rule; none has been compiler-checked.
        public void Drain(System.Collections.Generic.List<AuditEntry> entries)
        {
            foreach (AuditEntry c in entries) { }
            using (AuditConn c = OpenConn())
            {
                c.Close();
            }
        }
    }
}
