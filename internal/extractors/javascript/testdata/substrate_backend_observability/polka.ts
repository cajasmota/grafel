// Polka backend-HTTP log_extraction fixture (#2905).
// Polka is a minimal router with no bundled logger; apps wire the `morgan`
// access logger as middleware. The observability extractor attributes a
// morgan log signal.
import polka from "polka";
import morgan from "morgan";

const app = polka();
app.use(morgan("tiny"));

app.get("/health", (req, res) => {
  res.end(JSON.stringify({ ok: true }));
});
