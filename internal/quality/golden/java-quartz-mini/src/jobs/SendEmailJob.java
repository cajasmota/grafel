import org.quartz.Job;
import org.quartz.JobExecutionContext;
import org.quartz.JobExecutionException;
import org.quartz.DisallowConcurrentExecution;

/**
 * Consumer: sends emails as a Quartz job.
 */
public class SendEmailJob implements Job {
    @Override
    public void execute(JobExecutionContext context) throws JobExecutionException {
        // consumer logic
    }
}

/**
 * Consumer: generates a PDF report. Marked @DisallowConcurrentExecution
 * to prevent concurrent runs.
 */
@DisallowConcurrentExecution
public class GenerateReportJob implements Job {
    @Override
    public void execute(JobExecutionContext context) throws JobExecutionException {
        // consumer logic
    }
}
