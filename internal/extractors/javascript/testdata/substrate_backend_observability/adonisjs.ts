// AdonisJS backend-HTTP log_extraction fixture (#2905).
// AdonisJS ships a pino-backed Logger; controllers import it and call
// logger.info. The observability extractor attributes a pino log signal.
import pino from "pino";

const logger = pino({ level: "info" });

export default class UsersController {
  async index({ response }) {
    logger.info("listing users");
    return response.json([]);
  }
}
