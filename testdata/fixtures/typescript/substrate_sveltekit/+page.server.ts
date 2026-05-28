// SvelteKit server route fixture — proves taint-source, taint-sink,
// sanitizer, and vulnerability_finding for the jsts substrate sniffers
// (issue #2850).  Hand-written; no node_modules.
//
// SvelteKit load() receives a RequestEvent whose .request is a standard
// Fetch API Request object.  Server actions receive a RequestEvent too,
// where request.body / request.json() carry user input.
import { db } from '$lib/db';
import DOMPurify from 'dompurify';

const SECRET = process.env.SK_SECRET ?? 'dev-only';

// Source: request.body is a recognised taint source (HTTP body).
export async function load({ request, params, url }) {
  const body = request.body;
  const q = request.query;

  // Sink: SQL injection via template-string concatenation.
  const unsafe = db.query(`SELECT * FROM users WHERE id = ${q}`);

  // Sanitizer: DOMPurify.sanitize.
  const safeContent = DOMPurify.sanitize(q ?? '');

  // Sink: eval — command injection.
  function runDynamic(input: string) {
    eval(input);
  }

  return { unsafe, safeContent };
}

// Actions also receive request.body — additional source signal.
export const actions = {
  default: async ({ request }) => {
    const data = request.body;
    // Sink: fs.readFile with a user-controlled path.
    const result = await fs.readFile(data, 'utf-8');
    return { result };
  },
};
