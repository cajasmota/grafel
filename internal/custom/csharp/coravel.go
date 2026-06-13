// coravel.go — Coravel scheduling / queuing / mailing extraction for C#/.NET
// (#5075, spun out of #5016 / #4969). Sibling of the Quartz.NET pass
// (internal/custom/csharp/quartz_net.go) and the Hangfire pass
// (internal/custom/csharp/hangfire.go); it parses Coravel's fluent scheduler the
// same way the Quartz.NET trigger chain is parsed.
//
// Coravel (github.com/jamesmh/coravel) is a near-zero-config .NET task scheduler
// / queue / mailer. Its surfaces:
//
//   - Invocables (consumers) — class X : IInvocable { Task Invoke() } is the unit
//     of scheduled / queued work (the Coravel analogue of a Quartz IJob).
//
//   - Scheduling (producers) — inside app.Services.UseScheduler(scheduler => {...}):
//       scheduler.Schedule<SendNewsletter>().EveryMinute();
//       scheduler.Schedule<Cleanup>().DailyAt("13:00");
//       scheduler.ScheduleAsync(async () => ...).Hourly();
//       scheduler.Schedule<T>().Cron("*/5 * * * *");
//     The fluent frequency call (EveryMinute / Daily / Hourly / Weekly / Monthly /
//     Cron("...") / DailyAt("hh:mm") / Every*Minutes) is the schedule.
//
//   - Queuing (producers) — IQueue.QueueInvocable<T>() / QueueInvocableWithPayload /
//     QueueAsyncTask(...) dispatch background work to a Coravel invocable.
//
//   - Mailing (producers) — IMailer.Send(new XMailable(...)) / SendAsync send a
//     Mailable.
//
// Like the Quartz.NET / rate-limit / Polly passes, each surface adds a flat
// SCOPE marker (Coravel fluent chains span calls that do not reduce to a single
// op). The property contract answers "what work is scheduled / queued, on what
// cadence, and which invocable runs it?":
//
//	framework        — "coravel".
//	pattern_type     — invocable | schedule | queue | mail.
//	invocable        — the IInvocable type name (the unit of work). For
//	                   Schedule<T>()/QueueInvocable<T>() it is the generic arg.
//	schedule_type    — "cron" (.Cron("...")) | "interval" (EveryMinute / Hourly /
//	                   Every5Minutes / ...) | "daily" (Daily / DailyAt("hh:mm")) |
//	                   "weekly" | "monthly".
//	cron_expression  — the cron string from .Cron("...") when literal.
//	frequency        — the named fluent frequency token (EveryMinute, Hourly,
//	                   DailyAt, Every5Minutes, ...) — evidence for the cadence.
//	daily_at         — the "hh:mm" from .DailyAt("13:00") when literal.
//	interval_seconds — normalised seconds for EveryNMinutes / EverySeconds where
//	                   the fluent token carries a literal magnitude.
//	task_id          — "task:coravel:<Invocable>" — the join key between the
//	                   Schedule<T>()/QueueInvocable<T>() producer and the X :
//	                   IInvocable consumer (mirrors task:quartz.net:<T>).
//	edge_kind        — CONSUMES (invocable) | PRODUCES (schedule / queue / mail).
//
// Honest-partial: schedules / invocables behind variables or config are recorded
// without the resolved cadence. A Schedule<T>() with no recognised frequency
// token records schedule_type unresolved (omitted) rather than guessing.
//
// Closes #5075 (Coravel half).
package csharp

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_csharp_coravel", &coravelExtractor{})
}

type coravelExtractor struct{}

func (e *coravelExtractor) Language() string { return "custom_csharp_coravel" }

