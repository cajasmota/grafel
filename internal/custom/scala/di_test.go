package scala_test

import (
	"testing"
)

// ---------------------------------------------------------------------------
// DI extractor tests
// ---------------------------------------------------------------------------

func TestDIFinatraGuiceBind(t *testing.T) {
	src := `
import com.twitter.finatra.http._
import com.google.inject._
class AppModule extends TwitterModule {
  bind[UserRepository].to[UserRepositoryImpl]
  bind[OrderService].toInstance(new OrderServiceImpl())
}
`
	ents := extract(t, "custom_scala_di", fi("AppModule.scala", "scala", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.DI" && e.Subtype == "di_binding" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected di_binding from Guice bind[T].to[Impl]")
	}
}

func TestDIFinatraGuiceInject(t *testing.T) {
	src := `
import com.twitter.finatra.http._
import javax.inject.Inject
@Singleton
class UserController @Inject()(userService: UserService) extends HttpController {
  @Get("/users")
  def getUsers(request: Request): Response = ???
}
`
	ents := extract(t, "custom_scala_di", fi("UserController.scala", "scala", src))
	// Should get both a singleton scope and inject point
	foundInject := false
	foundScope := false
	for _, e := range ents {
		if e.Kind == "SCOPE.DI" && e.Subtype == "injection_point" {
			foundInject = true
		}
		if e.Kind == "SCOPE.DI" && e.Subtype == "di_scope" {
			foundScope = true
		}
	}
	if !foundInject {
		t.Error("expected injection_point from @Inject")
	}
	if !foundScope {
		t.Error("expected di_scope from @Singleton")
	}
}

func TestDIZioHttpZLayer(t *testing.T) {
	src := `
import zio._
import zio.http._
val userServiceLayer: ZLayer[Database, Nothing, UserService] =
  ZLayer.succeed(new UserServiceImpl())

val appLayer = ZLayer.make[AppEnv](
  userServiceLayer,
  databaseLayer,
  configLayer
)

val program = for {
  _ <- Server.serve(routes)
} yield ()

def run = program.provide(appLayer)
`
	ents := extract(t, "custom_scala_di", fi("App.scala", "scala", src))
	foundBinding := false
	foundInjection := false
	foundScope := false
	for _, e := range ents {
		if e.Kind == "SCOPE.DI" && e.Subtype == "di_binding" {
			foundBinding = true
		}
		if e.Kind == "SCOPE.DI" && e.Subtype == "injection_point" {
			foundInjection = true
		}
		if e.Kind == "SCOPE.DI" && e.Subtype == "di_scope" {
			foundScope = true
		}
	}
	if !foundBinding {
		t.Error("expected di_binding from ZLayer")
	}
	if !foundInjection {
		t.Error("expected injection_point from .provide()")
	}
	if !foundScope {
		t.Error("expected di_scope from typed ZLayer val")
	}
}

func TestDILagomGuice(t *testing.T) {
	src := `
import com.lightbend.lagom.scaladsl.server._
import com.google.inject.AbstractModule
class UserServiceModule extends AbstractModule {
  bind[UserService].to[UserServiceImpl]
  bind[UserRepository].toInstance(new InMemoryUserRepository())
}
@Singleton
class UserServiceImpl @Inject()(repo: UserRepository) extends UserService {
  def getUser(id: String) = repo.findById(id)
}
`
	ents := extract(t, "custom_scala_di", fi("UserServiceModule.scala", "scala", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.DI" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected DI entities from Lagom/Guice module")
	}
}

func TestDINoMatchNonDIFramework(t *testing.T) {
	// http4s doesn't have explicit DI via our extractor
	src := `
import org.http4s._
val routes = HttpRoutes.of[IO] {
  case GET -> Root / "users" => Ok("users")
}
`
	ents := extract(t, "custom_scala_di", fi("Routes.scala", "scala", src))
	if len(ents) != 0 {
		t.Errorf("expected no DI entities for http4s (not a DI-explicit framework), got %d", len(ents))
	}
}
