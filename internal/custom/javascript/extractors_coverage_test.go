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

// ---------------------------------------------------------------------------
// Angular — additional coverage
// ---------------------------------------------------------------------------

func TestAngularDirective(t *testing.T) {
	src := `
@Directive({ selector: '[appHighlight]' })
export class HighlightDirective {}
`
	ents := extract(t, "custom_js_angular", fi("highlight.directive.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "HighlightDirective") {
		t.Error("expected HighlightDirective directive")
	}
}

func TestAngularPipe(t *testing.T) {
	src := `
@Pipe({ name: 'titleCase' })
export class TitleCasePipe {}
`
	ents := extract(t, "custom_js_angular", fi("title-case.pipe.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "TitleCasePipe") {
		t.Error("expected TitleCasePipe pipe entity")
	}
}

func TestAngularInputOutput(t *testing.T) {
	src := `
@Input() title: string
@Output() titleChange = new EventEmitter<string>()
`
	ents := extract(t, "custom_js_angular", fi("comp.ts", "typescript", src))
	if !containsSubtype(ents, "input_property") {
		t.Error("expected input_property entity")
	}
	if !containsSubtype(ents, "output_property") {
		t.Error("expected output_property entity")
	}
}

func TestAngularGuard(t *testing.T) {
	src := `
export class AuthGuard implements CanActivate {
  canActivate() { return this.authService.isLoggedIn() }
}
`
	ents := extract(t, "custom_js_angular", fi("auth.guard.ts", "typescript", src))
	if !containsSubtype(ents, "guard") {
		t.Error("expected guard entity")
	}
}

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
// Wrong language guard — one per key extractor
// ---------------------------------------------------------------------------

func TestWrongLanguageGuards(t *testing.T) {
	cases := []struct {
		name string
		lang string
	}{
		{"custom_js_express", "go"},
		{"custom_js_nestjs", "python"},
		{"custom_js_angular", "rust"},
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
