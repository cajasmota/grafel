// Source: https://github.com/grails/grails-core (synthetic based on real Grails controller patterns) | License: Apache-2.0

package myapp

import grails.rest.RestfulController
import grails.validation.ValidationException
import org.springframework.http.HttpStatus

class PostController extends RestfulController<Post> {

    static responseFormats = ['json', 'xml']
    static allowedMethods = [save: "POST", update: "PUT", patch: "PATCH", delete: "DELETE"]

    PostService postService

    PostController() {
        super(Post)
    }

    @Override
    def index(Integer max) {
        params.max = Math.min(max ?: 10, 100)
        def currentUser = springSecurityService.currentUser

        respond postService.list(params, currentUser),
                model: [postCount: postService.count()]
    }

    @Override
    def show() {
        respond findResource(params.id)
    }

    @Override
    def create() {
        respond new Post(params)
    }

    @Override
    def save() {
        if (request.method == 'GET') {
            forward action: 'create'
            return
        }

        def post = new Post()
        bindData(post, request)
        post.author = springSecurityService.currentUser

        try {
            postService.save(post)
        } catch (ValidationException e) {
            respond post.errors, view: 'create'
            return
        }

        respond post, [status: HttpStatus.CREATED, view: 'show']
    }

    @Override
    def update() {
        def post = findResource(params.id)
        if (post == null) {
            transactionStatus.setRollbackOnly()
            render status: HttpStatus.NOT_FOUND
            return
        }

        bindData(post, request)

        try {
            postService.save(post)
        } catch (ValidationException e) {
            respond post.errors, view: 'edit'
            return
        }

        respond post, [status: HttpStatus.OK, view: 'show']
    }

    @Override
    def delete() {
        def post = findResource(params.id)
        if (post == null) {
            transactionStatus.setRollbackOnly()
            render status: HttpStatus.NOT_FOUND
            return
        }

        postService.delete(post.id)

        render status: HttpStatus.NO_CONTENT
    }
}

class Post {
    String title
    String body
    String slug
    PostStatus status = PostStatus.DRAFT
    Date dateCreated
    Date lastUpdated

    static belongsTo = [author: User]
    static hasMany = [tags: Tag, comments: Comment]

    static constraints = {
        title blank: false, maxSize: 255
        body blank: false
        slug blank: false, unique: true, maxSize: 255
        status inList: PostStatus.values().toList()
    }

    static mapping = {
        body type: 'text'
        sort dateCreated: 'desc'
        tags joinTable: [name: 'post_tags', key: 'post_id', column: 'tag_id']
    }

    String toString() { title }
}

enum PostStatus {
    DRAFT, PUBLISHED, ARCHIVED
}
