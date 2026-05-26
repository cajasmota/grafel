// Sample Kotlin Spring Boot controller — golden fixture source.
package com.example.api

import org.springframework.web.bind.annotation.*
import org.springframework.http.ResponseEntity
import org.springframework.http.HttpStatus

data class User(val id: Int, val name: String, val email: String)
data class CreateUserRequest(val name: String, val email: String)

@RestController
@RequestMapping("/api/users")
class UserController {

    private val users = mutableListOf(User(1, "Alice", "alice@example.com"))
    private var nextId = 2

    @GetMapping("/health")
    fun health(): Map<String, String> = mapOf("status" to "ok")

    @GetMapping
    fun listUsers(): List<User> = users

    @GetMapping("/{id}")
    fun getUser(@PathVariable id: Int): ResponseEntity<User> {
        val user = users.find { it.id == id }
            ?: return ResponseEntity.notFound().build()
        return ResponseEntity.ok(user)
    }

    @PostMapping
    fun createUser(@RequestBody request: CreateUserRequest): ResponseEntity<User> {
        val user = User(nextId++, request.name, request.email)
        users.add(user)
        return ResponseEntity.status(HttpStatus.CREATED).body(user)
    }

    @DeleteMapping("/{id}")
    fun deleteUser(@PathVariable id: Int): ResponseEntity<Void> {
        val removed = users.removeIf { it.id == id }
        return if (removed) ResponseEntity.noContent().build()
        else ResponseEntity.notFound().build()
    }
}
