// Source: https://github.com/spring-projects/spring-data-jpa (synthetic based on real Hibernate + Lombok patterns) | License: Apache-2.0

package com.example.blog.entity;

import jakarta.persistence.*;
import jakarta.validation.constraints.*;
import lombok.*;
import lombok.experimental.SuperBuilder;
import org.hibernate.annotations.CreationTimestamp;
import org.hibernate.annotations.UpdateTimestamp;

import java.time.Instant;
import java.util.*;

@MappedSuperclass
@Getter
@Setter
@SuperBuilder
@NoArgsConstructor
public abstract class BaseEntity {

    @Id
    @GeneratedValue(strategy = GenerationType.UUID)
    @Column(name = "id", updatable = false, nullable = false)
    private UUID id;

    @CreationTimestamp
    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    @UpdateTimestamp
    @Column(name = "updated_at", nullable = false)
    private Instant updatedAt;

    @Version
    @Column(name = "version", nullable = false)
    private Long version;
}

@Entity
@Table(
    name = "posts",
    indexes = {
        @Index(name = "idx_posts_slug", columnList = "slug", unique = true),
        @Index(name = "idx_posts_author_status", columnList = "author_id, status"),
        @Index(name = "idx_posts_published_at", columnList = "published_at DESC"),
    }
)
@Getter
@Setter
@SuperBuilder
@NoArgsConstructor
@ToString(exclude = {"author", "category", "tags", "comments"})
@EqualsAndHashCode(callSuper = false, onlyExplicitlyIncluded = true)
@NamedEntityGraph(
    name = "Post.withDetails",
    attributeNodes = {
        @NamedAttributeNode("author"),
        @NamedAttributeNode("category"),
        @NamedAttributeNode(value = "tags"),
    }
)
public class Post extends BaseEntity {

    public enum Status {
        DRAFT, PUBLISHED, ARCHIVED
    }

    @ManyToOne(fetch = FetchType.LAZY)
    @JoinColumn(name = "author_id", nullable = false, foreignKey = @ForeignKey(name = "fk_posts_author"))
    @NotNull
    private User author;

    @ManyToOne(fetch = FetchType.LAZY)
    @JoinColumn(name = "category_id", foreignKey = @ForeignKey(name = "fk_posts_category"))
    private Category category;

    @NotBlank
    @Size(min = 3, max = 255)
    @Column(name = "title", nullable = false, length = 255)
    @EqualsAndHashCode.Include
    private String title;

    @NotBlank
    @Size(max = 255)
    @Column(name = "slug", nullable = false, length = 255, unique = true)
    private String slug;

    @Size(max = 500)
    @Column(name = "excerpt", columnDefinition = "TEXT")
    private String excerpt;

    @NotBlank
    @Column(name = "body", nullable = false, columnDefinition = "TEXT")
    private String body;

    @Column(name = "cover_image")
    private String coverImage;

    @Enumerated(EnumType.STRING)
    @Column(name = "status", nullable = false, length = 20)
    @Builder.Default
    private Status status = Status.DRAFT;

    @Column(name = "views_count", nullable = false)
    @Builder.Default
    private Long viewsCount = 0L;

    @Column(name = "published_at")
    private Instant publishedAt;

    @ManyToMany(fetch = FetchType.LAZY)
    @JoinTable(
        name = "post_tags",
        joinColumns = @JoinColumn(name = "post_id"),
        inverseJoinColumns = @JoinColumn(name = "tag_id")
    )
    @Builder.Default
    private Set<Tag> tags = new HashSet<>();

    @OneToMany(mappedBy = "post", cascade = CascadeType.ALL, orphanRemoval = true)
    @Builder.Default
    private List<Comment> comments = new ArrayList<>();

    // Business methods
    public void publish() {
        if (this.status == Status.PUBLISHED) {
            throw new IllegalStateException("Post is already published: " + this.id);
        }
        this.status = Status.PUBLISHED;
        this.publishedAt = Instant.now();
    }

    public void addTag(Tag tag) {
        this.tags.add(tag);
    }

    public void removeTag(Tag tag) {
        this.tags.remove(tag);
    }

    public boolean isPublished() {
        return this.status == Status.PUBLISHED;
    }

    public void incrementViews() {
        this.viewsCount++;
    }
}

@Entity
@Table(name = "tags")
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
@EqualsAndHashCode(onlyExplicitlyIncluded = true)
public class Tag {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Integer id;

    @NotBlank
    @Size(max = 50)
    @Column(nullable = false, length = 50)
    @EqualsAndHashCode.Include
    private String name;

    @NotBlank
    @Column(nullable = false, unique = true, length = 100)
    @EqualsAndHashCode.Include
    private String slug;

    @ManyToMany(mappedBy = "tags")
    @Builder.Default
    private Set<Post> posts = new HashSet<>();
}
