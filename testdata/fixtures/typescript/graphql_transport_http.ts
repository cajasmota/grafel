// GraphQL resolvers over HTTP — Apollo Server standalone (#2906).
// startStandaloneServer binds the resolver map to an HTTP transport.
import { ApolloServer } from '@apollo/server';
import { startStandaloneServer } from '@apollo/server/standalone';

const resolvers = {
  Query: {
    hello: () => 'world',
  },
  Mutation: {
    setName: (_: unknown, { name }: { name: string }) => name,
  },
};

const server = new ApolloServer({ resolvers });
await startStandaloneServer(server, { listen: { port: 4000 } });
