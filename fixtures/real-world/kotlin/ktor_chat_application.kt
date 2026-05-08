// Source: https://github.com/ktorio/ktor-samples (synthetic based on real Ktor patterns) | License: Apache-2.0

package io.ktor.samples.chat

import io.ktor.server.application.*
import io.ktor.server.engine.*
import io.ktor.server.netty.*
import io.ktor.server.routing.*
import io.ktor.server.websocket.*
import io.ktor.websocket.*
import kotlinx.coroutines.channels.*
import java.time.Duration
import java.util.*
import java.util.concurrent.atomic.*
import java.util.concurrent.ConcurrentHashMap

fun main() {
    embeddedServer(Netty, port = 8080, module = Application::module).start(wait = true)
}

fun Application.module() {
    install(WebSockets) {
        pingPeriod = Duration.ofSeconds(15)
        timeout = Duration.ofSeconds(15)
        maxFrameSize = Long.MAX_VALUE
        masking = false
    }

    routing {
        webSocket("/chat") {
            val thisConnection = Connection(this)
            ChatServer.onConnect(thisConnection)
            try {
                send("You are connected! There are ${ChatServer.connections.count()} users here.")
                for (frame in incoming) {
                    frame as? Frame.Text ?: continue
                    val receivedText = frame.readText()
                    val textWithUsername = "[${thisConnection.name}]: $receivedText"
                    ChatServer.broadcast(textWithUsername)
                }
            } catch (e: Exception) {
                println(e.localizedMessage)
            } finally {
                println("Removing $thisConnection!")
                ChatServer.onDisconnect(thisConnection)
            }
        }
    }
}

class Connection(val session: DefaultWebSocketSession) {
    companion object {
        val lastId = AtomicInteger(0)
    }
    val name = "user${lastId.getAndIncrement()}"
}

object ChatServer {
    val connections: MutableSet<Connection> = Collections.newSetFromMap(ConcurrentHashMap())

    fun onConnect(connection: Connection) {
        connections += connection
    }

    fun onDisconnect(connection: Connection) {
        connections -= connection
    }

    suspend fun broadcast(message: String) {
        connections.forEach {
            it.session.send(message)
        }
    }
}
