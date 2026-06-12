package csharp_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// MediatR in-process CQRS / mediator coverage (#4922). Value-asserting:
// dispatch (PRODUCES) and handler (CONSUMES) must converge by task_id, and
// message contracts must be recovered as schemas.

const mediatrSrc = `
using MediatR;

public record GetUserQuery(int Id) : IRequest<UserDto>;

public class CreateUserCommand : IRequest
{
    public string Name { get; set; }
}

public record UserCreatedNotification(int Id) : INotification;

public class GetUserHandler : IRequestHandler<GetUserQuery, UserDto>
{
    public Task<UserDto> Handle(GetUserQuery request, CancellationToken ct) => default;
}

public class CreateUserHandler : IRequestHandler<CreateUserCommand>
{
    public Task Handle(CreateUserCommand request, CancellationToken ct) => Task.CompletedTask;
}

public class SendWelcomeEmail : INotificationHandler<UserCreatedNotification>
{
    public Task Handle(UserCreatedNotification n, CancellationToken ct) => Task.CompletedTask;
}

public class LoggingBehavior<TReq, TResp> : IPipelineBehavior<TReq, TResp>
{
    public Task<TResp> Handle(TReq r, RequestHandlerDelegate<TResp> next, CancellationToken ct) => next();
}

public class UsersController
{
    private readonly IMediator _mediator;

    public async Task<UserDto> Get(int id)
    {
        var user = await _mediator.Send(new GetUserQuery(id));
        await _mediator.Send(new CreateUserCommand { Name = "x" });
        await _mediator.Publish(new UserCreatedNotification(id));
        return user;
    }
}
`

func findBy(ents []types.EntityRecord, subtype, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == subtype && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func TestMediatRSendPublishHandlersConverge(t *testing.T) {
	ents := extractFull(t, "custom_csharp_mediatr", fi("Users.cs", "csharp", mediatrSrc))

	// --- Producer: Send(new GetUserQuery) ---
	send := findBy(ents, "request_dispatch", "Send GetUserQuery")
	if send == nil {
		t.Fatal("expected request_dispatch 'Send GetUserQuery'")
	}
	if send.Properties["edge_kind"] != "PRODUCES" {
		t.Errorf("Send edge_kind = %q, want PRODUCES", send.Properties["edge_kind"])
	}
	if send.Properties["task_id"] != "mediatr:request:GetUserQuery" {
		t.Errorf("Send task_id = %q, want mediatr:request:GetUserQuery", send.Properties["task_id"])
	}

	// void-request command dispatch
	if findBy(ents, "request_dispatch", "Send CreateUserCommand") == nil {
		t.Error("expected request_dispatch 'Send CreateUserCommand'")
	}

	// --- Producer: Publish(new UserCreatedNotification) ---
	pub := findBy(ents, "notification_dispatch", "Publish UserCreatedNotification")
	if pub == nil {
		t.Fatal("expected notification_dispatch 'Publish UserCreatedNotification'")
	}
	if pub.Properties["task_id"] != "mediatr:notification:UserCreatedNotification" {
		t.Errorf("Publish task_id = %q", pub.Properties["task_id"])
	}

	// --- Consumer: request handler converges with Send via task_id ---
	rh := findBy(ents, "request_handler", "GetUserHandler")
	if rh == nil {
		t.Fatal("expected request_handler 'GetUserHandler'")
	}
	if rh.Properties["edge_kind"] != "CONSUMES" {
		t.Errorf("handler edge_kind = %q, want CONSUMES", rh.Properties["edge_kind"])
	}
	if rh.Properties["task_id"] != send.Properties["task_id"] {
		t.Errorf("request handler task_id %q != dispatch task_id %q",
			rh.Properties["task_id"], send.Properties["task_id"])
	}
	if rh.Properties["message_type"] != "GetUserQuery" {
		t.Errorf("handler message_type = %q, want GetUserQuery", rh.Properties["message_type"])
	}

	// void-request handler recovered
	if findBy(ents, "request_handler", "CreateUserHandler") == nil {
		t.Error("expected request_handler 'CreateUserHandler' (IRequestHandler<CreateUserCommand>)")
	}

	// --- Consumer: notification handler converges with Publish ---
	nh := findBy(ents, "notification_handler", "SendWelcomeEmail")
	if nh == nil {
		t.Fatal("expected notification_handler 'SendWelcomeEmail'")
	}
	if nh.Properties["task_id"] != pub.Properties["task_id"] {
		t.Errorf("notification handler task_id %q != publish task_id %q",
			nh.Properties["task_id"], pub.Properties["task_id"])
	}

	// --- Pipeline behavior ---
	if findBy(ents, "pipeline_behavior", "LoggingBehavior") == nil {
		t.Error("expected pipeline_behavior 'LoggingBehavior'")
	}
}

func TestMediatRMessageContractsAreSchemas(t *testing.T) {
	ents := extractFull(t, "custom_csharp_mediatr", fi("Users.cs", "csharp", mediatrSrc))

	q := findBy(ents, "request_message", "GetUserQuery")
	if q == nil {
		t.Fatal("expected request_message schema 'GetUserQuery'")
	}
	if q.Kind != "SCOPE.Schema" {
		t.Errorf("request_message kind = %q, want SCOPE.Schema", q.Kind)
	}
	if q.Properties["task_id"] != "mediatr:request:GetUserQuery" {
		t.Errorf("request_message task_id = %q", q.Properties["task_id"])
	}

	if findBy(ents, "request_message", "CreateUserCommand") == nil {
		t.Error("expected request_message 'CreateUserCommand' (IRequest void form)")
	}

	n := findBy(ents, "notification_message", "UserCreatedNotification")
	if n == nil {
		t.Fatal("expected notification_message schema 'UserCreatedNotification'")
	}
	if n.Kind != "SCOPE.Schema" {
		t.Errorf("notification_message kind = %q, want SCOPE.Schema", n.Kind)
	}

	// Negative: a handler class must NOT be mis-claimed as a message contract.
	if findBy(ents, "request_message", "GetUserHandler") != nil {
		t.Error("GetUserHandler (IRequestHandler) must not be a request_message")
	}
	if findBy(ents, "notification_message", "SendWelcomeEmail") != nil {
		t.Error("SendWelcomeEmail (INotificationHandler) must not be a notification_message")
	}
}
