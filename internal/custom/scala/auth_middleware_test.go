package scala_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Deep AUTH + MIDDLEWARE extraction tests — flagship frameworks.
//
// These assert the SPECIFIC auth method (basic / oauth2 / bearer / jwt) and the
// NAMED middleware / filters / directives + their composition order — not a
// vacuous "≥1 auth/middleware entity exists" check.
// ---------------------------------------------------------------------------

// extractFull returns full EntityRecords (incl. Properties) for prop-level asserts.
func extractFull(t *testing.T, name string, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

// findByName returns the first record with the given subtype and exact name.
func findByName(ents []types.EntityRecord, subtype, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == subtype && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// findByProp returns the first record whose property key==val.
func findByProp(ents []types.EntityRecord, subtype, key, val string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == subtype && ents[i].Properties[key] == val {
			return &ents[i]
		}
	}
	return nil
}

func dumpRecords(ents []types.EntityRecord) string {
	out := ""
	for _, e := range ents {
		out += "\n  {Subtype:" + e.Subtype + " Name:" + e.Name + " props:" + props(e) + "}"
	}
	return out
}

func props(e types.EntityRecord) string {
	s := ""
	for _, k := range []string{"auth_method", "directive", "realm", "authenticator", "middleware_name", "composition_order", "filter_chain", "handler", "action_kind", "action_name"} {
		if v, ok := e.Properties[k]; ok && v != "" {
			s += k + "=" + v + " "
		}
	}
	return s
}

// ---------------------------------------------------------------------------
// http4s AUTH — AuthMiddleware(authUser) + AuthedRoutes.of, with bearer/jwt scheme.
// ---------------------------------------------------------------------------

func TestDeepAuthHttp4sAuthMiddleware(t *testing.T) {
	src := `
import org.http4s._
import org.http4s.server.AuthMiddleware
import org.http4s.headers.Authorization
val authUser: Kleisli[OptionT[IO, *], Request[IO], User] =
  Kleisli(req => OptionT.liftF(JwtClaim.decode(req)))
val secured = AuthMiddleware(authUser)
val authedRoutes: AuthedRoutes[User, IO] = AuthedRoutes.of[User, IO] {
  case GET -> Root / "me" as user => Ok(user.name)
}
`
	ents := extractFull(t, "custom_scala_frameworks", fi("Secured.scala", "scala", src))

	mw := findByName(ents, "auth_check", "auth:http4s:AuthMiddleware:authUser")
	if mw == nil {
		t.Fatalf("expected AuthMiddleware auth_check named with authenticator authUser; got:%s", dumpRecords(ents))
	}
	if mw.Properties["authenticator"] != "authUser" {
		t.Errorf("expected authenticator=authUser, got %q", mw.Properties["authenticator"])
	}
	if mw.Properties["auth_method"] != "jwt" {
		t.Errorf("expected auth_method=jwt (verifyJwt in window), got %q", mw.Properties["auth_method"])
	}
	if findByName(ents, "auth_check", "auth:http4s:AuthedRoutes") == nil {
		t.Errorf("expected AuthedRoutes auth_check entity; got:%s", dumpRecords(ents))
	}
}

// ---------------------------------------------------------------------------
// akka-http AUTH — authenticateBasic(realm) + authenticateOAuth2 + authorize.
// realm string captured via quoted-string match (a ')' inside must not truncate).
// ---------------------------------------------------------------------------

func TestDeepAuthAkkaHttpDirectives(t *testing.T) {
	src := `
import akka.http.scaladsl.server.Directives._
val route =
  authenticateBasic(realm = "secure (prod)", myBasicAuth) { user =>
    authorize(user.isAdmin) {
      authenticateOAuth2(realm = "api", tokenAuth) { token =>
        complete("ok")
      }
    }
  }
`
	ents := extractFull(t, "custom_scala_frameworks", fi("Routes.scala", "scala", src))

	basic := findByProp(ents, "auth_check", "directive", "authenticateBasic")
	if basic == nil {
		t.Fatalf("expected authenticateBasic directive; got:%s", dumpRecords(ents))
	}
	if basic.Properties["auth_method"] != "basic" {
		t.Errorf("expected auth_method=basic, got %q", basic.Properties["auth_method"])
	}
	// Realm contains a ')' — must be captured whole, not truncated at the paren.
	if basic.Properties["realm"] != "secure (prod)" {
		t.Errorf("expected realm=%q, got %q", "secure (prod)", basic.Properties["realm"])
	}

	oauth := findByProp(ents, "auth_check", "directive", "authenticateOAuth2")
	if oauth == nil || oauth.Properties["auth_method"] != "oauth2" {
		t.Fatalf("expected authenticateOAuth2 with auth_method=oauth2; got:%s", dumpRecords(ents))
	}
	if oauth.Properties["realm"] != "api" {
		t.Errorf("expected realm=api, got %q", oauth.Properties["realm"])
	}

	authz := findByProp(ents, "auth_check", "auth_method", "authorize")
	if authz == nil || authz.Properties["directive"] != "authorize" {
		t.Errorf("expected authorize directive entity; got:%s", dumpRecords(ents))
	}
}

// ---------------------------------------------------------------------------
// play AUTH — ActionBuilder / ActionFilter / ActionRefiner with named action.
// ---------------------------------------------------------------------------

func TestDeepAuthPlayActions(t *testing.T) {
	src := `
import play.api.mvc._
class AuthenticatedAction @Inject() (parser: BodyParsers.Default)(implicit ec: ExecutionContext)
    extends ActionBuilder[UserRequest, AnyContent] {
  def invokeBlock[A](request: Request[A], block: UserRequest[A] => Future[Result]) = {
    val bearer = request.headers.get("Authorization")
    block(new UserRequest(decodeJwt(bearer), request))
  }
}
object AdminFilter extends ActionFilter[Request] {
  def filter[A](req: Request[A]) = Future.successful(None)
}
`
	ents := extractFull(t, "custom_scala_frameworks", fi("Actions.scala", "scala", src))

	ab := findByName(ents, "auth_check", "auth:play:ActionBuilder:AuthenticatedAction")
	if ab == nil {
		t.Fatalf("expected play ActionBuilder auth entity named AuthenticatedAction; got:%s", dumpRecords(ents))
	}
	if ab.Properties["action_kind"] != "ActionBuilder" || ab.Properties["action_name"] != "AuthenticatedAction" {
		t.Errorf("expected action_kind=ActionBuilder action_name=AuthenticatedAction, got kind=%q name=%q",
			ab.Properties["action_kind"], ab.Properties["action_name"])
	}
	if ab.Properties["auth_method"] != "jwt" {
		t.Errorf("expected auth_method=jwt (decodeJwt in window), got %q", ab.Properties["auth_method"])
	}
	if findByName(ents, "auth_check", "auth:play:ActionFilter:AdminFilter") == nil {
		t.Errorf("expected play ActionFilter AdminFilter auth entity; got:%s", dumpRecords(ents))
	}
}

// ---------------------------------------------------------------------------
// http4s MIDDLEWARE — named (CORS/GZip/Logger) + composition order mw1(mw2(routes)).
// ---------------------------------------------------------------------------

func TestDeepMiddlewareHttp4sNamedAndComposition(t *testing.T) {
	src := `
import org.http4s.server.middleware._
val app = CORS.policy(GZip(Logger.httpApp(true, true)(routes)))
val httpApp = CORS(GZip(routes))
`
	ents := extractFull(t, "custom_scala_frameworks", fi("Server.scala", "scala", src))

	for _, mw := range []string{"CORS", "GZip", "Logger"} {
		if findByName(ents, "middleware", "middleware:http4s:"+mw) == nil {
			t.Errorf("expected http4s named middleware %s; got:%s", mw, dumpRecords(ents))
		}
	}
	// Composition order CORS>GZip(routes) from CORS(GZip(routes)).
	comp := findByName(ents, "middleware", "middleware:http4s:compose:CORS>GZip(routes)")
	if comp == nil {
		t.Fatalf("expected composition CORS>GZip(routes); got:%s", dumpRecords(ents))
	}
	if comp.Properties["composition_order"] != "CORS>GZip(routes)" ||
		comp.Properties["outer"] != "CORS" || comp.Properties["inner"] != "GZip" {
		t.Errorf("expected outer=CORS inner=GZip order=CORS>GZip(routes), got order=%q outer=%q inner=%q",
			comp.Properties["composition_order"], comp.Properties["outer"], comp.Properties["inner"])
	}
}

// ---------------------------------------------------------------------------
// akka-http MIDDLEWARE — handleRejections/handleExceptions (named) + cors + transform.
// ---------------------------------------------------------------------------

func TestDeepMiddlewareAkkaHttp(t *testing.T) {
	src := `
import akka.http.scaladsl.server.Directives._
val route =
  handleRejections(myRejectionHandler) {
    handleExceptions(myExceptionHandler) {
      cors() {
        encodeResponse {
          mapRequest(addTraceHeader) {
            complete("ok")
          }
        }
      }
    }
  }
`
	ents := extractFull(t, "custom_scala_frameworks", fi("Routes.scala", "scala", src))

	rej := findByName(ents, "middleware", "middleware:akka:handleRejections:myRejectionHandler")
	if rej == nil || rej.Properties["handler"] != "myRejectionHandler" {
		t.Fatalf("expected handleRejections w/ handler=myRejectionHandler; got:%s", dumpRecords(ents))
	}
	if findByName(ents, "middleware", "middleware:akka:handleExceptions:myExceptionHandler") == nil {
		t.Errorf("expected handleExceptions w/ named handler; got:%s", dumpRecords(ents))
	}
	if findByName(ents, "middleware", "middleware:akka:cors") == nil {
		t.Errorf("expected cors middleware; got:%s", dumpRecords(ents))
	}
	for _, d := range []string{"encodeResponse", "mapRequest"} {
		if findByName(ents, "middleware", "middleware:akka:"+d) == nil {
			t.Errorf("expected transform directive %s; got:%s", d, dumpRecords(ents))
		}
	}
}

// ---------------------------------------------------------------------------
// play MIDDLEWARE — global filter chain order + custom EssentialFilter defs.
// ---------------------------------------------------------------------------

func TestDeepMiddlewarePlayFilterChain(t *testing.T) {
	src := `
import play.api.http.DefaultHttpFilters
import play.api.mvc._
class Filters @Inject() (
  securityHeaders: SecurityHeadersFilter,
  csrf: CSRFFilter,
  gzip: GzipFilter
) extends DefaultHttpFilters(securityHeaders, csrf, gzip)

class AccessLogFilter extends EssentialFilter {
  def apply(next: EssentialAction) = next
}
`
	ents := extractFull(t, "custom_scala_frameworks", fi("Filters.scala", "scala", src))

	chain := findByProp(ents, "middleware", "chain_kind", "DefaultHttpFilters")
	if chain == nil {
		t.Fatalf("expected DefaultHttpFilters chain; got:%s", dumpRecords(ents))
	}
	if chain.Properties["filter_chain"] != "securityHeaders>csrf>gzip" {
		t.Errorf("expected filter_chain=securityHeaders>csrf>gzip (declared order), got %q",
			chain.Properties["filter_chain"])
	}
	if findByName(ents, "middleware", "middleware:play:filterDef:AccessLogFilter") == nil {
		t.Errorf("expected EssentialFilter def AccessLogFilter; got:%s", dumpRecords(ents))
	}
}

// play filters via def filters = Seq(...) ordering.
func TestDeepMiddlewarePlayFiltersSeq(t *testing.T) {
	src := `
import play.api.mvc.EssentialFilter
import play.api.http.HttpFilters
class MyFilters @Inject() (a: LoggingFilter, b: AuthFilter) extends HttpFilters {
  override def filters: Seq[EssentialFilter] = Seq(a, b)
}
`
	ents := extractFull(t, "custom_scala_frameworks", fi("MyFilters.scala", "scala", src))
	chain := findByProp(ents, "middleware", "chain_kind", "EssentialFilterSeq")
	if chain == nil {
		t.Fatalf("expected EssentialFilterSeq chain; got:%s", dumpRecords(ents))
	}
	if chain.Properties["filter_chain"] != "a>b" {
		t.Errorf("expected filter_chain=a>b, got %q", chain.Properties["filter_chain"])
	}
}
