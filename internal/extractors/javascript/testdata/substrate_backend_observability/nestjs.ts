// NestJS backend-HTTP trace_extraction fixture (#2905).
// NestJS is the JS/TS backend with first-class OpenTelemetry tracing wiring
// (nestjs-otel / @opentelemetry SDK). This fixture starts a span around a
// controller method; the observability extractor attributes a trace signal.
import { Controller, Get } from "@nestjs/common";
import { trace } from "@opentelemetry/api";

@Controller("users")
export class UsersController {
  @Get()
  async findAll() {
    const tracer = trace.getTracer("users");
    return tracer.startActiveSpan("findAll", (span) => {
      span.end();
      return [];
    });
  }
}
