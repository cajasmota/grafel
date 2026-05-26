package com.example.ktor

import io.ktor.server.application.*
import io.ktor.server.engine.*
import io.ktor.server.netty.*
import io.ktor.server.routing.*
import io.ktor.server.request.*
import io.ktor.server.response.*
import io.ktor.http.*
import com.rabbitmq.client.*
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.jetbrains.exposed.sql.*
import org.jetbrains.exposed.sql.transactions.transaction

/**
 * Kotlin Ktor + RabbitMQ fixture.
 * Demonstrates: HTTP endpoints, RabbitMQ producer, RabbitMQ consumer, DB access via Exposed.
 */

@Serializable
data class Product(val id: Int = 0, val name: String, val price: Double)

object Products : Table("products") {
    val id = integer("id").autoIncrement()
    val name = varchar("name", 255)
    val price = double("price")
    override val primaryKey = PrimaryKey(id)
}

class RabbitPublisher(connectionFactory: ConnectionFactory) {
    private val connection = connectionFactory.newConnection()
    private val channel = connection.createChannel()

    init {
        channel.queueDeclare("product.events", true, false, false, null)
    }

    fun publish(event: String) {
        channel.basicPublish("", "product.events", null, event.toByteArray())
    }

    fun close() {
        channel.close()
        connection.close()
    }
}

class RabbitConsumer(connectionFactory: ConnectionFactory) {
    private val connection = connectionFactory.newConnection()
    private val channel = connection.createChannel()

    fun start() {
        channel.queueDeclare("product.events", true, false, false, null)
        val deliverCallback = DeliverCallback { _, delivery ->
            val message = String(delivery.body)
            println("Received product event: $message")
        }
        channel.basicConsume("product.events", true, deliverCallback) { _ -> }
    }
}

fun Application.productRoutes(publisher: RabbitPublisher) {
    routing {
        get("/api/products") {
            val products = transaction {
                Products.selectAll().map {
                    Product(it[Products.id], it[Products.name], it[Products.price])
                }
            }
            call.respond(products)
        }

        get("/api/products/{id}") {
            val id = call.parameters["id"]?.toIntOrNull()
                ?: return@get call.respond(HttpStatusCode.BadRequest)
            val product = transaction {
                Products.select { Products.id eq id }.firstOrNull()?.let {
                    Product(it[Products.id], it[Products.name], it[Products.price])
                }
            }
            if (product != null) call.respond(product)
            else call.respond(HttpStatusCode.NotFound)
        }

        post("/api/products") {
            val p = call.receive<Product>()
            val newId = transaction {
                Products.insertAndGetId {
                    it[name] = p.name
                    it[price] = p.price
                }.value
            }
            val saved = p.copy(id = newId)
            publisher.publish(Json.encodeToString(Product.serializer(), saved))
            call.respond(HttpStatusCode.Created, saved)
        }

        delete("/api/products/{id}") {
            val id = call.parameters["id"]?.toIntOrNull()
                ?: return@delete call.respond(HttpStatusCode.BadRequest)
            val deleted = transaction { Products.deleteWhere { Products.id eq id } }
            if (deleted > 0) call.respond(HttpStatusCode.NoContent)
            else call.respond(HttpStatusCode.NotFound)
        }
    }
}

fun main() {
    val factory = ConnectionFactory().apply { host = "localhost" }
    val publisher = RabbitPublisher(factory)
    val consumer = RabbitConsumer(factory)

    Database.connect("jdbc:h2:mem:test;DB_CLOSE_DELAY=-1", driver = "org.h2.Driver")
    transaction { SchemaUtils.create(Products) }

    consumer.start()

    embeddedServer(Netty, port = 8080) {
        productRoutes(publisher)
    }.start(wait = true)
}
