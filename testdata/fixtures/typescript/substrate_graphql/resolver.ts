// GraphQL resolver fixture — proves taint-source, taint-sink, sanitizer,
// and vulnerability_finding for the jsts substrate sniffers (issue #3057).
// Hand-written; no node_modules.
//
// GraphQL resolvers receive (parent, args, context, info).  The context
// object typically carries the raw HTTP request, so context.request.body
// and req.body are the canonical taint-source signals — both matched by
// jstsSourceReqRe.  The args object itself is the primary user-input
// channel; for this proving fixture we use context.request shapes that the
// existing jsts sniffer already recognises.
import { ApolloServer } from '@apollo/server';
import { startStandaloneServer } from '@apollo/server/standalone';
import DOMPurify from 'dompurify';
import { db } from '../lib/db';

const API_URL = 'https://graphql.example.com';
const SECRET = process.env.GRAPHQL_SECRET ?? 'dev-only';

const resolvers = {
  Query: {
    user: async (_parent: unknown, _args: unknown, context: any) => {
      // Source: context carries the incoming HTTP request.
      const req = context.request;
      const userId = req.params.id;
      const q = req.query;

      // Sink: raw SQL with template-string interpolation.
      const row = db.query(`SELECT * FROM users WHERE id = ${userId}`);

      // Sanitizer: DOMPurify.sanitize.
      const safeInput = DOMPurify.sanitize(q.name ?? '');

      return row;
    },
  },
  Mutation: {
    createUser: async (_parent: unknown, _args: unknown, context: any) => {
      const request = context.request;
      const body = request.body;

      // Sink: eval — dynamic execution of user-supplied code.
      eval(body.script);

      // Sink: fs.readFile with user-controlled path.
      const content = await fs.readFile(body.path, 'utf-8');

      return { ok: true };
    },
  },
};

const server = new ApolloServer({ resolvers });
await startStandaloneServer(server, { listen: { port: 4000 } });
