module.exports = {
  async up(queryInterface, Sequelize) {
    await queryInterface.createTable('Users', { id: { type: Sequelize.INTEGER } })
    await queryInterface.addColumn('Users', 'email', { type: Sequelize.STRING })
  },
  async down(queryInterface) { await queryInterface.dropTable('Users') }
}
