<?php
// Sample PHP controller — golden fixture source.

namespace App\Controllers;

class UserController
{
    private array $users = [];
    private int $nextId = 1;

    public function __construct()
    {
        $this->users[] = ['id' => $this->nextId++, 'name' => 'Alice', 'email' => 'alice@example.com'];
    }

    public function index(): array
    {
        return ['users' => $this->users];
    }

    public function show(int $id): array
    {
        foreach ($this->users as $user) {
            if ($user['id'] === $id) {
                return $user;
            }
        }
        return ['error' => 'Not found'];
    }

    public function create(string $name, string $email): array
    {
        $user = ['id' => $this->nextId++, 'name' => $name, 'email' => $email];
        $this->users[] = $user;
        return $user;
    }

    public function delete(int $id): bool
    {
        foreach ($this->users as $key => $user) {
            if ($user['id'] === $id) {
                unset($this->users[$key]);
                return true;
            }
        }
        return false;
    }

    private function validateEmail(string $email): bool
    {
        return filter_var($email, FILTER_VALIDATE_EMAIL) !== false;
    }
}

function handleRequest(string $method, string $path, UserController $controller): void
{
    if ($method === 'GET' && $path === '/health') {
        echo json_encode(['status' => 'ok']);
        return;
    }

    if ($method === 'GET' && $path === '/users') {
        echo json_encode($controller->index());
        return;
    }

    if ($method === 'POST' && $path === '/users') {
        $body = json_decode(file_get_contents('php://input'), true);
        echo json_encode($controller->create($body['name'], $body['email']));
        return;
    }
}
