// app.ts — Express-flavoured server entry point.
// Proves import_resolution_quality: API_BASE_URL and DB_URL are imported
// from ./config and used as route-base / connection-string literals; the
// substrate constant-propagation pass must resolve them across the module
// boundary (ProvenanceCrossFile → ProvenanceLiteral / ProvenanceEnvFallback).
import express from "express";
import { API_BASE_URL, DB_URL } from "./config";

const app = express();

// Route handler — uses the imported base URL as a path prefix constant.
app.get("/health", (_req: any, res: any) => {
  res.json({ status: "ok", apiBase: API_BASE_URL });
});

// Database connection — uses the cross-file env-fallback constant.
async function connectDb() {
  console.log("Connecting to", DB_URL);
}

connectDb();
app.listen(8080);
