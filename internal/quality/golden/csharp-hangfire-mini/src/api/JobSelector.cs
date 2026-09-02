using Hangfire;

namespace Api.Controllers
{
    // #6742 over-fire bait. Every colon below is a colon that is NOT a base
    // list, and none of them may produce a class-hierarchy edge:
    //
    //   `where T : IJobFilter`  a generic constraint
    //   `enum ... : byte`       an enum's underlying storage type
    //   `cond ? A : B`          a ternary
    //   `case 1:` / `default:`  switch labels
    //
    // Recall cannot see an edge that should not exist, so these are graded by
    // forbidden_relationships rows in expected.json rather than by must-haves.
    // They live in api/, while the positive `EmailJob : IBackgroundJob` case
    // lives in jobs/ — a file-scoped regression cannot green both.
    public class JobSelector<T> where T : IJobFilter, new()
    {
        public string Describe(int mode)
        {
            var label = mode > 0 ? Ascending : Descending;
            switch (mode)
            {
                case 1: return label;
                default: return Ascending;
            }
        }

        private const string Ascending = "asc";
        private const string Descending = "desc";
    }

    public enum JobPriority : byte
    {
        Low,
        High
    }
}
