package engine

import (
	"testing"
)

func TestIssue652_BasePath(t *testing.T) {
	src := "const BASE_PATH = '/api/v1';\nfetch(`${BASE_PATH}/users`);"
	got, _ := runDetect(t, "typescript", "652-base-path.ts", src)
	found := false
	for _, id := range got {
		if id == "http:GET:/api/v1/users" {
			found = true
		}
	}
	if !found {
		t.Errorf("#652: expected http:GET:/api/v1/users, got: %v", got)
	}
}

func TestIssue654_EnvVarVariable(t *testing.T) {
	src := "const url = `${process.env.API_URL}/users`;\nfetch(url);"
	got, _ := runDetect(t, "typescript", "654-env-var.ts", src)
	t.Logf("#654 got: %v", got)
}
