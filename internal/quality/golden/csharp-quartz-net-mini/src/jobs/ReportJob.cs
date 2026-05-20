using Quartz;

namespace Workers.Jobs
{
    [DisallowConcurrentExecution]
    public class ReportJob : IJob
    {
        public async Task Execute(IJobExecutionContext context)
        {
            // Consumer: generate a report
        }
    }

    public class EmailJob : IJob
    {
        public async Task Execute(IJobExecutionContext context)
        {
            // Consumer: send an email
        }
    }
}
