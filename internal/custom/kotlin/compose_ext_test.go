package kotlin_test

// ---------------------------------------------------------------------------
// Compose extension tests: screen_detection, state_management, state_setter,
// platform_branching (KMP expect/actual)
// ---------------------------------------------------------------------------

import (
	"testing"
)

func TestComposeScreenDetection(t *testing.T) {
	src := `
@Composable
fun HomeScreen(viewModel: HomeViewModel) {
    Text("Home")
}

@Composable
fun DetailScreen(id: Int) {
    Text("Detail $id")
}
`
	ents := extract(t, "custom_kotlin_compose", fi("Screens.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "HomeScreen") {
		t.Error("expected HomeScreen UIComponent")
	}
	if !containsEntity(ents, "SCOPE.UIComponent", "DetailScreen") {
		t.Error("expected DetailScreen UIComponent")
	}
	// Check screen subtype specifically
	foundScreen := false
	for _, e := range ents {
		if e.Kind == "SCOPE.UIComponent" && e.Subtype == "screen" && e.Name == "HomeScreen" {
			foundScreen = true
		}
	}
	if !foundScreen {
		t.Error("expected HomeScreen with subtype=screen")
	}
}

func TestComposeStateFlow(t *testing.T) {
	src := `
class HomeViewModel : ViewModel() {
    private val _uiState = MutableStateFlow<HomeUiState>(HomeUiState.Loading)
    val uiState: StateFlow<HomeUiState> = _uiState.asStateFlow()
}
`
	ents := extract(t, "custom_kotlin_compose", fi("HomeViewModel.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "state_management" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected state_management entity from StateFlow")
	}
}

func TestComposeMutableStateOf(t *testing.T) {
	src := `
@Composable
fun CounterScreen() {
    var count by remember { mutableStateOf(0) }
    Text("Count: $count")
}
`
	ents := extract(t, "custom_kotlin_compose", fi("Counter.kt", "kotlin", src))
	foundSetter := false
	for _, e := range ents {
		if e.Subtype == "state_setter" {
			foundSetter = true
		}
	}
	if !foundSetter {
		t.Error("expected state_setter entity from mutableStateOf")
	}
}

func TestComposeRemember(t *testing.T) {
	src := `
@Composable
fun SettingsScreen() {
    val navController = remember { NavController() }
    val state = rememberSaveable { SaveableState() }
}
`
	ents := extract(t, "custom_kotlin_compose", fi("Settings.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "state_management" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected state_management entity from remember{}")
	}
}

func TestComposeCollectAsState(t *testing.T) {
	src := `
@Composable
fun FeedScreen(viewModel: FeedViewModel) {
    val uiState by viewModel.uiState.collectAsState()
    LazyColumn {  }
}
`
	ents := extract(t, "custom_kotlin_compose", fi("Feed.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "state_management" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected state_management entity from collectAsState()")
	}
}

func TestKmpExpectActual(t *testing.T) {
	expectSrc := `
package com.example

expect fun getPlatformName(): String

expect class PlatformContext {
    fun getSystemProperty(key: String): String?
}
`
	actualSrc := `
package com.example

actual fun getPlatformName(): String = "Android"

actual class PlatformContext {
    actual fun getSystemProperty(key: String): String? = System.getProperty(key)
}
`
	expectEnts := extract(t, "custom_kotlin_compose", fi("Platform.kt", "kotlin", expectSrc))
	actualEnts := extract(t, "custom_kotlin_compose", fi("PlatformAndroid.kt", "kotlin", actualSrc))

	foundExpect := false
	for _, e := range expectEnts {
		if e.Subtype == "platform_branching" {
			foundExpect = true
			break
		}
	}
	if !foundExpect {
		t.Error("expected platform_branching entity from expect declaration")
	}

	foundActual := false
	for _, e := range actualEnts {
		if e.Subtype == "platform_branching" {
			foundActual = true
			break
		}
	}
	if !foundActual {
		t.Error("expected platform_branching entity from actual declaration")
	}
}

func TestKmpExpectFunction(t *testing.T) {
	src := `
expect fun httpClient(): HttpClient
`
	ents := extract(t, "custom_kotlin_compose", fi("HttpClient.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Pattern", "expect:httpClient") {
		t.Error("expected platform_branching entity for expect:httpClient")
	}
}

func TestKmpActualFunction(t *testing.T) {
	src := `
actual fun httpClient(): HttpClient = OkHttpClient()
`
	ents := extract(t, "custom_kotlin_compose", fi("HttpClientAndroid.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Pattern", "actual:httpClient") {
		t.Error("expected platform_branching entity for actual:httpClient")
	}
}
