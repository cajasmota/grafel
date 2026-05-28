CREATE TABLE "users" ("id" serial PRIMARY KEY NOT NULL, "email" text);
--> statement-breakpoint
ALTER TABLE "users" ADD COLUMN "name" text;
