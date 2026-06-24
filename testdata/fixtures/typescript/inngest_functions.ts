// Proving fixture for the Inngest durable-function extractor
// (custom_js_inngest, issue #5480, epic #5479). Each inngest.createFunction(...)
// call site is extracted as one SCOPE.Function entity named after the config
// `id`, with the trigger event/cron captured as a property. Edges
// (EMITS/TRIGGERS) are later tickets (#5482/#5483) and are NOT exercised here.

import { Inngest } from "inngest";

export const inngest = new Inngest({ id: "demo-app" });

// Modern object-config signature with an event trigger.
export const syncUser = inngest.createFunction(
  { id: "sync-user", name: "Sync User" },
  { event: "user/created" },
  async ({ event, step }) => {
    await step.run("persist", () => persistUser(event.data));
  },
);

// Second event-triggered function — must not bleed its id/event into the first.
export const sendWelcome = inngest.createFunction(
  { id: "send-welcome" },
  { event: "user/created" },
  async ({ event }) => {
    await sendEmail(event.data.email);
  },
);

// Cron-triggered function carries a cron attribute instead of an event.
export const nightlyReport = inngest.createFunction(
  { id: "nightly-report" },
  { cron: "0 0 * * *" },
  async () => {
    await buildReport();
  },
);

// #5481: producer side — inngest.send({ name }) references the same
// "user/created" event (must dedupe with the trigger above) plus a new
// "user/deleted" event, so this file declares two distinct MessageTopics.
export async function deleteUser(id: string) {
  await inngest.send({ name: "user/deleted", data: { id } });
  await inngest.send({ name: "user/created", data: { id } });
}

declare function persistUser(data: unknown): Promise<void>;
declare function sendEmail(to: string): Promise<void>;
declare function buildReport(): Promise<void>;
