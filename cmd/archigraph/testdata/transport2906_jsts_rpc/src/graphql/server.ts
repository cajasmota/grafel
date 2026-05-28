// Apollo Server standalone — resolver map served over HTTP only. The
// resolver-field synthetics emitted here are stamped transport=http.
import { ApolloServer } from "@apollo/server";
import { startStandaloneServer } from "@apollo/server/standalone";

const resolvers = {
  Query: {
    posts: () => [],
    post: (_: unknown, { id }: { id: string }) => ({ id }),
  },
  Mutation: {
    createPost: (_: unknown, { title }: { title: string }) => ({ id: 1, title }),
  },
};

const server = new ApolloServer({ resolvers });
await startStandaloneServer(server, { listen: { port: 4000 } });
