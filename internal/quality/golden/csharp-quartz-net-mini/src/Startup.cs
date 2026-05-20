using Quartz;

namespace App
{
    public class Startup
    {
        public async Task ConfigureScheduler(IScheduler scheduler)
        {
            // Producer: build job detail
            var reportJob = JobBuilder.Create<ReportJob>()
                .WithIdentity("report-job", "reports")
                .Build();

            var emailJob = JobBuilder.Create<EmailJob>()
                .WithIdentity("email-job")
                .Build();

            // Producer: build trigger
            var trigger = TriggerBuilder.Create()
                .WithIdentity("report-trigger")
                .StartNow()
                .Build();

            // Producer: schedule job
            await scheduler.ScheduleJob(reportJob, trigger);
        }
    }
}
