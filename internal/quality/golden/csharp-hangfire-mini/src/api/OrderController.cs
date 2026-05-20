using Hangfire;

namespace Api.Controllers
{
    public class OrderController
    {
        public void PlaceOrder(int orderId)
        {
            // Producer: fire-and-forget
            BackgroundJob.Enqueue(() => EmailService.SendConfirmation(orderId));
        }

        public void ConfigureRecurring()
        {
            // Producer: recurring job
            RecurringJob.AddOrUpdate("daily-cleanup", () => CleanupService.Run(), Cron.Daily);

            // Producer: typed enqueue
            BackgroundJob.Enqueue<IEmailService>(x => x.SendNewsletter());
        }
    }
}
