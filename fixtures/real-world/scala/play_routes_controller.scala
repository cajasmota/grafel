// Source: https://github.com/playframework/play-samples (synthetic based on real Play Framework patterns) | License: Apache-2.0

package controllers

import javax.inject._
import play.api._
import play.api.data._
import play.api.data.Forms._
import play.api.mvc._
import play.api.libs.json._
import scala.concurrent.{ExecutionContext, Future}
import models._
import services._

case class TaskForm(title: String, description: Option[String], dueDate: Option[String])

@Singleton
class TaskController @Inject()(
  val controllerComponents: ControllerComponents,
  taskService: TaskService,
  cc: MessagesControllerComponents
)(implicit ec: ExecutionContext) extends MessagesAbstractController(cc) {

  private val logger = Logger(getClass)

  val taskForm: Form[TaskForm] = Form(
    mapping(
      "title" -> nonEmptyText(minLength = 1, maxLength = 200),
      "description" -> optional(text(maxLength = 1000)),
      "dueDate" -> optional(text)
    )(TaskForm.apply)(TaskForm.unapply)
  )

  def index: Action[AnyContent] = Action.async { implicit request =>
    taskService.listAll().map { tasks =>
      Ok(views.html.tasks.index(tasks, taskForm))
    }
  }

  def create: Action[AnyContent] = Action.async { implicit request =>
    taskForm.bindFromRequest().fold(
      formWithErrors => {
        logger.warn(s"Form validation failed: ${formWithErrors.errors}")
        taskService.listAll().map { tasks =>
          BadRequest(views.html.tasks.index(tasks, formWithErrors))
        }
      },
      task => {
        taskService.create(task.title, task.description, task.dueDate).map { _ =>
          Redirect(routes.TaskController.index())
            .flashing("success" -> "Task created successfully.")
        }
      }
    )
  }

  def show(id: Long): Action[AnyContent] = Action.async { implicit request =>
    taskService.findById(id).map {
      case Some(task) => Ok(views.html.tasks.show(task))
      case None       => NotFound(views.html.errors.notFound())
    }
  }

  def update(id: Long): Action[AnyContent] = Action.async { implicit request =>
    taskForm.bindFromRequest().fold(
      formWithErrors => Future.successful(BadRequest(views.html.tasks.edit(id, formWithErrors))),
      task => {
        taskService.update(id, task.title, task.description).map {
          case 0 => NotFound
          case _ => Redirect(routes.TaskController.show(id))
            .flashing("success" -> "Task updated.")
        }
      }
    )
  }

  def delete(id: Long): Action[AnyContent] = Action.async { implicit request =>
    taskService.delete(id).map { _ =>
      Redirect(routes.TaskController.index())
        .flashing("success" -> "Task deleted.")
    }
  }

  def apiList: Action[AnyContent] = Action.async {
    taskService.listAll().map { tasks =>
      Ok(Json.toJson(tasks))
    }
  }

  def apiCreate: Action[JsValue] = Action.async(parse.json) { request =>
    request.body.validate[TaskForm] match {
      case JsSuccess(form, _) =>
        taskService.create(form.title, form.description, form.dueDate).map { task =>
          Created(Json.toJson(task))
        }
      case JsError(errors) =>
        Future.successful(BadRequest(Json.obj("errors" -> JsError.toJson(errors))))
    }
  }
}

// Model
case class Task(
  id: Long,
  title: String,
  description: Option[String],
  dueDate: Option[String],
  completed: Boolean,
  createdAt: java.time.LocalDateTime
)

object Task {
  implicit val taskFormat: OFormat[Task] = Json.format[Task]
}

// Form JSON reads
object TaskForm {
  implicit val reads: Reads[TaskForm] = (
    (JsPath \ "title").read[String] and
    (JsPath \ "description").readNullable[String] and
    (JsPath \ "dueDate").readNullable[String]
  )(TaskForm.apply _)
}
