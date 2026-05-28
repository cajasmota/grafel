CREATE TABLE "User" ("id" SERIAL NOT NULL, "email" TEXT NOT NULL, CONSTRAINT "User_pkey" PRIMARY KEY ("id"));
ALTER TABLE "User" ADD COLUMN "name" TEXT;
CREATE UNIQUE INDEX "User_email_key" ON "User"("email");
