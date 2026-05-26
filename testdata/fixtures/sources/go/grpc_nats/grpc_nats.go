// Go gRPC + NATS fixture.
// Demonstrates: gRPC endpoint (HTTP/2), NATS publisher, NATS subscriber, DB access via sql.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"

	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// --- Domain types ---

type Task struct {
	ID    int64
	Title string
	Done  bool
}

// --- NATS publisher ---

type TaskPublisher struct {
	nc *nats.Conn
}

func NewTaskPublisher(url string) (*TaskPublisher, error) {
	nc, err := nats.Connect(url)
	if err != nil {
		return nil, err
	}
	return &TaskPublisher{nc: nc}, nil
}

func (p *TaskPublisher) PublishCreated(t Task) error {
	msg := fmt.Sprintf(`{"id":%d,"title":"%s"}`, t.ID, t.Title)
	return p.nc.Publish("tasks.created", []byte(msg))
}

func (p *TaskPublisher) PublishCompleted(id int64) error {
	return p.nc.Publish("tasks.completed", []byte(fmt.Sprintf(`{"id":%d}`, id)))
}

// --- NATS subscriber ---

type TaskSubscriber struct {
	nc  *nats.Conn
	sub *nats.Subscription
}

func NewTaskSubscriber(nc *nats.Conn) (*TaskSubscriber, error) {
	ts := &TaskSubscriber{nc: nc}
	sub, err := nc.Subscribe("tasks.*", ts.handleMessage)
	if err != nil {
		return nil, err
	}
	ts.sub = sub
	return ts, nil
}

func (ts *TaskSubscriber) handleMessage(msg *nats.Msg) {
	log.Printf("Received on %s: %s", msg.Subject, string(msg.Data))
}

// --- gRPC service ---

type TaskServiceServer struct {
	db        *sql.DB
	publisher *TaskPublisher
}

// GetTask implements TaskService.GetTask gRPC method.
func (s *TaskServiceServer) GetTask(ctx context.Context, req *GetTaskRequest) (*TaskResponse, error) {
	var t Task
	err := s.db.QueryRowContext(ctx, "SELECT id, title, done FROM tasks WHERE id=$1", req.Id).
		Scan(&t.ID, &t.Title, &t.Done)
	if err == sql.ErrNoRows {
		return nil, status.Errorf(codes.NotFound, "task %d not found", req.Id)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "db error: %v", err)
	}
	return &TaskResponse{Id: t.ID, Title: t.Title, Done: t.Done}, nil
}

// ListTasks implements TaskService.ListTasks gRPC method.
func (s *TaskServiceServer) ListTasks(ctx context.Context, req *ListTasksRequest) (*ListTasksResponse, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, title, done FROM tasks ORDER BY id")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "db error: %v", err)
	}
	defer rows.Close()
	var tasks []*TaskResponse
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Title, &t.Done); err != nil {
			continue
		}
		tasks = append(tasks, &TaskResponse{Id: t.ID, Title: t.Title, Done: t.Done})
	}
	return &ListTasksResponse{Tasks: tasks}, nil
}

// CreateTask implements TaskService.CreateTask gRPC method.
func (s *TaskServiceServer) CreateTask(ctx context.Context, req *CreateTaskRequest) (*TaskResponse, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		"INSERT INTO tasks (title, done) VALUES ($1, false) RETURNING id",
		req.Title,
	).Scan(&id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "db error: %v", err)
	}
	t := Task{ID: id, Title: req.Title}
	if err := s.publisher.PublishCreated(t); err != nil {
		log.Printf("NATS publish error: %v", err)
	}
	return &TaskResponse{Id: id, Title: req.Title, Done: false}, nil
}

// CompleteTask implements TaskService.CompleteTask gRPC method.
func (s *TaskServiceServer) CompleteTask(ctx context.Context, req *CompleteTaskRequest) (*TaskResponse, error) {
	result, err := s.db.ExecContext(ctx,
		"UPDATE tasks SET done=true WHERE id=$1", req.Id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "db error: %v", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return nil, status.Errorf(codes.NotFound, "task %d not found", req.Id)
	}
	s.publisher.PublishCompleted(req.Id)
	return &TaskResponse{Id: req.Id, Done: true}, nil
}

// --- Stub proto types (normally generated) ---

type GetTaskRequest struct{ Id int64 }
type ListTasksRequest struct{}
type ListTasksResponse struct{ Tasks []*TaskResponse }
type CreateTaskRequest struct{ Title string }
type CompleteTaskRequest struct{ Id int64 }
type TaskResponse struct {
	Id    int64
	Title string
	Done  bool
}

// --- main ---

func main() {
	db, err := sql.Open("postgres", "postgres://localhost/tasks_db?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	nc, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		log.Fatal(err)
	}
	defer nc.Close()

	publisher := &TaskPublisher{nc: nc}
	_, err = NewTaskSubscriber(nc)
	if err != nil {
		log.Fatal(err)
	}

	svc := &TaskServiceServer{db: db, publisher: publisher}

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	_ = svc // would register: pb.RegisterTaskServiceServer(grpcServer, svc)
	log.Printf("gRPC server listening on :50051")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
