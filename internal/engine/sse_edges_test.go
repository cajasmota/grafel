package engine

import (
	"testing"
)

// ---------------------------------------------------------------------------
// SSE — Node/Express server: text/event-stream
// ---------------------------------------------------------------------------

func TestSSE_NodeWriteHead_EmitsStreamAndStreamsTo(t *testing.T) {
	src := `function sse(req, res) {
  res.writeHead(200, { 'Content-Type': 'text/event-stream' });
  res.write('data: hello\n\n');
}
app.get('/api/events', sse);
`
	res := runDetectWS(t, "javascript", "sse.js", src)
	streams := filterEntities(res.Entities, streamKind)
	if len(streams) == 0 {
		t.Fatalf("expected ≥1 Stream entity; got %v", res.Entities)
	}
	to := filterRels(res.Relationships, "STREAMS_TO")
	if len(to) == 0 {
		t.Errorf("expected STREAMS_TO edge; got %v", res.Relationships)
	}
}

func TestSSE_NodeSetHeader_EmitsStream(t *testing.T) {
	src := `function tail(req, res) {
  res.setHeader('Content-Type', 'text/event-stream');
  res.write('data: tick\n\n');
}
`
	res := runDetectWS(t, "typescript", "tail.ts", src)
	if len(filterEntities(res.Entities, streamKind)) == 0 {
		t.Errorf("expected Stream entity for setHeader variant; got %v", res.Entities)
	}
}

// ---------------------------------------------------------------------------
// SSE — Browser client: new EventSource
// ---------------------------------------------------------------------------

func TestSSE_EventSourceClient_EmitsStreamsFrom(t *testing.T) {
	src := `function subscribe() {
  const es = new EventSource("/api/notifications");
  es.onmessage = (e) => {};
}
`
	res := runDetectWS(t, "typescript", "client.ts", src)
	streams := filterEntities(res.Entities, streamKind)
	if len(streams) == 0 || streams[0].ID != "sse:/api/notifications" {
		t.Fatalf("expected sse:/api/notifications; got %v", streams)
	}
	from := filterRels(res.Relationships, "STREAMS_FROM")
	if len(from) == 0 {
		t.Errorf("expected STREAMS_FROM edge")
	}
}

// Cross-stack: Django streaming response + browser EventSource share path identity.
func TestSSE_CrossStack_MatchByPath(t *testing.T) {
	server := `def stream(request):
    return StreamingHttpResponse(gen(), content_type="text/event-stream")
`
	client := `function go() { new EventSource("/stream"); }`
	srvRes := runDetectWS(t, "python", "views.py", server)
	clRes := runDetectWS(t, "javascript", "ui.js", client)

	srvStreams := filterEntities(srvRes.Entities, streamKind)
	clStreams := filterEntities(clRes.Entities, streamKind)
	if len(srvStreams) == 0 || len(clStreams) == 0 {
		t.Fatalf("missing streams: server=%v client=%v", srvStreams, clStreams)
	}
	// Server stream is keyed by enclosing fn name "stream"; client by raw path "/stream".
	// Both must canonicalise to sse:/stream so the cross-repo linker matches them.
	if srvStreams[0].ID != "sse:/stream" {
		t.Errorf("server stream ID = %q, want sse:/stream", srvStreams[0].ID)
	}
	if clStreams[0].ID != "sse:/stream" {
		t.Errorf("client stream ID = %q, want sse:/stream", clStreams[0].ID)
	}
}

// ---------------------------------------------------------------------------
// SSE — FastAPI StreamingResponse
// ---------------------------------------------------------------------------

func TestSSE_FastAPIStreamingResponse(t *testing.T) {
	src := `from fastapi.responses import StreamingResponse

def event_stream():
    yield "data: hi\n\n"

@app.get("/events")
def events():
    return StreamingResponse(event_stream(), media_type="text/event-stream")
`
	res := runDetectWS(t, "python", "main.py", src)
	if len(filterEntities(res.Entities, streamKind)) == 0 {
		t.Errorf("expected FastAPI SSE Stream entity")
	}
}

// ---------------------------------------------------------------------------
// SSE — Spring SseEmitter
// ---------------------------------------------------------------------------

func TestSSE_SpringSseEmitter(t *testing.T) {
	src := `public class Ctrl {
    @GetMapping("/notify")
    public SseEmitter notify() {
        return new SseEmitter();
    }
}`
	res := runDetectWS(t, "java", "Ctrl.java", src)
	if len(filterEntities(res.Entities, streamKind)) == 0 {
		t.Errorf("expected SseEmitter Stream entity")
	}
}

// ---------------------------------------------------------------------------
// SSE — non-SSE file produces NO Stream entities (parity)
// ---------------------------------------------------------------------------

func TestSSE_NonSSEFile_NoStreams(t *testing.T) {
	src := `function plain(req, res) {
  res.writeHead(200, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ ok: true }));
}`
	res := runDetectWS(t, "javascript", "plain.js", src)
	if ss := filterEntities(res.Entities, streamKind); len(ss) > 0 {
		t.Errorf("non-SSE file produced Stream entities: %v", ss)
	}
}
