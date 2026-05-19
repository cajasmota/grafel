package com.example.demo.model;

import javax.persistence.Entity;
import javax.persistence.GeneratedValue;
import javax.persistence.Id;

@Entity
public class User {
    @Id
    @GeneratedValue
    private Long id;
    private String email;
    private String name;

    public Long getId() { return id; }
    public String getEmail() { return email; }
    public String getName() { return name; }
    public void setEmail(String email) { this.email = email; }
    public void setName(String name) { this.name = name; }
}
