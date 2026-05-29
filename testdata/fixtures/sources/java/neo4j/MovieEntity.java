package com.example.neo4j.domain;

import org.springframework.data.neo4j.core.schema.Id;
import org.springframework.data.neo4j.core.schema.GeneratedValue;
import org.springframework.data.neo4j.core.schema.Node;
import org.springframework.data.neo4j.core.schema.Property;
import org.springframework.data.neo4j.core.schema.Relationship;
import org.springframework.data.neo4j.core.schema.Relationship.Direction;

import java.util.List;

/**
 * Spring Data Neo4j entity representing a Movie node in the graph database.
 */
@Node("Movie")
public class MovieEntity {

    @Id
    @GeneratedValue
    private Long id;

    @Property("title")
    private String title;

    @Property("released")
    private Integer released;

    @Property("tagline")
    private String tagline;

    @Relationship(type = "ACTED_IN", direction = Direction.INCOMING)
    private List<ActorEntity> actors;

    @Relationship(type = "DIRECTED", direction = Direction.INCOMING)
    private List<DirectorEntity> directors;

    public MovieEntity() {}

    public MovieEntity(String title, Integer released) {
        this.title = title;
        this.released = released;
    }

    public Long getId() { return id; }
    public String getTitle() { return title; }
    public Integer getReleased() { return released; }
    public List<ActorEntity> getActors() { return actors; }
    public List<DirectorEntity> getDirectors() { return directors; }
}
