package dart_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

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
	if !containsEntity(ents, "SCOPE.Operation", "go_route:/profile/:id") {
		t.Error("expected go_route:/profile/:id route")
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
