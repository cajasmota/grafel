// Sample Scala service — golden fixture source.
package com.example.api

import scala.collection.mutable
import scala.util.{Try, Success, Failure}

case class User(id: Int, name: String, email: String)
case class CreateUserRequest(name: String, email: String)

class UserService {
  private val users: mutable.ListBuffer[User] = mutable.ListBuffer(
    User(1, "Alice", "alice@example.com")
  )
  private var nextId = 2

  def findAll(): List[User] = users.toList

  def findById(id: Int): Option[User] = users.find(_.id == id)

  def create(request: CreateUserRequest): Try[User] = {
    if (request.name.isEmpty) {
      Failure(new IllegalArgumentException("Name cannot be empty"))
    } else {
      val user = User(nextId, request.name, request.email)
      nextId += 1
      users += user
      Success(user)
    }
  }

  def delete(id: Int): Boolean = {
    val before = users.length
    users.filterInPlace(_.id != id)
    users.length < before
  }
}

object UserService {
  def apply(): UserService = new UserService()
}

trait Repository[T, ID] {
  def findById(id: ID): Option[T]
  def findAll(): List[T]
  def save(entity: T): T
  def delete(id: ID): Boolean
}

class InMemoryUserRepository extends Repository[User, Int] {
  private val store: mutable.Map[Int, User] = mutable.Map.empty

  override def findById(id: Int): Option[User] = store.get(id)
  override def findAll(): List[User] = store.values.toList
  override def save(entity: User): User = { store(entity.id) = entity; entity }
  override def delete(id: Int): Boolean = store.remove(id).isDefined
}
