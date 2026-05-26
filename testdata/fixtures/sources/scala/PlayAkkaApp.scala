package com.example.play

import akka.actor.typed.ActorSystem
import akka.actor.typed.scaladsl.Behaviors
import akka.actor.typed.ActorRef
import akka.actor.typed.Behavior
import play.api.mvc._
import play.api.libs.json._
import play.api.db.slick.DatabaseConfigProvider
import slick.jdbc.PostgresProfile.api._
import javax.inject._
import scala.concurrent.{ExecutionContext, Future}

/**
 * Scala Play + Akka fixture.
 * Demonstrates: HTTP endpoints, Akka actor (message producer), Akka actor (message consumer), DB via Slick.
 */

// ── Domain ───────────────────────────────────────────────────────────────────

case class Report(id: Long = 0, title: String, content: String, status: String = "pending")

object Report {
  implicit val format: OFormat[Report] = Json.format[Report]
}

// ── Akka Actors ──────────────────────────────────────────────────────────────

object ReportProcessor {
  sealed trait Command
  case class ProcessReport(id: Long, title: String, replyTo: ActorRef[Result]) extends Command
  case class Result(success: Boolean, message: String)

  def apply(): Behavior[Command] = Behaviors.receive { (ctx, msg) =>
    msg match {
      case ProcessReport(id, title, replyTo) =>
        ctx.log.info(s"Processing report $id: $title")
        replyTo ! Result(success = true, message = s"Report $id processed")
        Behaviors.same
    }
  }
}

object NotificationActor {
  sealed trait Command
  case class Notify(reportId: Long, status: String) extends Command

  def apply(): Behavior[Command] = Behaviors.receive { (_, msg) =>
    msg match {
      case Notify(id, st) =>
        println(s"Report $id changed to status: $st")
        Behaviors.same
    }
  }
}

// ── Slick table mapping ───────────────────────────────────────────────────────

class ReportsTable(tag: Tag) extends Table[Report](tag, "reports") {
  def id      = column[Long]("id", O.PrimaryKey, O.AutoInc)
  def title   = column[String]("title")
  def content = column[String]("content")
  def status  = column[String]("status")
  def * = (id, title, content, status).mapTo[Report]
}

// ── Controller ────────────────────────────────────────────────────────────────

@Singleton
class ReportsController @Inject() (
    cc: ControllerComponents,
    dbConfigProvider: DatabaseConfigProvider,
    system: ActorSystem[_]
)(implicit ec: ExecutionContext) extends AbstractController(cc) {

  private val db      = dbConfigProvider.get.db
  private val reports = TableQuery[ReportsTable]
  private val processor   = system.systemActorOf(ReportProcessor(), "report-processor")
  private val notifier    = system.systemActorOf(NotificationActor(), "notifier")

  def list(): Action[AnyContent] = Action.async {
    db.run(reports.result).map(rs => Ok(Json.toJson(rs)))
  }

  def get(id: Long): Action[AnyContent] = Action.async {
    db.run(reports.filter(_.id === id).result.headOption).map {
      case Some(r) => Ok(Json.toJson(r))
      case None    => NotFound
    }
  }

  def create(): Action[JsValue] = Action.async(parse.json) { request =>
    request.body.validate[Report].fold(
      errors => Future.successful(BadRequest(JsError.toJson(errors))),
      report =>
        db.run((reports returning reports.map(_.id)) += report).map { newId =>
          val saved = report.copy(id = newId)
          // Send to Akka actor for async processing
          import akka.actor.typed.scaladsl.AskPattern._
          import scala.concurrent.duration._
          implicit val timeout: akka.util.Timeout = 3.seconds
          implicit val scheduler = system.scheduler
          processor.ask(ReportProcessor.ProcessReport(newId, report.title, _))
          Created(Json.toJson(saved))
        }
    )
  }

  def delete(id: Long): Action[AnyContent] = Action.async {
    db.run(reports.filter(_.id === id).delete).map {
      case 0 => NotFound
      case _ =>
        notifier ! NotificationActor.Notify(id, "deleted")
        NoContent
    }
  }
}
