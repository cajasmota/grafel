// Sails backend-HTTP log_extraction fixture (#2905).
// Sails ships a captains-log/winston-backed `sails.log`; controllers also
// import winston directly for structured logging. The observability
// extractor attributes a winston log signal.
import winston from "winston";

const logger = winston.createLogger({ level: "info" });

export default {
  find: async function (req, res) {
    logger.info("find users", { ip: req.ip });
    return res.json([]);
  },
};
