// config.ts — shared constants exported for import_resolution_quality fixture.
// Proves: a literal string and an env-fallback constant defined here are
// resolved in app.ts and nest_app.ts via the cross-file IMPORTS walk.

// Literal base URL — re-exported and consumed in app.ts / nest_app.ts.
export const API_BASE_URL = "https://api.example.com";

// Env-fallback database URL — tests the env_fallback provenance path over
// a cross-file hop.
export const DB_URL = process.env.DATABASE_URL ?? "postgres://localhost:5432/mydb";

// Port constant consumed by the NestJS-flavoured module.
export const SERVER_PORT = "3000";
