import org.quartz.*;
import org.quartz.impl.StdSchedulerFactory;
import static org.quartz.JobBuilder.newJob;
import static org.quartz.TriggerBuilder.newTrigger;

/**
 * Producer: builds and schedules Quartz jobs.
 */
public class AppScheduler {
    public void start() throws Exception {
        Scheduler scheduler = StdSchedulerFactory.getDefaultScheduler();

        // Producer: build email job detail
        JobDetail emailJob = JobBuilder.newJob(SendEmailJob.class)
                .withIdentity("email-job", "email-group")
                .build();

        // Producer: build trigger
        Trigger emailTrigger = TriggerBuilder.newTrigger()
                .withIdentity("email-trigger", "email-group")
                .startNow()
                .build();

        // Producer: build report job using static import
        JobDetail reportJob = newJob(GenerateReportJob.class)
                .withIdentity("report-job", "report-group")
                .build();

        // Producer: schedule both jobs
        scheduler.scheduleJob(emailJob, emailTrigger);
        scheduler.scheduleJob(reportJob, emailTrigger);
        scheduler.start();
    }
}
