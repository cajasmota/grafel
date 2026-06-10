package com.example.api

import scala.util.{Try, Success, Failure}
import org.slf4j.LoggerFactory

// createUser — representative Scala service action with an env-gate
// (sys.env.get("SIGNUP_ENABLED")), two early-return guards yielding Either Left
// error values carrying named statuses (ServiceUnavailable 503 / BadRequest 400),
// a 409 Conflict guard inside the try, and a try/catch that logs then re-throws.
class UserService(repo: UserRepository) {

  private val logger = LoggerFactory.getLogger(classOf[UserService])

  def createUser(req: CreateUserRequest): Either[ApiError, User] = {
    if (sys.env.get("SIGNUP_ENABLED").isEmpty) {
      return Left(ServiceUnavailable("signup disabled"))
    }

    if (req.email == null) {
      return Left(BadRequest("email is required"))
    }

    try {
      val existing = repo.findByEmail(req.email)
      if (existing.isDefined) {
        return Left(Conflict("email already in use"))
      }
      val user = repo.create(req)
      Right(user)
    } catch {
      case e: Exception =>
        logger.error("createUser failed", e)
        throw new ServiceException("create failed", 500)
    }
  }
}
