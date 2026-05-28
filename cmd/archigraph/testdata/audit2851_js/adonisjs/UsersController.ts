// AdonisJS controller. Methods back the Route.get/post/'UsersController.x' refs.
export default class UsersController {
  public async index() {
    return [{ id: 1 }];
  }
  public async store() {
    return { ok: true };
  }
  public async show({ params }) {
    return { id: params.id };
  }
}
