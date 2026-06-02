package dart_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/dart"
)

func fi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

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

// ---------------------------------------------------------------------------
// Flutter
// ---------------------------------------------------------------------------

func TestFlutterStatelessWidget(t *testing.T) {
	src := `
class UserCard extends StatelessWidget {
  @override
  Widget build(BuildContext context) => Card();
}
`
	ents := extract(t, "custom_dart_flutter", fi("user_card.dart", "dart", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "UserCard") {
		t.Error("expected UserCard UIComponent")
	}
}

func TestFlutterStatefulWidget(t *testing.T) {
	src := `
class CounterPage extends StatefulWidget {
  @override
  State<CounterPage> createState() => _CounterPageState();
}
`
	ents := extract(t, "custom_dart_flutter", fi("counter.dart", "dart", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "CounterPage") {
		t.Error("expected CounterPage UIComponent")
	}
}

func TestFlutterBloc(t *testing.T) {
	src := `
class AuthBloc extends Bloc<AuthEvent, AuthState> {
  AuthBloc() : super(AuthInitial());
}
`
	ents := extract(t, "custom_dart_flutter", fi("auth_bloc.dart", "dart", src))
	if !containsEntity(ents, "SCOPE.Pattern", "AuthBloc") {
		t.Error("expected AuthBloc Pattern (bloc)")
	}
}

func TestFlutterCubit(t *testing.T) {
	src := `
class CounterCubit extends Cubit<int> {
  CounterCubit() : super(0);
  void increment() => emit(state + 1);
}
`
	ents := extract(t, "custom_dart_flutter", fi("counter_cubit.dart", "dart", src))
	if !containsEntity(ents, "SCOPE.Pattern", "CounterCubit") {
		t.Error("expected CounterCubit Pattern (cubit)")
	}
}

func TestFlutterGoRoute(t *testing.T) {
	src := `
GoRoute(
  path: '/home',
  builder: (context, state) => const HomeScreen(),
),
GoRoute(
  path: '/profile/:id',
  builder: (context, state) => ProfileScreen(id: state.params['id']!),
),
`
	ents := extract(t, "custom_dart_flutter", fi("router.dart", "dart", src))
	// GoRoute entity name = "go_route:" + path
	if !containsEntity(ents, "SCOPE.Operation", "go_route:/home") {
		t.Error("expected go_route:/home route")
	}
	// #3578: path params are normalized (:id -> {id}) for stable route stubs.
	if !containsEntity(ents, "SCOPE.Operation", "go_route:/profile/{id}") {
		t.Error("expected go_route:/profile/{id} route")
	}
}

func TestFlutterNavigatorPushNamed(t *testing.T) {
	src := `Navigator.pushNamed(context, '/settings');`
	ents := extract(t, "custom_dart_flutter", fi("nav.dart", "dart", src))
	// pushNamed entity name = "route:" + path
	if !containsEntity(ents, "SCOPE.Operation", "route:/settings") {
		t.Error("expected route:/settings from pushNamed")
	}
}

func TestFlutterChangeNotifier(t *testing.T) {
	src := `
class CartModel extends ChangeNotifier {
  final List<Item> _items = [];
  void add(Item item) { _items.add(item); notifyListeners(); }
}
`
	ents := extract(t, "custom_dart_flutter", fi("cart_model.dart", "dart", src))
	if !containsEntity(ents, "SCOPE.Pattern", "CartModel") {
		t.Error("expected CartModel ChangeNotifier pattern")
	}
}

func TestFlutterNoMatch(t *testing.T) {
	src := `void main() => runApp(MyApp());`
	ents := extract(t, "custom_dart_flutter", fi("main.dart", "dart", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Flutter EDGES (#3578) — NAVIGATES_TO + USES
// ---------------------------------------------------------------------------

// edgeSummary captures an embedded relationship anchored on a host entity.
type edgeSummary struct {
	From, To, Kind, BindKind, NavKind, Target, Unresolved string
}

func extractEdges(t *testing.T, name string, file extreg.FileInput) []edgeSummary {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []edgeSummary
	for _, ent := range ents {
		for _, r := range ent.Relationships {
			es := edgeSummary{From: r.FromID, To: r.ToID, Kind: r.Kind}
			if r.Properties != nil {
				es.BindKind = r.Properties["bind_kind"]
				es.NavKind = r.Properties["nav_kind"]
				es.Target = r.Properties["target"]
				es.Unresolved = r.Properties["unresolved"]
			}
			out = append(out, es)
		}
	}
	return out
}

func hasEdge(edges []edgeSummary, from, to, kind string) bool {
	for _, e := range edges {
		if e.From == from && e.To == to && e.Kind == kind {
			return true
		}
	}
	return false
}

func TestFlutterNavigatesToNamedRoute(t *testing.T) {
	src := `
class HomeScreen extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return ElevatedButton(
      onPressed: () => Navigator.pushNamed(context, '/detail/:id'),
      child: Text('go'),
    );
  }
}
`
	edges := extractEdges(t, "custom_dart_flutter", fi("home.dart", "dart", src))
	// :id normalized to {id}; HomeScreen NAVIGATES_TO route:/detail/{id}
	if !hasEdge(edges, "HomeScreen", "route:/detail/{id}", "NAVIGATES_TO") {
		t.Fatalf("expected HomeScreen NAVIGATES_TO route:/detail/{id}; got %+v", edges)
	}
}

func TestFlutterNavigatesToWidgetViaMaterialPageRoute(t *testing.T) {
	src := `
class HomeScreen extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: () => Navigator.push(
        context,
        MaterialPageRoute(builder: (_) => const DetailScreen()),
      ),
    );
  }
}
`
	edges := extractEdges(t, "custom_dart_flutter", fi("home.dart", "dart", src))
	if !hasEdge(edges, "HomeScreen", "DetailScreen", "NAVIGATES_TO") {
		t.Fatalf("expected HomeScreen NAVIGATES_TO DetailScreen (MaterialPageRoute); got %+v", edges)
	}
}

func TestFlutterNavigatesToGoRouterImperative(t *testing.T) {
	src := `
class ProfileButton extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return TextButton(onPressed: () => context.go('/detail'), child: Text('x'));
  }
}
`
	edges := extractEdges(t, "custom_dart_flutter", fi("profile.dart", "dart", src))
	if !hasEdge(edges, "ProfileButton", "route:/detail", "NAVIGATES_TO") {
		t.Fatalf("expected ProfileButton NAVIGATES_TO route:/detail (context.go); got %+v", edges)
	}
}

func TestFlutterGoRouteWiresRouteToScreen(t *testing.T) {
	src := `
final router = GoRouter(routes: [
  GoRoute(path: '/profile/:id', builder: (context, state) => ProfileScreen()),
]);
`
	edges := extractEdges(t, "custom_dart_flutter", fi("router.dart", "dart", src))
	// route stub → screen widget (route→screen wiring), normalized path.
	if !hasEdge(edges, "go_route:/profile/{id}", "ProfileScreen", "NAVIGATES_TO") {
		t.Fatalf("expected go_route:/profile/{id} NAVIGATES_TO ProfileScreen; got %+v", edges)
	}
}

func TestFlutterUsesBlocViaContextRead(t *testing.T) {
	src := `
class ProfileScreen extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final bloc = context.read<ProfileBloc>();
    return Container();
  }
}
`
	edges := extractEdges(t, "custom_dart_flutter", fi("profile.dart", "dart", src))
	if !hasEdge(edges, "ProfileScreen", "ProfileBloc", "USES") {
		t.Fatalf("expected ProfileScreen USES ProfileBloc (context.read); got %+v", edges)
	}
}

func TestFlutterUsesModelViaProvider(t *testing.T) {
	src := `
class CartWidget extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final cart = Provider.of<CartModel>(context);
    return Text('${cart.count}');
  }
}
`
	edges := extractEdges(t, "custom_dart_flutter", fi("cart.dart", "dart", src))
	if !hasEdge(edges, "CartWidget", "CartModel", "USES") {
		t.Fatalf("expected CartWidget USES CartModel (Provider.of); got %+v", edges)
	}
}

func TestFlutterUsesProviderViaRiverpod(t *testing.T) {
	src := `
class CounterView extends ConsumerWidget {
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final count = ref.watch(counterProvider);
    return Text('$count');
  }
}
`
	edges := extractEdges(t, "custom_dart_flutter", fi("counter.dart", "dart", src))
	// ConsumerWidget isn't a Stateless/Stateful widget; host class still
	// tracked via class-span. Riverpod ref.watch -> USES counterProvider.
	if !hasEdge(edges, "CounterView", "counterProvider", "USES") {
		t.Fatalf("expected CounterView USES counterProvider (ref.watch); got %+v", edges)
	}
}

// Negative test: two sibling widgets in one file must NOT leak each other's
// navigation/binding edges across screen boundaries.
func TestFlutterNoCrossScreenEdgeLeak(t *testing.T) {
	src := `
class ScreenA extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return TextButton(onPressed: () => context.go('/a'), child: Text('a'));
  }
}

class ScreenB extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final bloc = context.read<BBloc>();
    return Container();
  }
}
`
	edges := extractEdges(t, "custom_dart_flutter", fi("screens.dart", "dart", src))
	// Correct attribution.
	if !hasEdge(edges, "ScreenA", "route:/a", "NAVIGATES_TO") {
		t.Errorf("expected ScreenA NAVIGATES_TO route:/a; got %+v", edges)
	}
	if !hasEdge(edges, "ScreenB", "BBloc", "USES") {
		t.Errorf("expected ScreenB USES BBloc; got %+v", edges)
	}
	// No leakage: ScreenA must not own BBloc, ScreenB must not own /a.
	if hasEdge(edges, "ScreenA", "BBloc", "USES") {
		t.Errorf("leak: ScreenA must not USES BBloc; got %+v", edges)
	}
	if hasEdge(edges, "ScreenB", "route:/a", "NAVIGATES_TO") {
		t.Errorf("leak: ScreenB must not NAVIGATES_TO route:/a; got %+v", edges)
	}
}

// Ensure NAVIGATES_TO / USES are valid relationship kinds in the taxonomy.
func TestFlutterEdgeKindsValid(t *testing.T) {
	if !types.IsValidRelationshipKind("NAVIGATES_TO") {
		t.Error("NAVIGATES_TO not a valid relationship kind")
	}
	if !types.IsValidRelationshipKind("USES") {
		t.Error("USES not a valid relationship kind")
	}
}
