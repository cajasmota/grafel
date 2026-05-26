// Source: https://github.com/apollographql/apollo-server (synthetic based on real Apollo resolvers patterns) | License: MIT

import { GraphQLError } from 'graphql';
import { Resolvers, PostStatus, UserRole } from './generated/graphql';
import { Context } from './context';
import { pubsub, COMMENT_ADDED, POST_PUBLISHED } from './pubsub';

export const resolvers: Resolvers = {
  Query: {
    me: (_, __, { user }) => {
      if (!user) throw new GraphQLError('Not authenticated', {
        extensions: { code: 'UNAUTHENTICATED' }
      });
      return user;
    },

    post: async (_, { id, slug }, { dataSources }) => {
      if (id) return dataSources.postAPI.getById(id);
      if (slug) return dataSources.postAPI.getBySlug(slug);
      throw new GraphQLError('Must provide id or slug');
    },

    posts: async (_, args, { dataSources }) => {
      return dataSources.postAPI.list(args);
    },

    tags: async (_, __, { dataSources }) => {
      return dataSources.tagAPI.listAll();
    },
  },

  Mutation: {
    login: async (_, { input }, { dataSources, res }) => {
      const { token, user } = await dataSources.authAPI.login(input.email, input.password);
      res.cookie('token', token, { httpOnly: true, secure: true, sameSite: 'strict' });
      return { token, user };
    },

    register: async (_, { input }, { dataSources }) => {
      return dataSources.authAPI.register(input);
    },

    createPost: async (_, { input }, { user, dataSources }) => {
      if (!user) throw new GraphQLError('Not authenticated', {
        extensions: { code: 'UNAUTHENTICATED' }
      });
      return dataSources.postAPI.create({ ...input, authorId: user.id });
    },

    updatePost: async (_, { id, input }, { user, dataSources }) => {
      if (!user) throw new GraphQLError('Not authenticated', {
        extensions: { code: 'UNAUTHENTICATED' }
      });
      const post = await dataSources.postAPI.getById(id);
      if (!post) throw new GraphQLError('Post not found', {
        extensions: { code: 'NOT_FOUND' }
      });
      if (post.authorId !== user.id && user.role !== UserRole.Admin) {
        throw new GraphQLError('Forbidden', { extensions: { code: 'FORBIDDEN' } });
      }
      return dataSources.postAPI.update(id, input);
    },

    publishPost: async (_, { id }, { user, dataSources }) => {
      if (!user) throw new GraphQLError('Not authenticated', {
        extensions: { code: 'UNAUTHENTICATED' }
      });
      const post = await dataSources.postAPI.publish(id);
      pubsub.publish(POST_PUBLISHED, { postPublished: post });
      return post;
    },

    createComment: async (_, { input }, { user, dataSources }) => {
      if (!user) throw new GraphQLError('Not authenticated', {
        extensions: { code: 'UNAUTHENTICATED' }
      });
      const comment = await dataSources.commentAPI.create({
        ...input,
        authorId: user.id,
      });
      pubsub.publish(COMMENT_ADDED, {
        commentAdded: comment,
        postId: input.postId,
      });
      return comment;
    },

    deletePost: async (_, { id }, { user, dataSources }) => {
      if (!user || user.role !== UserRole.Admin) {
        throw new GraphQLError('Forbidden', { extensions: { code: 'FORBIDDEN' } });
      }
      await dataSources.postAPI.delete(id);
      return true;
    },
  },

  Subscription: {
    commentAdded: {
      subscribe: (_, { postId }) =>
        pubsub.asyncIterator([COMMENT_ADDED]),
      resolve: (payload) => payload.commentAdded,
    },
    postPublished: {
      subscribe: () => pubsub.asyncIterator([POST_PUBLISHED]),
      resolve: (payload) => payload.postPublished,
    },
  },

  Post: {
    author: async (post, _, { dataSources }) => {
      return dataSources.userAPI.getById(post.authorId);
    },
    tags: async (post, _, { dataSources }) => {
      return dataSources.tagAPI.getByPostId(post.id);
    },
    comments: async (post, args, { dataSources }) => {
      return dataSources.commentAPI.listByPost(post.id, args);
    },
  },

  User: {
    posts: async (user, _, { dataSources }) => {
      const result = await dataSources.postAPI.list({ authorId: user.id, first: 10 });
      return result.edges.map(e => e.node);
    },
  },
};
