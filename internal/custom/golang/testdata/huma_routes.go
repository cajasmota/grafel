// Huma routing fixture (OpenAPI-first). huma.Register binds an Operation
// (carrying Method + Path) to a handler passed as the final argument —
// covering endpoint synthesis + handler attribution. Exercises both the
// http.Method* constant form and the bare string-literal verb form.
package humafixture

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type GetUserInput struct {
	ID string `path:"id"`
}

type GetUserOutput struct {
	Body struct {
		ID string `json:"id"`
	}
}

type CreateUserInput struct {
	Body struct {
		Name string `json:"name"`
	}
}

type CreateUserOutput struct {
	Body struct {
		ID string `json:"id"`
	}
}

func register(api huma.API) {
	huma.Register(api, huma.Operation{
		Method:  http.MethodGet,
		Path:    "/users/{id}",
		Summary: "Get user by id",
	}, getUser)

	huma.Register(api, huma.Operation{
		Method:  "POST",
		Path:    "/users",
		Summary: "Create user",
	}, createUser)

	huma.Register(api, huma.Operation{
		OperationID: "deleteUser",
		Method:      http.MethodDelete,
		Path:        "/users/{id}",
	}, deleteUser)
}

func getUser(ctx context.Context, in *GetUserInput) (*GetUserOutput, error) {
	return &GetUserOutput{}, nil
}

func createUser(ctx context.Context, in *CreateUserInput) (*CreateUserOutput, error) {
	return &CreateUserOutput{}, nil
}

func deleteUser(ctx context.Context, in *GetUserInput) (*struct{}, error) {
	return nil, nil
}
