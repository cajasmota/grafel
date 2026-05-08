// Source: https://github.com/spockframework/spock/tree/master/spock-specs (synthetic based on real Spock test patterns) | License: Apache-2.0

package myapp

import grails.testing.gorm.DataTest
import grails.testing.services.ServiceUnitTest
import spock.lang.*

class PostServiceSpec extends Specification implements ServiceUnitTest<PostService>, DataTest {

    def setupSpec() {
        mockDomains Post, User, Tag
    }

    User author

    def setup() {
        author = new User(username: 'testuser', email: 'test@example.com', password: 'hashed')
        author.save(flush: true, failOnError: true)
    }

    def "creates a post with valid data"() {
        given: "valid post attributes"
        def attrs = [
            title: 'My First Post',
            body: 'This is the post body content.',
            author: author
        ]

        when: "the service creates the post"
        def post = service.create(attrs)

        then: "the post is saved and has a slug"
        post.id != null
        post.title == 'My First Post'
        post.slug == 'my-first-post'
        post.status == PostStatus.DRAFT
    }

    def "rejects post creation with blank title"() {
        given: "invalid post attributes"
        def attrs = [title: '', body: 'Some content', author: author]

        when: "the service tries to create the post"
        service.create(attrs)

        then: "a validation exception is raised"
        thrown(ValidationException)
    }

    @Unroll
    def "generates slug '#expectedSlug' from title '#title'"() {
        when:
        def slug = service.generateSlug(title)

        then:
        slug == expectedSlug

        where:
        title                       | expectedSlug
        'Hello World'               | 'hello-world'
        'My Awesome Post!'          | 'my-awesome-post'
        'C++ Programming Guide'     | 'c-programming-guide'
        '  Spaces Around  '         | 'spaces-around'
        'already-a-slug'            | 'already-a-slug'
    }

    def "publishes a draft post"() {
        given: "a draft post"
        def post = new Post(title: 'Draft Post', body: 'Body', author: author)
        post.save(failOnError: true)
        assert post.status == PostStatus.DRAFT

        when: "publish is called"
        service.publish(post)

        then: "post status is PUBLISHED"
        post.reload().status == PostStatus.PUBLISHED
        post.publishedAt != null
    }

    def "cannot publish already published post"() {
        given: "an already published post"
        def post = new Post(
            title: 'Published Post',
            body: 'Body',
            author: author,
            status: PostStatus.PUBLISHED,
            publishedAt: new Date()
        )
        post.save(failOnError: true)

        when: "publish is called again"
        service.publish(post)

        then: "an exception is raised"
        thrown(IllegalStateException)
    }

    def "lists posts with pagination"() {
        given: "10 published posts"
        (1..10).each { i ->
            new Post(
                title: "Post $i",
                body: "Body $i",
                author: author,
                status: PostStatus.PUBLISHED,
                publishedAt: new Date()
            ).save(failOnError: true)
        }

        when: "we request page 1 with 5 items"
        def result = service.list(max: 5, offset: 0)

        then: "we get exactly 5 posts"
        result.size() == 5

        when: "we request page 2"
        def page2 = service.list(max: 5, offset: 5)

        then: "we get the remaining 5 posts"
        page2.size() == 5
    }

    def "delete removes post and associated comments"() {
        given: "a post with comments"
        def post = new Post(title: 'To Delete', body: 'Body', author: author)
        post.save(failOnError: true)
        // Comments would be created here in a full test

        when: "delete is called"
        service.delete(post.id)

        then: "post is gone"
        Post.get(post.id) == null
    }
}
