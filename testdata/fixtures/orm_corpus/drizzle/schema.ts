import { pgTable, serial, text } from 'drizzle-orm/pg-core'
export const users = pgTable("users", { id: serial("id").primaryKey(), email: text("email") })
export const usersRelations = relations(users, ({ many }) => ({ posts: many(posts) }))
