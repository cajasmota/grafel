// Proving fixture for BullMQ topic_attribution (#2865).
//
// The engine pass internal/engine/bullmq_edges.go emits one SCOPE.Queue
// entity Named `bullmq:emails` for the queue below, plus a PUBLISHES_TO edge
// from enqueueWelcome (carrying job_name=welcome) and a SUBSCRIBES_TO edge
// from the worker registration. A matching `new Worker('emails', …)` in a
// separate consumer service produces the identical `bullmq:emails` node, so
// the cross-repo topic linker (internal/links/topic_pass.go) joins producer
// and consumer with no BullMQ-specific matching code.
import { Queue, Worker } from 'bullmq';

const emailQueue = new Queue('emails', { connection });

export async function enqueueWelcome(userId: string): Promise<void> {
  await emailQueue.add('welcome', { userId });
}

// Consumer side (co-located here for the fixture; in a real deployment this
// lives in a separate worker service).
new Worker('emails', async (job) => {
  await sendEmail(job.data);
});
