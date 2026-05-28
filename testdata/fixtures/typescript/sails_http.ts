// Sails middleware_coverage fixture (#2853). Hand-written, manifest-free.
// Sails declares its global middleware pipeline in config/http.js as the
// `order` array under `middleware`. This is a bespoke, declarative idiom: there
// is no per-route middleware chain — the order array applies app-wide. This is
// the framework_specific middleware cell for Sails.
module.exports.http = {
  middleware: {
    order: [
      'cookieParser',
      'session',
      'bodyParser',
      'compress',
      'poweredBy',
      'router',
      'www',
      'favicon',
    ],
    bodyParser: (req, res, next) => next(),
  },
}
