// Feathers service registration. Services are registered at mount paths;
// each service expands to the standard REST verb set.
import feathers from "@feathersjs/feathers";

class MessageService {
  async find() {
    return [];
  }
  async get(id) {
    return { id };
  }
  async create(data) {
    return data;
  }
}

const app = feathers();
app.use("/messages", new MessageService());

export default app;