var (
	// class X : IInvocable — the Coravel unit of work (consumer).
	cvInvocableImplRe = regexp.MustCompile(`(?m)class\s+(\w+)\s*(?::\s*[^{]*\bIInvocable\b)`)

	// scheduler.Schedule<T>() / ScheduleAsync<T>() / ScheduleWithParams<T>()
	cvScheduleGenericRe = regexp.MustCompile(`\.\s*Schedule(?:Async|WithParams)?\s*<\s*([\w.]+)\s*>\s*\(`)
	// scheduler.Schedule(() => ...) / ScheduleAsync(async () => ...) — anonymous.
	cvScheduleAnonRe = regexp.MustCompile(`\.\s*Schedule(?:Async)?\s*\(\s*(?:async\s*)?\(`)

	// IQueue.QueueInvocable<T>() / QueueInvocableWithPayload<T,P>() — queued work.
	cvQueueInvocableRe = regexp.MustCompile(`\.\s*QueueInvocable(?:WithPayload)?\s*<\s*([\w.]+)`)
	// IQueue.QueueAsyncTask(...) / QueueTask(...) — anonymous queued work.
	cvQueueTaskRe = regexp.MustCompile(`\.\s*Queue(?:Async)?Task\s*\(`)

	// IMailer.Send(new XMailable(...)) / SendAsync(...) — mailing.
	cvMailSendRe = regexp.MustCompile(`\.\s*Send(?:Async)?\s*\(\s*new\s+([\w.]+)`)

	// Cron / named-frequency fluent tokens on a schedule chain.
	cvCronRe       = regexp.MustCompile(`\.\s*Cron\s*\(\s*"([^"]+)"`)
	cvDailyAtRe    = regexp.MustCompile(`\.\s*DailyAt\s*\(\s*"([^"]+)"`)
	cvEveryNMinRe  = regexp.MustCompile(`\.\s*Every(\d+)Minutes\s*\(`)
	cvEverySecRe   = regexp.MustCompile(`\.\s*EverySeconds\s*\(\s*(\d+)`)
	cvFrequencyRe  = regexp.MustCompile(
		`\.\s*(EveryMinute|EveryFiveMinutes|EveryTenMinutes|EveryFifteenMinutes|EveryThirtyMinutes|` +
			`EverySecond|Hourly|HourlyAt|Daily|DailyAtHour|Weekly|Monthly)\s*\(`)
)

// cvDailyFreq is the set of named frequencies that mean a once-per-day cadence.
var cvDailyFreq = map[string]bool{"Daily": true, "DailyAtHour": true}

// coravelChain returns the source window from offset `from` bounded to the next
// ';' so a co-located schedule's frequency does not bleed into this one. Mirrors
// the bounded-window scan in quartz_net.go.
func coravelChain(src string, from int) string {
	rest := src[from:]
	if semi := strings.IndexByte(rest, ';'); semi >= 0 {
		return rest[:semi]
	}
	return rest
}

// parseCoravelSchedule scans a bounded schedule chain and stamps the cadence
// properties onto ent. It is shared by the generic and anonymous schedule paths.
func parseCoravelSchedule(ent *types.EntityRecord, chain string) {
	if cm := cvCronRe.FindStringSubmatch(chain); cm != nil {
		setProps(ent, "schedule_type", "cron", "cron_expression", cm[1], "frequency", "Cron")
		return
	}
	if dm := cvDailyAtRe.FindStringSubmatch(chain); dm != nil {
		setProps(ent, "schedule_type", "daily", "daily_at", dm[1], "frequency", "DailyAt")
		return
	}
	if em := cvEveryNMinRe.FindStringSubmatch(chain); em != nil {
		if n, err := strconv.Atoi(em[1]); err == nil {
			setProps(ent, "schedule_type", "interval",
				"interval_seconds", strconv.Itoa(n*60), "frequency", "Every"+em[1]+"Minutes")
			return
		}
	}
	if sm := cvEverySecRe.FindStringSubmatch(chain); sm != nil {
		setProps(ent, "schedule_type", "interval", "interval_seconds", sm[1], "frequency", "EverySeconds")
		return
	}
	if fm := cvFrequencyRe.FindStringSubmatch(chain); fm != nil {
		freq := fm[1]
		st := "interval"
		switch {
		case cvDailyFreq[freq]:
			st = "daily"
		case freq == "Weekly":
			st = "weekly"
		case freq == "Monthly":
			st = "monthly"
		}
		setProps(ent, "schedule_type", st, "frequency", freq)
		if secs := coravelNamedInterval[freq]; secs != "" {
			setProps(ent, "interval_seconds", secs)
		}
	}
}

// coravelNamedInterval gives the literal seconds for the fixed named intervals.
var coravelNamedInterval = map[string]string{
	"EveryMinute":         "60",
	"EveryFiveMinutes":    "300",
	"EveryTenMinutes":     "600",
	"EveryFifteenMinutes": "900",
	"EveryThirtyMinutes":  "1800",
	"EverySecond":         "1",
	"Hourly":              "3600",
}

