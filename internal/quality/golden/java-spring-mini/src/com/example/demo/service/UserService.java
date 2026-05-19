package com.example.demo.service;

import com.example.demo.model.User;
import com.example.demo.repository.UserRepository;
import org.springframework.stereotype.Service;

import java.util.List;
import java.util.Optional;

@Service
public class UserService {
    private final UserRepository repo;

    public UserService(UserRepository repo) {
        this.repo = repo;
    }

    public List<User> listAll() {
        return repo.findAll();
    }

    public Optional<User> findByEmail(String email) {
        return repo.findByEmail(email);
    }

    public User create(String email, String name) {
        User u = new User();
        u.setEmail(email);
        u.setName(name);
        return repo.save(u);
    }
}
