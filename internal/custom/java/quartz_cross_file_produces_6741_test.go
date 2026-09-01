package java

import "testing"

// #6741 arm 2. The Quartz Java PRODUCES cross-edge loop only ever paired a
// job_builder producer with a `job_class` consumer found in the SAME
// PatternResult — i.e. the same file. Real Quartz projects (and the
// java-quartz-mini golden fixture) put the scheduler in one file and the Job
// implementations in another, so the pairing was structurally impossible and
// the repo's only real PRODUCES emitter never fired.
//
// The fix follows the established Java cross-file convention: emit the edge at
// the name stub `Class:<Name>` (javaClassRef, orm_helpers.go), which the
// resolver's byName fallback binds to the separately-extracted class entity in
// whatever file declares it — the same mechanism the JPA/Hibernate
// field-target REFERENCES edges use (#4367).

const quartzProducerOnlySrc = `
import org.quartz.*;
import static org.quartz.JobBuilder.newJob;

public class AppScheduler {
    public void start() throws Exception {
        Scheduler scheduler = StdSchedulerFactory.getDefaultScheduler();
        JobDetail emailJob = JobBuilder.newJob(SendEmailJob.class)
                .withIdentity("email-job", "email-group")
                .build();
        JobDetail reportJob = newJob(GenerateReportJob.class)
                .withIdentity("report-job", "report-group")
                .build();
        scheduler.scheduleJob(emailJob, null);
    }
}
`

const quartzSameFileSrc = `
import org.quartz.*;

public class SendEmailJob implements Job {
    public void execute(JobExecutionContext context) throws JobExecutionException {
    }
}

public class Scheduler2 {
    public void start() throws Exception {
        JobDetail emailJob = JobBuilder.newJob(SendEmailJob.class).build();
    }
}
`

func producesEdges(res PatternResult) []Relationship {
	var out []Relationship
	for _, r := range res.Relationships {
		if r.RelationshipType == "PRODUCES" {
			out = append(out, r)
		}
	}
	return out
}

func producerRefFor(t *testing.T, res PatternResult, jobClass string) string {
	t.Helper()
	for _, e := range res.Entities {
		if e.Subtype != "job_builder" {
			continue
		}
		if jc, _ := e.Properties["job_class"].(string); jc == jobClass {
			return e.Ref
		}
	}
	t.Fatalf("no job_builder entity for job class %q", jobClass)
	return ""
}

// A producer file with no Job implementation of its own must still emit a
// PRODUCES edge, aimed at the resolvable cross-file class stub.
func TestQuartzJavaProducesReachesJobClassInAnotherFile(t *testing.T) {
	res := ExtractQuartzJava(PatternContext{
		Source:   quartzProducerOnlySrc,
		Language: "java",
		FilePath: "src/AppScheduler.java",
	})

	got := producesEdges(res)
	if len(got) != 2 {
		t.Fatalf("want 2 PRODUCES edges (one per job_builder), got %d: %+v", len(got), got)
	}

	// Each producer must point at ITS OWN job class, not at whichever class
	// happened to be seen first. A pairing that matched any job class would
	// satisfy a bare count assertion while inventing false edges.
	wantTarget := map[string]string{
		producerRefFor(t, res, "SendEmailJob"):      javaClassRef("SendEmailJob"),
		producerRefFor(t, res, "GenerateReportJob"): javaClassRef("GenerateReportJob"),
	}
	seen := map[string]bool{}
	for _, r := range got {
		want, ok := wantTarget[r.SourceRef]
		if !ok {
			t.Errorf("PRODUCES from unexpected source %q", r.SourceRef)
			continue
		}
		if r.TargetRef != want {
			t.Errorf("PRODUCES from %q: target = %q, want %q", r.SourceRef, r.TargetRef, want)
		}
		if seen[r.SourceRef] {
			t.Errorf("duplicate PRODUCES from %q", r.SourceRef)
		}
		seen[r.SourceRef] = true
		if r.Properties["task_id"] == "" {
			t.Errorf("PRODUCES from %q carries no task_id", r.SourceRef)
		}
	}
}

// The task_id property must name the job class this producer actually builds —
// it is the `task:<framework>:<id>` address ADR-0028 joins the pair on.
func TestQuartzJavaProducesTaskIDMatchesItsOwnJobClass(t *testing.T) {
	res := ExtractQuartzJava(PatternContext{
		Source:   quartzProducerOnlySrc,
		Language: "java",
		FilePath: "src/AppScheduler.java",
	})
	want := map[string]string{
		producerRefFor(t, res, "SendEmailJob"):      "task:quartz:SendEmailJob",
		producerRefFor(t, res, "GenerateReportJob"): "task:quartz:GenerateReportJob",
	}
	for _, r := range producesEdges(res) {
		if got := r.Properties["task_id"]; got != want[r.SourceRef] {
			t.Errorf("PRODUCES from %q: task_id = %q, want %q", r.SourceRef, got, want[r.SourceRef])
		}
	}
}

// When the Job implementation IS in the same file, the shipped behaviour is
// preserved: the edge lands on the in-file quartz job_class entity, and the
// cross-file stub is not emitted as a second, duplicate edge.
func TestQuartzJavaProducesPrefersSameFileJobClassAndDoesNotDoubleEdge(t *testing.T) {
	res := ExtractQuartzJava(PatternContext{
		Source:   quartzSameFileSrc,
		Language: "java",
		FilePath: "src/All.java",
	})

	got := producesEdges(res)
	if len(got) != 1 {
		t.Fatalf("want exactly 1 PRODUCES edge, got %d: %+v", len(got), got)
	}
	wantTarget := "scope:service:quartz_job:src/All.java:SendEmailJob"
	if got[0].TargetRef != wantTarget {
		t.Errorf("same-file PRODUCES target = %q, want the in-file job_class ref %q",
			got[0].TargetRef, wantTarget)
	}
}

// Scoping guard: only `job_builder` entities mint a PRODUCES edge. The
// @DisallowConcurrentExecution pattern entity also carries a `job_class`
// property, and the trigger / schedule_job producers carry `edge_kind:
// PRODUCES` — so a pairing scoped on either of those instead of the
// `job_builder` subtype would fire in a file that dispatches nothing. This
// consumer-only file must emit no PRODUCES edge at all.
func TestQuartzJavaProducesIsScopedToJobBuilderProducers(t *testing.T) {
	res := ExtractQuartzJava(PatternContext{
		Source: `
import org.quartz.*;

@DisallowConcurrentExecution
public class GenerateReportJob implements Job {
    public void execute(JobExecutionContext context) throws JobExecutionException {
    }
}
`,
		Language: "java",
		FilePath: "src/jobs/GenerateReportJob.java",
	})
	// Precondition: the concurrency-policy entity really does carry a job_class,
	// so this file is a genuine counter-example and not a vacuous one.
	found := false
	for _, e := range res.Entities {
		if e.Subtype == "concurrency_policy" {
			if jc, _ := e.Properties["job_class"].(string); jc == "GenerateReportJob" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("fixture no longer exercises the case: no concurrency_policy entity carrying job_class")
	}
	if got := producesEdges(res); len(got) != 0 {
		t.Fatalf("consumer-only file must emit no PRODUCES, got %d: %+v", len(got), got)
	}
}
