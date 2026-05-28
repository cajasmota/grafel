export class CreateUsers1700 implements MigrationInterface {
  async up(queryRunner) {
    await queryRunner.createTable(new Table({ name: "users", columns: [] }))
    await queryRunner.addColumn("users", new TableColumn({ name: "email" }))
  }
  async down(queryRunner) { await queryRunner.dropTable("users") }
}
