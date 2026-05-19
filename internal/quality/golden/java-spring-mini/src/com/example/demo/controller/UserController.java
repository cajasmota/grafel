package com.example.demo.controller;

import com.example.demo.model.User;
import com.example.demo.service.UserService;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import java.util.List;

@RestController
@RequestMapping("/users")
public class UserController {
    private final UserService service;

    public UserController(UserService service) {
        this.service = service;
    }

    @GetMapping
    public List<User> listUsers() {
        return service.listAll();
    }

    @GetMapping("/{email}")
    public User getByEmail(@PathVariable String email) {
        return service.findByEmail(email).orElse(null);
    }

    @PostMapping
    public User createUser(@RequestBody User payload) {
        return service.create(payload.getEmail(), payload.getName());
    }
}
