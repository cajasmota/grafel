// GraphQL resolvers over both HTTP and WebSocket (#2906).
// Queries/mutations are served via the Apollo express4 middleware (HTTP);
// subscriptions are served via graphql-ws useServer over a WebSocketServer.
import { expressMiddleware } from '@apollo/server/express4';
import { useServer } from 'graphql-ws/lib/use/ws';
import { WebSocketServer } from 'ws';

const resolvers = {
  Query: {
    hello: () => 'world',
  },
  Subscription: {
    messageAdded: { subscribe: () => null },
  },
};

app.use('/graphql', expressMiddleware(server));

const wsServer = new WebSocketServer({ server: httpServer, path: '/graphql' });
useServer({ schema }, wsServer);
