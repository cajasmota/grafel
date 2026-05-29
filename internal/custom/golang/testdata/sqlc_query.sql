-- name: GetAuthor :one
SELECT id, name, bio, created_at FROM authors WHERE id = $1;

-- name: ListBooks :many
SELECT id, author_id, title FROM books ORDER BY title;

-- name: CreateAuthor :execresult
INSERT INTO authors (name, bio) VALUES ($1, $2);

-- name: DeleteAuthor :exec
DELETE FROM authors WHERE id = $1;
