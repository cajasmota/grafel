package com.example.api

import org.springframework.beans.factory.annotation.Value
import org.springframework.http.HttpStatus
import org.springframework.http.ResponseEntity
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RestController
import org.slf4j.LoggerFactory

@RestController
class UserController(private val repo: UserRepository) {

    private val log = LoggerFactory.getLogger(UserController::class.java)

    @Value("\${signup.maxName:50}")
    private var maxName: Int = 50

    // create — Spring controller method (Kotlin) with an env-gate
    // (System.getenv), an early-return guard returning
    // ResponseEntity.status(HttpStatus.BAD_REQUEST), a 409 guard inside a try,
    // and a catch that logs then returns a 500 ResponseEntity.
    @PostMapping("/users")
    fun create(@RequestBody dto: UserDto): ResponseEntity<Any> {
        if (System.getenv("SIGNUP_ENABLED") == null) {
            return ResponseEntity.status(503).build()
        }

        if (dto.email == null) {
            return ResponseEntity.status(HttpStatus.BAD_REQUEST).body("email required")
        }

        try {
            if (repo.existsByEmail(dto.email)) {
                return ResponseEntity.status(HttpStatus.CONFLICT).body("email in use")
            }
            val saved = repo.save(dto.toEntity())
            return ResponseEntity.status(HttpStatus.CREATED).body(saved)
        } catch (e: Exception) {
            log.error("create failed", e)
            return ResponseEntity.status(500).body("internal error")
        }
    }
}
