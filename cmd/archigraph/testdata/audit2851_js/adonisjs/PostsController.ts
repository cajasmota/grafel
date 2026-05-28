// AdonisJS resourceful controller backing Route.resource('posts', ...).
export default class PostsController {
  public async index() {
    return [];
  }
  public async store() {
    return {};
  }
  public async show({ params }) {
    return { id: params.id };
  }
  public async update({ params }) {
    return { id: params.id };
  }
  public async destroy() {
    return {};
  }
}
