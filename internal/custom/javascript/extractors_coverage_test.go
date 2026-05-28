package javascript_test

// Additional tests to push coverage above 80%.

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Express — additional coverage
// ---------------------------------------------------------------------------

func TestExpressErrorHandler(t *testing.T) {
	src := `
function globalErrorHandler(err, req, res, next) { res.status(500).json({}) }
`
	ents := extract(t, "custom_js_express", fi("app.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Pattern", "globalErrorHandler") {
		t.Error("expected globalErrorHandler error handler")
	}
}

func TestExpressInlineErrorHandler(t *testing.T) {
	src := `app.use(function(err, req, res, next) { res.status(500).send('error') })`
	ents := extract(t, "custom_js_express", fi("app.ts", "typescript", src))
	if !containsSubtype(ents, "error_handler") {
		t.Error("expected inline error_handler entity")
	}
}

func TestExpressStaticServe(t *testing.T) {
	src := `app.use(express.static('public'))`
	ents := extract(t, "custom_js_express", fi("app.ts", "typescript", src))
	if !containsSubtype(ents, "static") {
		t.Error("expected static entity")
	}
}

func TestExpressConfig(t *testing.T) {
	src := `
app.set('view engine', 'pug')
app.engine('pug', pugEngine)
`
	ents := extract(t, "custom_js_express", fi("app.ts", "typescript", src))
	if !containsSubtype(ents, "config") {
		t.Error("expected config entity")
	}
}

func TestExpressSocketIO(t *testing.T) {
	src := `io.on('connection', (socket) => { console.log('connected') })`
	ents := extract(t, "custom_js_express", fi("app.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "socket:connection") {
		t.Error("expected socket:connection entity")
	}
}

func TestExpressParam(t *testing.T) {
	src := `app.param('userId', async (req, res, next, id) => { req.user = await User.findById(id); next() })`
	ents := extract(t, "custom_js_express", fi("app.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Pattern", "param:userId") {
		t.Error("expected param:userId entity")
	}
}

// ---------------------------------------------------------------------------
// NestJS — additional coverage
// ---------------------------------------------------------------------------

func TestNestJSInjectable(t *testing.T) {
	src := `
@Injectable()
export class UsersService {}
`
	ents := extract(t, "custom_js_nestjs", fi("users.service.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "UsersService") {
		t.Error("expected UsersService injectable")
	}
}

func TestNestJSGuard(t *testing.T) {
	src := `
export class JwtAuthGuard implements CanActivate {
  canActivate(context: ExecutionContext) { return true }
}
`
	ents := extract(t, "custom_js_nestjs", fi("auth.guard.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "JwtAuthGuard") {
		t.Error("expected JwtAuthGuard guard entity")
	}
}

func TestNestJSWebSocketGateway(t *testing.T) {
	src := `
@WebSocketGateway(3001)
export class EventsGateway {}
`
	ents := extract(t, "custom_js_nestjs", fi("events.gateway.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Pattern", "EventsGateway") {
		t.Error("expected EventsGateway gateway entity")
	}
}

func TestNestJSSubscribeMessage(t *testing.T) {
	src := `
@SubscribeMessage('message')
handleMessage(client: Socket, payload: any) {}
`
	ents := extract(t, "custom_js_nestjs", fi("events.gateway.ts", "typescript", src))
	if !containsSubtype(ents, "endpoint") {
		t.Error("expected endpoint from @SubscribeMessage")
	}
}

func TestNestJSResolver(t *testing.T) {
	src := `
@Resolver(() => User)
export class UsersResolver {}
`
	ents := extract(t, "custom_js_nestjs", fi("users.resolver.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "UsersResolver") {
		t.Error("expected UsersResolver resolver")
	}
}

func TestNestJSGraphQLQuery(t *testing.T) {
	src := `
@Query(() => [User])
async getUsers() { return [] }
`
	ents := extract(t, "custom_js_nestjs", fi("users.resolver.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "getUsers") {
		t.Error("expected getUsers query")
	}
}

func TestNestJSMutation(t *testing.T) {
	src := `
@Mutation(() => User)
async createUser(@Args('input') input: CreateUserInput) { return {} }
`
	ents := extract(t, "custom_js_nestjs", fi("users.resolver.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "createUser") {
		t.Error("expected createUser mutation")
	}
}

func TestNestJSPipe(t *testing.T) {
	src := `
export class ParseIntPipe implements PipeTransform {
  transform(value: string) { return parseInt(value, 10) }
}
`
	ents := extract(t, "custom_js_nestjs", fi("parse-int.pipe.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "ParseIntPipe") {
		t.Error("expected ParseIntPipe pipe")
	}
}

func TestNestJSExceptionFilter(t *testing.T) {
	src := `
@Catch(HttpException)
export class HttpExceptionFilter {}
`
	ents := extract(t, "custom_js_nestjs", fi("exception.filter.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Pattern", "HttpExceptionFilter") {
		t.Error("expected HttpExceptionFilter exception filter")
	}
}

func TestNestJSInterval(t *testing.T) {
	src := `
@Interval(10000)
async handleInterval() { console.log('tick') }
`
	ents := extract(t, "custom_js_nestjs", fi("sched.ts", "typescript", src))
	if !containsSubtype(ents, "job") {
		t.Error("expected job from @Interval")
	}
}

func TestNestJSParamDecorator(t *testing.T) {
	src := `export const User = createParamDecorator((data: unknown, ctx: ExecutionContext) => {})`
	ents := extract(t, "custom_js_nestjs", fi("user.decorator.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Pattern", "User") {
		t.Error("expected User param decorator")
	}
}

func TestNestJSMessagePattern(t *testing.T) {
	src := `
@MessagePattern({ cmd: 'sum' })
async accumulate(data: number[]) {}
`
	ents := extract(t, "custom_js_nestjs", fi("math.controller.ts", "typescript", src))
	if !containsSubtype(ents, "endpoint") {
		t.Error("expected endpoint from @MessagePattern")
	}
}

// Angular custom-extractor coverage tests removed (#2933) along with the
// redundant custom_js_angular extractor. The core javascript AST path covers
// @Directive/@Pipe/@Input/@Output/guards with richer entities; see
// internal/extractors/javascript/issue2854_angular_test.go et al.

// ---------------------------------------------------------------------------
// Mongoose — additional coverage
// ---------------------------------------------------------------------------

func TestMongooseVirtual(t *testing.T) {
	src := `UserSchema.virtual('fullName').get(function() { return this.firstName + ' ' + this.lastName })`
	ents := extract(t, "custom_js_mongoose", fi("user.model.ts", "typescript", src))
	if !containsSubtype(ents, "function") {
		t.Error("expected virtual function entity")
	}
}

func TestMongoosePopulate(t *testing.T) {
	src := `const post = await Post.findById(id).populate('author')`
	ents := extract(t, "custom_js_mongoose", fi("post.service.ts", "typescript", src))
	if !containsSubtype(ents, "query") {
		t.Error("expected populate query entity")
	}
}

func TestMongooseInstanceMethod(t *testing.T) {
	src := `UserSchema.methods.comparePassword = async function(password) { return bcrypt.compare(password, this.password) }`
	ents := extract(t, "custom_js_mongoose", fi("user.model.ts", "typescript", src))
	if !containsSubtype(ents, "function") {
		t.Error("expected instance method entity")
	}
}

func TestMongooseStaticMethod(t *testing.T) {
	src := `UserSchema.statics.findByEmail = async function(email) { return this.findOne({ email }) }`
	ents := extract(t, "custom_js_mongoose", fi("user.model.ts", "typescript", src))
	if !containsSubtype(ents, "function") {
		t.Error("expected static method entity")
	}
}

// ---------------------------------------------------------------------------
// LangChain — additional coverage
// ---------------------------------------------------------------------------

func TestLangchainVectorStore(t *testing.T) {
	src := `const vectorStore = await Chroma.fromDocuments(docs, embeddings, { collectionName: 'users' })`
	ents := extract(t, "custom_js_langchain", fi("index.ts", "typescript", src))
	if !containsSubtype(ents, "vector_store") {
		t.Error("expected vector_store entity")
	}
}

func TestLangchainTool(t *testing.T) {
	src := `const tool = new DynamicTool({ name: 'calculator', func: (input) => eval(input) })`
	ents := extract(t, "custom_js_langchain", fi("tools.ts", "typescript", src))
	if !containsSubtype(ents, "tool") {
		t.Error("expected tool entity")
	}
}

func TestLangchainAgentExecutor(t *testing.T) {
	src := `const executor = AgentExecutor.fromAgentAndTools({ agent, tools })`
	ents := extract(t, "custom_js_langchain", fi("agent.ts", "typescript", src))
	if !containsSubtype(ents, "agent_executor") {
		t.Error("expected agent_executor entity")
	}
}

// ---------------------------------------------------------------------------
// TypeORM — additional coverage
// ---------------------------------------------------------------------------

func TestTypeORMColumn(t *testing.T) {
	src := `
@Column()
name: string

@PrimaryGeneratedColumn()
id: number
`
	ents := extract(t, "custom_js_typeorm", fi("user.entity.ts", "typescript", src))
	if !containsSubtype(ents, "column") {
		t.Error("expected column entity")
	}
}

func TestTypeORMRepository(t *testing.T) {
	src := `const repo = getRepository(User)`
	ents := extract(t, "custom_js_typeorm", fi("user.service.ts", "typescript", src))
	if !containsSubtype(ents, "repository") {
		t.Error("expected repository entity")
	}
}

func TestTypeORMQueryBuilder(t *testing.T) {
	src := `const users = await getRepository(User).createQueryBuilder('user').where('user.id = :id', { id }).getOne()`
	ents := extract(t, "custom_js_typeorm", fi("user.service.ts", "typescript", src))
	if !containsSubtype(ents, "query") {
		t.Error("expected query entity from QueryBuilder")
	}
}

// ---------------------------------------------------------------------------
// React — additional coverage
// ---------------------------------------------------------------------------

func TestReactClassComponent(t *testing.T) {
	src := `
class UserList extends React.Component {
  render() { return <div /> }
}
`
	ents := extract(t, "custom_js_react", fi("UserList.tsx", "typescript", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "UserList") {
		t.Error("expected UserList class component")
	}
}

func TestReactUseContext(t *testing.T) {
	src := `const theme = useContext(ThemeContext)`
	ents := extract(t, "custom_js_react", fi("Component.tsx", "typescript", src))
	if !containsSubtype(ents, "context_use") {
		t.Error("expected context_use entity")
	}
}

// ---------------------------------------------------------------------------
// Next.js — additional coverage
// ---------------------------------------------------------------------------

func TestNextjsPagesRouter(t *testing.T) {
	src := `export default function UsersPage() { return <h1>Users</h1> }`
	ents := extract(t, "custom_js_nextjs", fi("pages/users.tsx", "typescript", src))
	if !containsSubtype(ents, "endpoint") {
		t.Error("expected pages router endpoint")
	}
}

func TestNextjsGetStaticProps(t *testing.T) {
	src := `export async function getStaticProps(ctx) { return { props: {} } }`
	ents := extract(t, "custom_js_nextjs", fi("pages/about.tsx", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "getStaticProps") {
		t.Error("expected getStaticProps entity")
	}
}

// ---------------------------------------------------------------------------
// Nuxt — additional coverage
// ---------------------------------------------------------------------------

func TestNuxtMiddleware(t *testing.T) {
	src := `export default defineNuxtRouteMiddleware((to, from) => { if (!user.value) return navigateTo('/login') })`
	ents := extract(t, "custom_js_nuxt", fi("middleware/auth.ts", "typescript", src))
	if !containsSubtype(ents, "middleware") {
		t.Error("expected middleware entity")
	}
}

// ---------------------------------------------------------------------------
// Prisma — additional coverage
// ---------------------------------------------------------------------------

func TestPrismaTransaction(t *testing.T) {
	src := `const result = await prisma.$transaction([prisma.user.create({ data: {} })])`
	ents := extract(t, "custom_js_prisma", fi("service.ts", "typescript", src))
	if !containsSubtype(ents, "transaction") {
		t.Error("expected transaction entity")
	}
}

func TestPrismaClientNew(t *testing.T) {
	src := `const prisma = new PrismaClient({ log: ['query'] })`
	ents := extract(t, "custom_js_prisma", fi("db.ts", "typescript", src))
	if !containsSubtype(ents, "client") {
		t.Error("expected PrismaClient entity")
	}
}

// ---------------------------------------------------------------------------
// Remix — additional coverage
// ---------------------------------------------------------------------------

func TestRemixMeta(t *testing.T) {
	src := `export const meta = () => [{ title: 'Users' }]`
	ents := extract(t, "custom_js_remix", fi("app/routes/users.tsx", "typescript", src))
	if !containsSubtype(ents, "meta") {
		t.Error("expected meta entity")
	}
}

func TestRemixErrorBoundary(t *testing.T) {
	src := `export function ErrorBoundary() { return <div>Error!</div> }`
	ents := extract(t, "custom_js_remix", fi("app/routes/users.tsx", "typescript", src))
	if !containsSubtype(ents, "error_boundary") {
		t.Error("expected error_boundary entity")
	}
}

// ---------------------------------------------------------------------------
// Sequelize — additional coverage
// ---------------------------------------------------------------------------

func TestSequelizeQuery(t *testing.T) {
	src := `const users = await User.findAll({ where: { active: true } })`
	ents := extract(t, "custom_js_sequelize", fi("user.service.ts", "typescript", src))
	if !containsSubtype(ents, "query") {
		t.Error("expected query entity")
	}
}

func TestSequelizeHook(t *testing.T) {
	src := `User.beforeCreate(async (user) => { user.password = await bcrypt.hash(user.password, 10) })`
	ents := extract(t, "custom_js_sequelize", fi("user.model.ts", "typescript", src))
	if !containsSubtype(ents, "lifecycle_hook") {
		t.Error("expected lifecycle_hook entity")
	}
}

// ---------------------------------------------------------------------------
// Svelte — additional coverage
// ---------------------------------------------------------------------------

func TestSvelteFormActions(t *testing.T) {
	src := `
export const actions = {
  default: async ({ request }) => {
    const data = await request.formData()
  }
}
`
	ents := extract(t, "custom_js_svelte", fi("src/routes/contact/+page.server.ts", "typescript", src))
	if !containsSubtype(ents, "form_actions") {
		t.Error("expected form_actions entity")
	}
}

// ---------------------------------------------------------------------------
// tRPC — additional coverage
// ---------------------------------------------------------------------------

func TestTRPCContext(t *testing.T) {
	src := `export async function createContext({ req, res }: CreateExpressContextOptions) { return {} }`
	ents := extract(t, "custom_js_trpc", fi("context.ts", "typescript", src))
	if !containsSubtype(ents, "context") {
		t.Error("expected context entity")
	}
}

// ---------------------------------------------------------------------------
// Vue — additional coverage
// ---------------------------------------------------------------------------

func TestVueProvideInject(t *testing.T) {
	src := `
provide('user', user)
const injectedUser = inject('user')
`
	ents := extract(t, "custom_js_vue", fi("app.ts", "typescript", src))
	if !containsSubtype(ents, "provide") {
		t.Error("expected provide entity")
	}
	if !containsSubtype(ents, "inject") {
		t.Error("expected inject entity")
	}
}

func TestVueRouter(t *testing.T) {
	src := `
const router = createRouter({
  routes: [
    { path: '/users', component: UsersPage },
    { path: '/about', component: AboutPage },
  ]
})
`
	ents := extract(t, "custom_js_vue", fi("router.ts", "typescript", src))
	if !containsSubtype(ents, "router") {
		t.Error("expected router entity")
	}
}

// ---------------------------------------------------------------------------
// Bull — additional coverage
// ---------------------------------------------------------------------------

func TestBullQueueEvent(t *testing.T) {
	src := `emailQueue.on('completed', (job, result) => { console.log('done') })`
	ents := extract(t, "custom_js_bull", fi("worker.ts", "typescript", src))
	if !containsSubtype(ents, "queue_event") {
		t.Error("expected queue_event entity")
	}
}

func TestBullRepeatableJob(t *testing.T) {
	src := `emailQueue.add('daily-report', {}, { repeat: { cron: '0 9 * * *' } })`
	ents := extract(t, "custom_js_bull", fi("scheduler.ts", "typescript", src))
	if !containsSubtype(ents, "job") {
		t.Error("expected job entity with repeat")
	}
}

// ---------------------------------------------------------------------------
// Fastify — additional coverage
// ---------------------------------------------------------------------------

func TestFastifyDecorate(t *testing.T) {
	src := `fastify.decorate('authenticate', async function(request, reply) {})`
	ents := extract(t, "custom_js_fastify", fi("app.ts", "typescript", src))
	if !containsSubtype(ents, "decorator") {
		t.Error("expected decorator entity")
	}
}

func TestFastifyInstance(t *testing.T) {
	src := `const app = fastify({ logger: true })`
	ents := extract(t, "custom_js_fastify", fi("app.ts", "typescript", src))
	if !containsSubtype(ents, "fastify_instance") {
		t.Error("expected fastify_instance entity")
	}
}

// ---------------------------------------------------------------------------
// ORM migration_parsing — issue #2861
// ---------------------------------------------------------------------------

func TestTypeORMMigrationOps(t *testing.T) {
	src := `
export class CreateUsers1700000000000 implements MigrationInterface {
  public async up(queryRunner: QueryRunner): Promise<void> {
    await queryRunner.createTable(new Table({ name: "users", columns: [] }))
    await queryRunner.addColumn("users", new TableColumn({ name: "email" }))
    await queryRunner.createIndex("users", new TableIndex({ name: "idx_email" }))
  }
  public async down(queryRunner: QueryRunner): Promise<void> {
    await queryRunner.dropColumn("users", "email")
    await queryRunner.dropTable("users")
  }
}
`
	ents := extract(t, "custom_js_typeorm", fi("1700-create-users.ts", "typescript", src))
	if !containsSubtype(ents, "migration") {
		t.Error("expected migration class entity")
	}
	for _, st := range []string{"create_table", "add_column", "create_index", "drop_column", "drop_table"} {
		if !containsSubtype(ents, st) {
			t.Errorf("expected migration op subtype %q", st)
		}
	}
	if !containsEntity(ents, "SCOPE.Evolution", "create_table:users") {
		t.Error("expected create_table:users evolution entity with table name")
	}
}

func TestSequelizeMigrationOps(t *testing.T) {
	src := `
module.exports = {
  async up(queryInterface, Sequelize) {
    await queryInterface.createTable('Users', { id: { type: Sequelize.INTEGER } })
    await queryInterface.addColumn('Users', 'email', { type: Sequelize.STRING })
    await queryInterface.addIndex('Users', ['email'])
  },
  async down(queryInterface) {
    await queryInterface.removeColumn('Users', 'email')
    await queryInterface.dropTable('Users')
  }
}
`
	ents := extract(t, "custom_js_sequelize", fi("20240101-users.js", "javascript", src))
	for _, st := range []string{"create_table", "add_column", "create_index", "drop_column", "drop_table"} {
		if !containsSubtype(ents, st) {
			t.Errorf("expected migration op subtype %q", st)
		}
	}
	if !containsEntity(ents, "SCOPE.Evolution", "create_table:Users") {
		t.Error("expected create_table:Users evolution entity")
	}
}

func TestPrismaMigrationSQL(t *testing.T) {
	src := `
CREATE TABLE "User" (
    "id" SERIAL NOT NULL,
    "email" TEXT NOT NULL
);
ALTER TABLE "User" ADD COLUMN "name" TEXT;
CREATE UNIQUE INDEX "User_email_key" ON "User"("email");
`
	ents := extract(t, "custom_js_prisma", fi("prisma/migrations/20240101_init/migration.sql", "sql", src))
	if !containsEntity(ents, "SCOPE.Evolution", "create_table:User") {
		t.Error("expected create_table:User from prisma migration.sql")
	}
	if !containsSubtype(ents, "add_column") {
		t.Error("expected add_column from ALTER TABLE")
	}
	if !containsSubtype(ents, "create_index") {
		t.Error("expected create_index from CREATE UNIQUE INDEX")
	}
}

func TestPrismaMigrationSQLNotTriggeredOnAppCode(t *testing.T) {
	// A regular TS file with an embedded SQL string must NOT yield migration ops.
	src := "const q = `CREATE TABLE foo (id int)`"
	ents := extract(t, "custom_js_prisma", fi("src/db.ts", "typescript", src))
	if containsSubtype(ents, "create_table") {
		t.Error("CREATE TABLE in app TS should not be a prisma migration op")
	}
}

func TestDrizzleModel(t *testing.T) {
	src := `
import { pgTable, serial, text } from 'drizzle-orm/pg-core'
export const users = pgTable("users", {
  id: serial("id").primaryKey(),
  email: text("email").notNull(),
})
export const usersRelations = relations(users, ({ many }) => ({ posts: many(posts) }))
`
	ents := extract(t, "custom_js_drizzle", fi("schema.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Schema", "users") {
		t.Error("expected users table model")
	}
	if !containsSubtype(ents, "relation") {
		t.Error("expected drizzle relations entity")
	}
}

func TestDrizzleMigrationSQL(t *testing.T) {
	src := `
CREATE TABLE "users" (
	"id" serial PRIMARY KEY NOT NULL,
	"email" text NOT NULL
);
--> statement-breakpoint
ALTER TABLE "users" ADD COLUMN "name" text;
`
	ents := extract(t, "custom_js_drizzle", fi("drizzle/migrations/0000_init.sql", "sql", src))
	if !containsEntity(ents, "SCOPE.Evolution", "create_table:users") {
		t.Error("expected create_table:users from drizzle migration")
	}
	if !containsSubtype(ents, "add_column") {
		t.Error("expected add_column from drizzle ALTER TABLE")
	}
}

func TestKnexMigration(t *testing.T) {
	src := `
exports.up = function(knex) {
  return knex.schema.createTable('users', (t) => {
    t.increments('id')
    t.string('email')
    t.index(['email'])
  })
}
exports.down = function(knex) {
  return knex.schema.dropTable('users')
}
`
	ents := extract(t, "custom_js_knex", fi("20240101_users.js", "javascript", src))
	if !containsSubtype(ents, "migration") {
		t.Error("expected migration up/down entity")
	}
	if !containsEntity(ents, "SCOPE.Evolution", "create_table:users") {
		t.Error("expected create_table:users")
	}
	if !containsSubtype(ents, "drop_table") {
		t.Error("expected drop_table")
	}
	if !containsSubtype(ents, "add_column") {
		t.Error("expected add_column from column builders")
	}
}

func TestMikroORMModelAndMigration(t *testing.T) {
	model := `
@Entity()
export class User {
  @PrimaryKey()
  id!: number

  @Property()
  email!: string

  @ManyToOne(() => Org)
  org!: Org
}
`
	ents := extract(t, "custom_js_mikroorm", fi("user.entity.ts", "typescript", model))
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User entity")
	}
	if !containsSubtype(ents, "field") {
		t.Error("expected @Property field entity")
	}
	if !containsSubtype(ents, "relation") {
		t.Error("expected @ManyToOne relation entity")
	}

	mig := `
import { Migration } from '@mikro-orm/migrations'
export class Migration20240101 extends Migration {
  async up(): Promise<void> {
    this.addSql('CREATE TABLE "user" ("id" serial primary key, "email" varchar);')
    this.addSql('ALTER TABLE "user" ADD COLUMN "name" varchar;')
  }
}
`
	ments := extract(t, "custom_js_mikroorm", fi("Migration20240101.ts", "typescript", mig))
	if !containsSubtype(ments, "migration") {
		t.Error("expected migration class entity")
	}
	if !containsEntity(ments, "SCOPE.Evolution", "create_table:user") {
		t.Error("expected create_table:user from addSql")
	}
	if !containsSubtype(ments, "add_column") {
		t.Error("expected add_column from addSql ALTER TABLE")
	}
}

func TestObjectionModelAndMigration(t *testing.T) {
	model := `
const { Model } = require('objection')
class Person extends Model {
  static get tableName() { return 'persons' }
  static get jsonSchema() { return { type: 'object', properties: { name: { type: 'string' } } } }
  static get relationMappings() {
    return {
      pets: { relation: Model.HasManyRelation, modelClass: Pet },
    }
  }
}
`
	ents := extract(t, "custom_js_objection", fi("person.model.js", "javascript", model))
	if !containsEntity(ents, "SCOPE.Schema", "Person") {
		t.Error("expected Person model")
	}
	if !containsSubtype(ents, "json_schema") {
		t.Error("expected jsonSchema entity")
	}
	if !containsSubtype(ents, "relation_mappings") {
		t.Error("expected relationMappings entity")
	}
	if !containsSubtype(ents, "relation") {
		t.Error("expected individual relation entity")
	}

	mig := `
exports.up = (knex) => knex.schema.createTable('persons', (t) => { t.increments('id') })
exports.down = (knex) => knex.schema.dropTable('persons')
`
	ments := extract(t, "custom_js_objection", fi("20240101_persons.js", "javascript", mig))
	if !containsEntity(ments, "SCOPE.Evolution", "create_table:persons") {
		t.Error("expected create_table:persons from objection/knex migration")
	}
	if !containsSubtype(ments, "drop_table") {
		t.Error("expected drop_table")
	}
}

// ---------------------------------------------------------------------------
// Wrong language guard — one per key extractor
// ---------------------------------------------------------------------------

func TestWrongLanguageGuards(t *testing.T) {
	cases := []struct {
		name string
		lang string
	}{
		{"custom_js_express", "go"},
		{"custom_js_nestjs", "python"},
		{"custom_js_react", "java"},
		{"custom_js_vue", "ruby"},
	}
	for _, tc := range cases {
		t.Run(tc.name+"_"+tc.lang, func(t *testing.T) {
			src := "const x = 1;"
			ents := extract(t, tc.name, fi("f.ts", tc.lang, src))
			if len(ents) != 0 {
				t.Errorf("%s with lang=%s: expected no entities, got %d", tc.name, tc.lang, len(ents))
			}
		})
	}
}
