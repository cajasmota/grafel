// Feathers backend-HTTP log_extraction fixture (#2905).
// Feathers services log through winston (its documented logger). The
// observability extractor attributes a winston log signal.
import winston from "winston";

const logger = winston.createLogger({ level: "info" });

export class UsersService {
  async find(params) {
    logger.info("find users", { query: params.query });
    return [];
  }
}
