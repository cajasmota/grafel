package javascript_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	// Blank import to trigger init() registrations.
	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
)

// helper builds a FileInput with the given source.
func fi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

// extract runs the named extractor and returns entities.
func extract(t *testing.T, name string, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []entitySummary
	for _, ent := range ents {
		out = append(out, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
	}
	return out
}

type entitySummary struct{ Kind, Subtype, Name string }

func containsEntity(ents []entitySummary, kind, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

func containsSubtype(ents []entitySummary, subtype string) bool {
	for _, e := range ents {
		if e.Subtype == subtype {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Express
// ---------------------------------------------------------------------------

func TestExpressRoutes(t *testing.T) {
	src := `
app.get('/users', listUsers)
app.post('/users', createUser)
app.put('/users/:id', updateUser)
`
	ents := extract(t, "custom_js_express", fi("routes.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users route")
	}
}

func TestExpressRouter(t *testing.T) {
	src := `
const apiRouter = express.Router()
app.use('/api', apiRouter)
`
	ents := extract(t, "custom_js_express", fi("app.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "apiRouter") {
		t.Error("expected apiRouter component")
	}
}

func TestExpressMiddleware(t *testing.T) {
	src := `app.use(express.json())`
	ents := extract(t, "custom_js_express", fi("app.js", "javascript", src))
	if len(ents) == 0 {
		t.Error("expected at least one entity")
	}
}

func TestExpressPassport(t *testing.T) {
	src := `passport.use(new LocalStrategy(opts, verify))`
	ents := extract(t, "custom_js_express", fi("auth.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Pattern", "passport:LocalStrategy") {
		t.Error("expected passport:LocalStrategy entity")
	}
}

func TestExpressNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_express", fi("empty.ts", "typescript", "// no express code"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// NestJS
// ---------------------------------------------------------------------------

func TestNestJSController(t *testing.T) {
	src := `
@Controller('users')
export class UsersController {}
`
	ents := extract(t, "custom_js_nestjs", fi("users.controller.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "UsersController") {
		t.Error("expected UsersController component")
	}
}

func TestNestJSHTTPMethod(t *testing.T) {
	src := `
@Get('profile')
async getProfile() {}

@Post('create')
async createUser() {}
`
	ents := extract(t, "custom_js_nestjs", fi("users.controller.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET getProfile") {
		t.Error("expected GET getProfile endpoint")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST createUser") {
		t.Error("expected POST createUser endpoint")
	}
}

func TestNestJSModule(t *testing.T) {
	src := `
@Module({ controllers: [UsersController], providers: [UsersService] })
export class UsersModule {}
`
	ents := extract(t, "custom_js_nestjs", fi("users.module.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Pattern", "UsersModule") {
		t.Error("expected UsersModule pattern")
	}
}

func TestNestJSCron(t *testing.T) {
	src := `
@Cron('0 * * * *')
async runHourly() {}
`
	ents := extract(t, "custom_js_nestjs", fi("sched.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "cron:runHourly") {
		t.Error("expected cron:runHourly job")
	}
}

func TestNestJSNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_nestjs", fi("plain.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Angular
// ---------------------------------------------------------------------------

func TestAngularComponent(t *testing.T) {
	src := `
@Component({ selector: 'app-root', templateUrl: './app.component.html' })
export class AppComponent {}
`
	ents := extract(t, "custom_js_angular", fi("app.component.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "AppComponent") {
		t.Error("expected AppComponent UIComponent")
	}
}

func TestAngularNgModule(t *testing.T) {
	src := `
@NgModule({ declarations: [AppComponent], imports: [RouterModule] })
export class AppModule {}
`
	ents := extract(t, "custom_js_angular", fi("app.module.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Pattern", "AppModule") {
		t.Error("expected AppModule pattern")
	}
}

func TestAngularService(t *testing.T) {
	src := `
@Injectable({ providedIn: 'root' })
export class UserService {}
`
	ents := extract(t, "custom_js_angular", fi("user.service.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "UserService") {
		t.Error("expected UserService component")
	}
}

func TestAngularNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_angular", fi("plain.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Bull
// ---------------------------------------------------------------------------

func TestBullQueue(t *testing.T) {
	src := `const emailQueue = new Queue('emails', { connection })`
	ents := extract(t, "custom_js_bull", fi("queue.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Service", "queue:emails") {
		t.Error("expected queue:emails service")
	}
}

func TestBullWorker(t *testing.T) {
	src := `const worker = new Worker('emails', async (job) => {})`
	ents := extract(t, "custom_js_bull", fi("worker.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Service", "worker:emails") {
		t.Error("expected worker:emails service")
	}
}

func TestBullJobAdd(t *testing.T) {
	src := `emailQueue.add('send-welcome', { userId: 1 })`
	ents := extract(t, "custom_js_bull", fi("producer.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "job:send-welcome") {
		t.Error("expected job:send-welcome operation")
	}
}

func TestBullNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_bull", fi("plain.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Fastify
// ---------------------------------------------------------------------------

func TestFastifyRoute(t *testing.T) {
	src := `fastify.get('/users', handler)`
	ents := extract(t, "custom_js_fastify", fi("routes.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
}

func TestFastifyPlugin(t *testing.T) {
	src := "fastify.register(require('@fastify/cors'), { origin: true })"
	ents := extract(t, "custom_js_fastify", fi("app.ts", "typescript", src))
	if !containsSubtype(ents, "plugin") {
		t.Error("expected plugin entity")
	}
}

func TestFastifyHook(t *testing.T) {
	src := `fastify.addHook('onRequest', async (req, reply) => {})`
	ents := extract(t, "custom_js_fastify", fi("hooks.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Pattern", "hook:onRequest") {
		t.Error("expected hook:onRequest pattern")
	}
}

func TestFastifyNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_fastify", fi("plain.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Jest
// ---------------------------------------------------------------------------

func TestJestDescribe(t *testing.T) {
	src := `
describe('UserService', () => {
  it('should create a user', () => {})
  test('should find by id', async () => {})
})
`
	ents := extract(t, "custom_js_jest", fi("user.spec.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "UserService") {
		t.Error("expected UserService test_suite")
	}
	if !containsEntity(ents, "SCOPE.Operation", "should create a user") {
		t.Error("expected 'should create a user' test_case")
	}
}

func TestJestHooks(t *testing.T) {
	src := `
beforeEach(() => { db.clear() })
afterAll(() => { db.close() })
`
	ents := extract(t, "custom_js_jest", fi("setup.spec.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Pattern", "beforeEach") {
		t.Error("expected beforeEach hook")
	}
}

func TestJestMock(t *testing.T) {
	src := `jest.mock('./service')`
	ents := extract(t, "custom_js_jest", fi("mock.spec.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Pattern", "jest.mock") {
		t.Error("expected jest.mock entity")
	}
}

func TestJestNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_jest", fi("plain.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// LangChain
// ---------------------------------------------------------------------------

func TestLangchainLLM(t *testing.T) {
	src := `const llm = new ChatOpenAI({ modelName: 'gpt-4' })`
	ents := extract(t, "custom_js_langchain", fi("chain.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "ChatOpenAI") {
		t.Error("expected ChatOpenAI LLM entity")
	}
}

func TestLangchainChain(t *testing.T) {
	src := `const chain = new LLMChain({ llm, prompt })`
	ents := extract(t, "custom_js_langchain", fi("chain.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "LLMChain") {
		t.Error("expected LLMChain entity")
	}
}

func TestLangchainPipe(t *testing.T) {
	src := `const chain = prompt.pipe(llm).pipe(parser)`
	ents := extract(t, "custom_js_langchain", fi("chain.ts", "typescript", src))
	if !containsSubtype(ents, "pipe_stage") {
		t.Error("expected pipe_stage entity")
	}
}

func TestLangchainNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_langchain", fi("plain.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Mongoose
// ---------------------------------------------------------------------------

func TestMongooseSchema(t *testing.T) {
	src := `const UserSchema = new Schema({ name: String, email: String })`
	ents := extract(t, "custom_js_mongoose", fi("user.model.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Schema", "UserSchema") {
		t.Error("expected UserSchema entity")
	}
}

func TestMongooseModel(t *testing.T) {
	src := `const User = mongoose.model('User', UserSchema)`
	ents := extract(t, "custom_js_mongoose", fi("user.model.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User model entity")
	}
}

func TestMongooseHook(t *testing.T) {
	src := `UserSchema.pre('save', async function() { this.updatedAt = Date.now() })`
	ents := extract(t, "custom_js_mongoose", fi("user.model.ts", "typescript", src))
	if !containsSubtype(ents, "middleware") {
		t.Error("expected middleware entity")
	}
}

func TestMongooseNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_mongoose", fi("plain.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Next.js
// ---------------------------------------------------------------------------

func TestNextjsHTTPHandler(t *testing.T) {
	src := `
export async function GET(request: Request) { return Response.json({}) }
export async function POST(request: Request) { return Response.json({}) }
`
	ents := extract(t, "custom_js_nextjs", fi("app/api/users/route.ts", "typescript", src))
	if !containsSubtype(ents, "endpoint") {
		t.Error("expected endpoint entity")
	}
}

func TestNextjsServerAction(t *testing.T) {
	src := `
'use server'
export async function createUser(data: FormData) {}
`
	ents := extract(t, "custom_js_nextjs", fi("app/users/actions.ts", "typescript", src))
	if !containsSubtype(ents, "server_action") {
		t.Error("expected server_action entity")
	}
}

func TestNextjsDataFetcher(t *testing.T) {
	src := `export async function getServerSideProps(ctx) { return { props: {} } }`
	ents := extract(t, "custom_js_nextjs", fi("pages/users.tsx", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "getServerSideProps") {
		t.Error("expected getServerSideProps entity")
	}
}

func TestNextjsNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_nextjs", fi("lib/utils.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Nuxt
// ---------------------------------------------------------------------------

func TestNuxtServerHandler(t *testing.T) {
	src := `export default defineEventHandler(async (event) => { return { ok: true } })`
	ents := extract(t, "custom_js_nuxt", fi("server/api/users.get.ts", "typescript", src))
	if len(ents) == 0 {
		t.Error("expected at least one entity from Nuxt server handler")
	}
}

func TestNuxtComposable(t *testing.T) {
	src := `export const useUser = () => { const user = ref(null); return { user } }`
	ents := extract(t, "custom_js_nuxt", fi("composables/useUser.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "useUser") {
		t.Error("expected useUser composable")
	}
}

func TestNuxtPlugin(t *testing.T) {
	src := `export default defineNuxtPlugin((nuxtApp) => { nuxtApp.provide('toast', toast) })`
	ents := extract(t, "custom_js_nuxt", fi("plugins/toast.ts", "typescript", src))
	if !containsSubtype(ents, "plugin") {
		t.Error("expected plugin entity")
	}
}

func TestNuxtNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_nuxt", fi("utils/helpers.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Prisma
// ---------------------------------------------------------------------------

func TestPrismaModel(t *testing.T) {
	src := `
model User {
  id    Int    @id
  email String @unique
}
`
	ents := extract(t, "custom_js_prisma", fi("schema.prisma", "prisma", src))
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User model entity")
	}
}

func TestPrismaEnum(t *testing.T) {
	src := `
enum Role {
  ADMIN
  USER
}
`
	ents := extract(t, "custom_js_prisma", fi("schema.prisma", "prisma", src))
	if !containsEntity(ents, "SCOPE.Schema", "Role") {
		t.Error("expected Role enum entity")
	}
}

func TestPrismaClientCall(t *testing.T) {
	src := `const users = await prisma.user.findMany()`
	ents := extract(t, "custom_js_prisma", fi("users.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "user.findMany") {
		t.Error("expected user.findMany operation")
	}
}

func TestPrismaNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_prisma", fi("plain.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// React
// ---------------------------------------------------------------------------

func TestReactFunctionalComponent(t *testing.T) {
	src := `
export function UserCard({ user }: Props) {
  return <div>{user.name}</div>
}
`
	ents := extract(t, "custom_js_react", fi("UserCard.tsx", "typescript", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "UserCard") {
		t.Error("expected UserCard UIComponent")
	}
}

func TestReactHook(t *testing.T) {
	src := `export function useUser(id: string) { const [user, setUser] = useState(null); return user }`
	ents := extract(t, "custom_js_react", fi("useUser.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "useUser") {
		t.Error("expected useUser hook")
	}
}

func TestReactContext(t *testing.T) {
	src := `const ThemeContext = React.createContext('light')`
	ents := extract(t, "custom_js_react", fi("ThemeContext.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "ThemeContext") {
		t.Error("expected ThemeContext context")
	}
}

func TestReactNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_react", fi("plain.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Remix
// ---------------------------------------------------------------------------

func TestRemixLoader(t *testing.T) {
	src := `export async function loader({ request }: LoaderFunctionArgs) { return json({}) }`
	ents := extract(t, "custom_js_remix", fi("app/routes/users.tsx", "typescript", src))
	if !containsSubtype(ents, "loader") {
		t.Error("expected loader entity")
	}
}

func TestRemixAction(t *testing.T) {
	src := `export async function action({ request }: ActionFunctionArgs) { return redirect('/') }`
	ents := extract(t, "custom_js_remix", fi("app/routes/users.new.tsx", "typescript", src))
	if !containsSubtype(ents, "action") {
		t.Error("expected action entity")
	}
}

func TestRemixComponent(t *testing.T) {
	src := `export default function UsersPage() { return <h1>Users</h1> }`
	ents := extract(t, "custom_js_remix", fi("app/routes/users.tsx", "typescript", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "UsersPage") {
		t.Error("expected UsersPage route component")
	}
}

func TestRemixNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_remix", fi("app/utils/helpers.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Sequelize
// ---------------------------------------------------------------------------

func TestSequelizeDefine(t *testing.T) {
	src := `const User = sequelize.define('User', { name: DataTypes.STRING })`
	ents := extract(t, "custom_js_sequelize", fi("user.model.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User model entity")
	}
}

func TestSequelizeClassModel(t *testing.T) {
	src := `
class User extends Model {}
User.init({ name: DataTypes.STRING }, { sequelize })
`
	ents := extract(t, "custom_js_sequelize", fi("user.model.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User class model entity")
	}
}

func TestSequelizeAssociation(t *testing.T) {
	src := `User.hasMany(Post, { foreignKey: 'userId' })`
	ents := extract(t, "custom_js_sequelize", fi("associations.ts", "typescript", src))
	if !containsSubtype(ents, "association") {
		t.Error("expected association entity")
	}
}

func TestSequelizeNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_sequelize", fi("plain.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Svelte
// ---------------------------------------------------------------------------

func TestSvelteComponent(t *testing.T) {
	src := `<script lang="ts">
let name = 'world'
</script>
<h1>Hello {name}</h1>`
	ents := extract(t, "custom_js_svelte", fi("src/routes/+page.svelte", "typescript", src))
	if !containsSubtype(ents, "page") {
		t.Error("expected page entity")
	}
}

func TestSvelteHTTPHandler(t *testing.T) {
	src := `export async function GET({ url }) { return json({ ok: true }) }`
	ents := extract(t, "custom_js_svelte", fi("src/routes/api/users/+server.ts", "typescript", src))
	if !containsSubtype(ents, "endpoint") {
		t.Error("expected endpoint entity from svelte +server.ts")
	}
}

func TestSvelteLoad(t *testing.T) {
	src := `export const load = async ({ fetch }) => { const data = await fetch('/api'); return { data } }`
	ents := extract(t, "custom_js_svelte", fi("src/routes/users/+page.ts", "typescript", src))
	if !containsSubtype(ents, "loader") {
		t.Error("expected loader entity from svelte load function")
	}
}

func TestSvelteNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_svelte", fi("lib/utils.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// tRPC
// ---------------------------------------------------------------------------

func TestTRPCRouter(t *testing.T) {
	src := `const appRouter = t.router({ getUser: t.procedure.query(async () => {}) })`
	ents := extract(t, "custom_js_trpc", fi("router.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "appRouter") {
		t.Error("expected appRouter entity")
	}
}

func TestTRPCProcedure(t *testing.T) {
	src := `const getUser = t.procedure.query(async ({ input }) => { return db.user.findUnique(input) })`
	ents := extract(t, "custom_js_trpc", fi("router.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "getUser") {
		t.Error("expected getUser procedure")
	}
}

func TestTRPCInlineProcedure(t *testing.T) {
	src := `
const appRouter = t.router({
  createUser: t.procedure.mutation(async ({ input }) => {}),
})
`
	ents := extract(t, "custom_js_trpc", fi("router.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Operation", "createUser") {
		t.Error("expected createUser inline procedure")
	}
}

func TestTRPCNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_trpc", fi("plain.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// TypeORM
// ---------------------------------------------------------------------------

func TestTypeORMEntity(t *testing.T) {
	src := `
@Entity('users')
export class User {}
`
	ents := extract(t, "custom_js_typeorm", fi("user.entity.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User entity")
	}
}

func TestTypeORMRelation(t *testing.T) {
	src := `
@OneToMany(() => Post, post => post.user)
posts: Post[]
`
	ents := extract(t, "custom_js_typeorm", fi("user.entity.ts", "typescript", src))
	if !containsSubtype(ents, "relation") {
		t.Error("expected relation entity")
	}
}

func TestTypeORMMigration(t *testing.T) {
	src := `export class AddUserTable1234567890 implements MigrationInterface {}`
	ents := extract(t, "custom_js_typeorm", fi("migrations/1234_user.ts", "typescript", src))
	if !containsSubtype(ents, "migration") {
		t.Error("expected migration entity")
	}
}

func TestTypeORMNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_typeorm", fi("plain.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Vue
// ---------------------------------------------------------------------------

func TestVueSFCComponent(t *testing.T) {
	src := `<script setup lang="ts">
const props = defineProps<{ title: string }>()
</script>
<template><h1>{{ title }}</h1></template>`
	ents := extract(t, "custom_js_vue", fi("src/components/Header.vue", "typescript", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "Header") {
		t.Error("expected Header component")
	}
}

func TestVueComposable(t *testing.T) {
	src := `export const useCounter = () => { const count = ref(0); return { count } }`
	ents := extract(t, "custom_js_vue", fi("src/composables/useCounter.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "useCounter") {
		t.Error("expected useCounter composable")
	}
}

func TestVuePiniaStore(t *testing.T) {
	src := `export const useUserStore = defineStore('user', () => { const user = ref(null); return { user } })`
	ents := extract(t, "custom_js_vue", fi("src/stores/user.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "user") {
		t.Error("expected user pinia store")
	}
}

func TestVueNoMatch(t *testing.T) {
	ents := extract(t, "custom_js_vue", fi("lib/utils.ts", "typescript", "const x = 1;"))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Empty content guard — all extractors
// ---------------------------------------------------------------------------

func TestAllExtractorsEmptyContent(t *testing.T) {
	extractors := []string{
		"custom_js_express", "custom_js_nestjs", "custom_js_angular",
		"custom_js_bull", "custom_js_fastify", "custom_js_jest",
		"custom_js_langchain", "custom_js_mongoose", "custom_js_nextjs",
		"custom_js_nuxt", "custom_js_prisma", "custom_js_react",
		"custom_js_remix", "custom_js_sequelize", "custom_js_svelte",
		"custom_js_trpc", "custom_js_typeorm", "custom_js_vue",
	}
	for _, name := range extractors {
		t.Run(name, func(t *testing.T) {
			ents := extract(t, name, fi("empty.ts", "typescript", ""))
			if len(ents) != 0 {
				t.Errorf("%s: expected no entities for empty content, got %d", name, len(ents))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Registration check
// ---------------------------------------------------------------------------

func TestAllExtractorsRegistered(t *testing.T) {
	expected := []string{
		"custom_js_express", "custom_js_nestjs", "custom_js_angular",
		"custom_js_bull", "custom_js_fastify", "custom_js_jest",
		"custom_js_langchain", "custom_js_mongoose", "custom_js_nextjs",
		"custom_js_nuxt", "custom_js_prisma", "custom_js_react",
		"custom_js_remix", "custom_js_sequelize", "custom_js_svelte",
		"custom_js_trpc", "custom_js_typeorm", "custom_js_vue",
	}
	for _, name := range expected {
		if _, ok := extreg.Get(name); !ok {
			t.Errorf("extractor %q not registered", name)
		}
	}
}