func (e *coravelExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.csharp_coravel.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "coravel"),
			attribute.String("file_path", file.Path),
		))
	defer span.End()

	if len(file.Content) == 0 || file.Language != "csharp" {
		return nil, nil
	}
	src := string(file.Content)
	// Fast guard: a Coravel surface must mention one of the recognised idioms.
	if !strings.Contains(src, "IInvocable") &&
		!strings.Contains(src, "Schedule") &&
		!strings.Contains(src, "QueueInvocable") &&
		!strings.Contains(src, "QueueAsyncTask") &&
		!strings.Contains(src, "UseScheduler") &&
		!strings.Contains(src, "Mailable") {
		return nil, nil
	}

	var out []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, ent)
	}

	// 1. Consumer: class X : IInvocable.
	for _, m := range cvInvocableImplRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Service", "invocable", file.Path, "csharp", line)
		setProps(&ent,
			"framework", "coravel",
			"pattern_type", "invocable",
			"invocable", name,
			"task_id", "task:coravel:"+name,
			"edge_kind", "CONSUMES",
			"provenance", "INFERRED_FROM_CORAVEL_IINVOCABLE",
		)
		add(ent)
	}

	// 2. Producer: scheduler.Schedule<T>().<frequency>().
	for _, m := range cvScheduleGenericRe.FindAllStringSubmatchIndex(src, -1) {
		inv := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity("Schedule<"+inv+">", "SCOPE.Operation", "schedule", file.Path, "csharp", line)
		setProps(&ent,
			"framework", "coravel",
			"pattern_type", "schedule",
			"invocable", inv,
			"task_id", "task:coravel:"+inv,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_CORAVEL_SCHEDULE",
		)
		parseCoravelSchedule(&ent, coravelChain(src, m[1]))
		add(ent)
	}

	// 3. Producer: scheduler.Schedule(() => ...).<frequency>() — anonymous work.
	for _, m := range cvScheduleAnonRe.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		ent := makeEntity("Schedule@line"+itoa(line), "SCOPE.Operation", "schedule", file.Path, "csharp", line)
		setProps(&ent,
			"framework", "coravel",
			"pattern_type", "schedule",
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_CORAVEL_SCHEDULE_ANON",
		)
		parseCoravelSchedule(&ent, coravelChain(src, m[0]))
		add(ent)
	}

	// 4. Producer: IQueue.QueueInvocable<T>().
	for _, m := range cvQueueInvocableRe.FindAllStringSubmatchIndex(src, -1) {
		inv := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity("QueueInvocable<"+inv+">", "SCOPE.Operation", "queue", file.Path, "csharp", line)
		setProps(&ent,
			"framework", "coravel",
			"pattern_type", "queue",
			"invocable", inv,
			"task_id", "task:coravel:"+inv,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_CORAVEL_QUEUE",
		)
		add(ent)
	}

	// 5. Producer: IQueue.QueueAsyncTask(...) — anonymous queued work.
	for _, m := range cvQueueTaskRe.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		ent := makeEntity("QueueTask@line"+itoa(line), "SCOPE.Operation", "queue", file.Path, "csharp", line)
		setProps(&ent,
			"framework", "coravel",
			"pattern_type", "queue",
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_CORAVEL_QUEUE_TASK",
		)
		add(ent)
	}

	// 6. Producer: IMailer.Send(new XMailable(...)).
	for _, m := range cvMailSendRe.FindAllStringSubmatchIndex(src, -1) {
		mailable := src[m[2]:m[3]]
		// Skip obvious non-mailable Send(new ...) calls — require a "Mailable"
		// suffix so this stays a Coravel mailing surface, not any Send(new T()).
		if !strings.HasSuffix(mailable, "Mailable") {
			continue
		}
		line := lineOf(src, m[0])
		ent := makeEntity("Send<"+mailable+">", "SCOPE.Operation", "mail", file.Path, "csharp", line)
		setProps(&ent,
			"framework", "coravel",
			"pattern_type", "mail",
			"mailable", mailable,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_CORAVEL_MAIL",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
