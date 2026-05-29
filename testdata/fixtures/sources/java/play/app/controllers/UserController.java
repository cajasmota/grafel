package controllers;

import play.mvc.Controller;
import play.mvc.Result;
import play.mvc.With;
import play.data.Form;
import play.data.FormFactory;
import javax.inject.Inject;

import actions.AuthAction;
import models.UserForm;

/**
 * Play Framework controller fixture — issue #3090.
 * Demonstrates: Result-returning methods, @Inject DI, @With action composition,
 * form binding/DTO extraction, and request body access.
 */
@With(AuthAction.class)
public class UserController extends Controller {

    @Inject
    private FormFactory formFactory;

    public Result list() {
        return ok("user list");
    }

    public Result show(Long id) {
        return ok("user " + id);
    }

    public Result create() {
        Form<UserForm> form = formFactory.form(UserForm.class).bindFromRequest();
        if (form.hasErrors()) {
            return badRequest(form.errorsAsJson());
        }
        UserForm userData = form.get();
        return created("created");
    }

    public Result update(Long id) {
        String body = request().body().asText().orElse("");
        return ok("updated " + id);
    }

    public Result delete(Long id) {
        return ok("deleted " + id);
    }
}
