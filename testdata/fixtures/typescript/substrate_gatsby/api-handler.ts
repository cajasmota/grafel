// Gatsby Functions (API route) fixture — proves taint-source, taint-sink,
// sanitizer, and vulnerability_finding for the jsts substrate sniffers
// (issue #3057).  Hand-written; no node_modules.
//
// Gatsby Functions live under src/api/ and receive the standard Node.js
// GatsbyFunctionRequest / GatsbyFunctionResponse pair.  The underlying
// request object is a plain Node.js IncomingMessage-compatible wrapper, so
// req.body, req.query, and req.params are the canonical taint sources —
// exactly what jstsSourceReqRe recognises.
import { db } from '../../lib/db';
import DOMPurify from 'dompurify';
import { graphql } from 'gatsby';

export const query = graphql`
  query PageQuery {
    site {
      siteMetadata {
        title
      }
    }
  }
`;

const API_BASE = process.env.GATSBY_API_URL ?? 'https://api.example.com';
const SECRET = process.env.GATSBY_SECRET ?? 'dev-only';

// Source: req.body and req.query are recognised taint sources.
export default async function handler(req, res) {
  const body = req.body;
  const q = req.query;
  const userId = req.params.id;

  // Sink: SQL injection via template-string concatenation.
  const unsafe = db.query(`SELECT * FROM users WHERE id = ${userId}`);

  // Sanitizer: DOMPurify.sanitize.
  const safeHtml = DOMPurify.sanitize(body ?? '');

  // Sink: eval — dynamic code execution.
  function runDynamic(input: string) {
    eval(input);
  }

  // Sink: fs.readFile with user-controlled path.
  const content = await fs.readFile(q.path, 'utf-8');

  res.json({ ok: true });
}
