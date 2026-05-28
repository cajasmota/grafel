import { Migration } from '@mikro-orm/migrations'
export class Migration20240101 extends Migration {
  async up() { this.addSql('CREATE TABLE "user" ("id" serial primary key);'); this.addSql('ALTER TABLE "user" ADD COLUMN "name" varchar;') }
}
