using Hangfire;

namespace Workers.Jobs
{
    public class EmailJob : IBackgroundJob
    {
        public async Task Execute(PerformContext ctx)
        {
            // Consumer: sends an email
        }
    }

    public class CleanupJob : IBackgroundJob
    {
        [AutomaticRetry(Attempts = 3)]
        public async Task Execute(PerformContext ctx)
        {
            // Consumer: cleans up stale data
        }
    }
}
