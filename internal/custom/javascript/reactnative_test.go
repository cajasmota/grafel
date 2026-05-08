package javascript_test

// Tests for the React Native component hierarchy and navigation route extractor
// (MX-1063). All tests run against the real extractor registered as
// "custom_js_react_native". No mocks.

import (
	"context"
	"os"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	// Blank import to trigger init() registrations.
	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
)

// extractFull runs the named extractor and returns full EntityRecord slice.
func extractFull(t *testing.T, name string, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

// containsFullEntity returns the first matching EntityRecord (or nil).
func findEntity(ents []types.EntityRecord, kind, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func countBySubtype(ents []types.EntityRecord, subtype string) int {
	n := 0
	for _, e := range ents {
		if e.Subtype == subtype {
			n++
		}
	}
	return n
}

// checkMeta asserts a string value in the Metadata map.
func checkMetadata(t *testing.T, meta map[string]interface{}, key, want string) {
	t.Helper()
	v, ok := meta[key]
	if !ok {
		t.Errorf("metadata missing key %q", key)
		return
	}
	if got, ok2 := v.(string); !ok2 || got != want {
		t.Errorf("metadata[%q] = %v (%T), want %q", key, v, v, want)
	}
}

// rnFixture reads a fixture file relative to this package.
func rnFixture(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../fixtures/sources/" + rel)
	if err != nil {
		t.Fatalf("rnFixture %q: %v", rel, err)
	}
	return data
}

// ---------------------------------------------------------------------------
// Navigation route extraction — Stack.Screen
// ---------------------------------------------------------------------------

func TestRNStackScreenRoutes(t *testing.T) {
	src := `
import { NavigationContainer } from '@react-navigation/native';
import { createStackNavigator } from '@react-navigation/stack';

const Stack = createStackNavigator();

export default function App() {
  return (
    <NavigationContainer>
      <Stack.Navigator>
        <Stack.Screen name="Home" component={HomeScreen} />
        <Stack.Screen name="Profile" component={ProfileScreen} />
        <Stack.Screen name="Settings" component={SettingsScreen} />
      </Stack.Navigator>
    </NavigationContainer>
  );
}
`
	ents := extractFull(t, "custom_js_react_native", fi("App.tsx", "typescript", src))

	for _, want := range []string{"route:Stack:Home", "route:Stack:Profile", "route:Stack:Settings"} {
		if findEntity(ents, "SCOPE.Operation", want) == nil {
			t.Errorf("expected entity SCOPE.Operation/%s", want)
		}
	}
}

func TestRNStackScreenCount(t *testing.T) {
	src := `
import { createStackNavigator } from '@react-navigation/stack';
const Stack = createStackNavigator();
export default function App() {
  return (
    <Stack.Navigator>
      <Stack.Screen name="A" component={AScreen} />
      <Stack.Screen name="B" component={BScreen} />
      <Stack.Screen name="C" component={CScreen} />
    </Stack.Navigator>
  );
}
`
	ents := extractFull(t, "custom_js_react_native", fi("App.tsx", "typescript", src))
	routeCount := countBySubtype(ents, "route")
	if routeCount != 3 {
		t.Errorf("expected 3 stack routes, got %d", routeCount)
	}
}

// ---------------------------------------------------------------------------
// Navigation route extraction — Tab.Screen
// ---------------------------------------------------------------------------

func TestRNTabScreenRoutes(t *testing.T) {
	src := `
import { createBottomTabNavigator } from '@react-navigation/bottom-tabs';
const Tab = createBottomTabNavigator();
export default function TabNavigator() {
  return (
    <Tab.Navigator>
      <Tab.Screen name="Feed" component={FeedScreen} />
      <Tab.Screen name="Messages" component={MessagesScreen} />
    </Tab.Navigator>
  );
}
`
	ents := extractFull(t, "custom_js_react_native", fi("TabNav.tsx", "typescript", src))

	if findEntity(ents, "SCOPE.Operation", "route:Tab:Feed") == nil {
		t.Error("expected route:Tab:Feed entity")
	}
	if findEntity(ents, "SCOPE.Operation", "route:Tab:Messages") == nil {
		t.Error("expected route:Tab:Messages entity")
	}
}

// ---------------------------------------------------------------------------
// Navigation route extraction — Drawer.Screen
// ---------------------------------------------------------------------------

func TestRNDrawerScreenRoutes(t *testing.T) {
	src := `
import { createDrawerNavigator } from '@react-navigation/drawer';
const Drawer = createDrawerNavigator();
export default function DrawerNav() {
  return (
    <Drawer.Navigator>
      <Drawer.Screen name="Dashboard" component={DashboardScreen} />
      <Drawer.Screen name="Profile" component={ProfileScreen} />
    </Drawer.Navigator>
  );
}
`
	ents := extractFull(t, "custom_js_react_native", fi("DrawerNav.tsx", "typescript", src))

	if findEntity(ents, "SCOPE.Operation", "route:Drawer:Dashboard") == nil {
		t.Error("expected route:Drawer:Dashboard entity")
	}
	if findEntity(ents, "SCOPE.Operation", "route:Drawer:Profile") == nil {
		t.Error("expected route:Drawer:Profile entity")
	}
}

// ---------------------------------------------------------------------------
// Route metadata: route_type, route_name, component
// ---------------------------------------------------------------------------

func TestRNRouteMetadataFields(t *testing.T) {
	src := `
import { createStackNavigator } from '@react-navigation/stack';
const Stack = createStackNavigator();
export default function App() {
  return (
    <Stack.Navigator>
      <Stack.Screen name="Home" component={HomeScreen} />
    </Stack.Navigator>
  );
}
`
	ents := extractFull(t, "custom_js_react_native", fi("App.tsx", "typescript", src))
	route := findEntity(ents, "SCOPE.Operation", "route:Stack:Home")
	if route == nil {
		t.Fatal("route:Stack:Home entity not found")
	}
	if route.Metadata == nil {
		t.Fatal("route:Stack:Home has nil metadata")
	}

	checkMetadata(t, route.Metadata, "route_type", "stack")
	checkMetadata(t, route.Metadata, "route_name", "Home")
	checkMetadata(t, route.Metadata, "component", "HomeScreen")
}

// ---------------------------------------------------------------------------
// Component hierarchy — HomeScreen has 3 children
// ---------------------------------------------------------------------------

func TestRNHomeScreenHierarchy(t *testing.T) {
	src := `
import React from 'react';
import { View } from 'react-native';
import Header from './Header';
import Content from './Content';
import Footer from './Footer';

export default function HomeScreen() {
  return (
    <View>
      <Header title="Home" />
      <Content>
        <View />
      </Content>
      <Footer />
    </View>
  );
}
`
	ents := extractFull(t, "custom_js_react_native", fi("HomeScreen.tsx", "typescript", src))

	if findEntity(ents, "SCOPE.UIComponent", "HomeScreen") == nil {
		t.Error("expected HomeScreen UIComponent")
	}
	for _, child := range []string{"Header", "Content", "Footer"} {
		if findEntity(ents, "SCOPE.UIComponent", child) == nil {
			t.Errorf("expected %s UIComponent child", child)
		}
	}
}

func TestRNParentChildMetadata(t *testing.T) {
	src := `
import React from 'react';
import { View } from 'react-native';

export default function ParentComp() {
  return (
    <View>
      <ChildWidget />
    </View>
  );
}
`
	ents := extractFull(t, "custom_js_react_native", fi("Parent.tsx", "typescript", src))
	child := findEntity(ents, "SCOPE.UIComponent", "ChildWidget")
	if child == nil {
		t.Fatal("ChildWidget entity not found")
	}
	if child.Metadata == nil {
		t.Fatal("ChildWidget has nil metadata")
	}
	checkMetadata(t, child.Metadata, "parent_component", "ParentComp")
}

// ---------------------------------------------------------------------------
// Counter — component with no PascalCase-only children
// ---------------------------------------------------------------------------

func TestRNCounterComponent(t *testing.T) {
	src := `
import React, { useState } from 'react';
import { View, Text, Button } from 'react-native';

export default function Counter() {
  const [count, setCount] = useState(0);
  return (
    <View>
      <Text>{count}</Text>
      <Button title="Increment" onPress={() => setCount(count + 1)} />
    </View>
  );
}
`
	ents := extractFull(t, "custom_js_react_native", fi("Counter.tsx", "typescript", src))

	if findEntity(ents, "SCOPE.UIComponent", "Counter") == nil {
		t.Error("Counter.tsx: expected Counter UIComponent")
	}
}

// ---------------------------------------------------------------------------
// File fixture integration: App.tsx → ≥3 routes
// ---------------------------------------------------------------------------

func TestRNFixtureAppTsx(t *testing.T) {
	content := rnFixture(t, "react-native/App.tsx")
	ents := extractFull(t, "custom_js_react_native", fi("App.tsx", "typescript", string(content)))

	routes := countBySubtype(ents, "route")
	if routes < 3 {
		t.Errorf("App.tsx fixture: expected ≥3 routes, got %d", routes)
	}
}

func TestRNFixtureHomeScreenTsx(t *testing.T) {
	content := rnFixture(t, "react-native/HomeScreen.tsx")
	ents := extractFull(t, "custom_js_react_native", fi("HomeScreen.tsx", "typescript", string(content)))

	if findEntity(ents, "SCOPE.UIComponent", "HomeScreen") == nil {
		t.Error("HomeScreen.tsx fixture: expected HomeScreen UIComponent")
	}
	childCount := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.UIComponent" && e.Name != "HomeScreen" {
			childCount++
		}
	}
	if childCount < 3 {
		t.Errorf("HomeScreen.tsx fixture: expected ≥3 child UIComponents, got %d", childCount)
	}
}

func TestRNFixtureCounterTsx(t *testing.T) {
	content := rnFixture(t, "react-native/Counter.tsx")
	ents := extractFull(t, "custom_js_react_native", fi("Counter.tsx", "typescript", string(content)))

	if findEntity(ents, "SCOPE.UIComponent", "Counter") == nil {
		t.Error("Counter.tsx fixture: expected Counter UIComponent")
	}
}

// ---------------------------------------------------------------------------
// File gate: non-RN files produce 0 entities
// ---------------------------------------------------------------------------

func TestRNFileGateNoMatch(t *testing.T) {
	src := `
export function Foo() {
  return <div>hello</div>;
}
`
	ents := extractFull(t, "custom_js_react_native", fi("plain.tsx", "typescript", src))
	if len(ents) != 0 {
		t.Errorf("plain TSX without RN import: expected 0 entities, got %d", len(ents))
	}
}

func TestRNWrongLanguage(t *testing.T) {
	src := `import { View } from 'react-native';`
	ents := extractFull(t, "custom_js_react_native", fi("comp.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("wrong language: expected 0 entities, got %d", len(ents))
	}
}

func TestRNEmptyFile(t *testing.T) {
	ents := extractFull(t, "custom_js_react_native", fi("empty.tsx", "typescript", ""))
	if len(ents) != 0 {
		t.Errorf("empty file: expected 0 entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Deduplication — same route name not emitted twice
// ---------------------------------------------------------------------------

func TestRNRouteDeduplication(t *testing.T) {
	src := `
import { createStackNavigator } from '@react-navigation/stack';
const Stack = createStackNavigator();
export default function App() {
  return (
    <Stack.Navigator>
      <Stack.Screen name="Home" component={HomeScreen} />
      <Stack.Screen name="Home" component={HomeScreen} />
    </Stack.Navigator>
  );
}
`
	ents := extractFull(t, "custom_js_react_native", fi("App.tsx", "typescript", src))
	homeCount := 0
	for _, e := range ents {
		if e.Name == "route:Stack:Home" {
			homeCount++
		}
	}
	if homeCount != 1 {
		t.Errorf("expected exactly 1 route:Stack:Home, got %d", homeCount)
	}
}

// ---------------------------------------------------------------------------
// Mixed navigator types in one file
// ---------------------------------------------------------------------------

func TestRNMixedNavigators(t *testing.T) {
	src := `
import { createStackNavigator } from '@react-navigation/stack';
import { createBottomTabNavigator } from '@react-navigation/bottom-tabs';
const Stack = createStackNavigator();
const Tab = createBottomTabNavigator();
export default function RootNav() {
  return (
    <Stack.Navigator>
      <Stack.Screen name="Main" component={MainTabs} />
      <Stack.Screen name="Details" component={DetailsScreen} />
    </Stack.Navigator>
  );
}
function MainTabs() {
  return (
    <Tab.Navigator>
      <Tab.Screen name="Home" component={HomeScreen} />
      <Tab.Screen name="Search" component={SearchScreen} />
    </Tab.Navigator>
  );
}
`
	ents := extractFull(t, "custom_js_react_native", fi("RootNav.tsx", "typescript", src))

	for _, want := range []string{
		"route:Stack:Main", "route:Stack:Details",
		"route:Tab:Home", "route:Tab:Search",
	} {
		if findEntity(ents, "SCOPE.Operation", want) == nil {
			t.Errorf("expected SCOPE.Operation/%s", want)
		}
	}
}

// ---------------------------------------------------------------------------
// Screen without component prop
// ---------------------------------------------------------------------------

func TestRNScreenWithoutComponent(t *testing.T) {
	src := `
import { createStackNavigator } from '@react-navigation/stack';
const Stack = createStackNavigator();
export default function App() {
  return (
    <Stack.Navigator>
      <Stack.Screen name="Loading" />
    </Stack.Navigator>
  );
}
`
	ents := extractFull(t, "custom_js_react_native", fi("App.tsx", "typescript", src))
	if findEntity(ents, "SCOPE.Operation", "route:Stack:Loading") == nil {
		t.Error("expected route:Stack:Loading even without component prop")
	}
}

// ---------------------------------------------------------------------------
// Arrow component form
// ---------------------------------------------------------------------------

func TestRNArrowComponentDetected(t *testing.T) {
	src := `
import { View } from 'react-native';
export const MyCard = () => (
  <View>
    <AvatarImage />
    <NameLabel />
  </View>
);
`
	ents := extractFull(t, "custom_js_react_native", fi("MyCard.tsx", "typescript", src))
	if findEntity(ents, "SCOPE.UIComponent", "MyCard") == nil {
		t.Error("expected MyCard UIComponent (arrow form)")
	}
}

// ---------------------------------------------------------------------------
// JavaScript (not TypeScript) is also accepted
// ---------------------------------------------------------------------------

func TestRNJavaScriptFile(t *testing.T) {
	src := `
import { View } from 'react-native';
export default function SimpleComp() {
  return <View />;
}
`
	ents := extractFull(t, "custom_js_react_native", fi("comp.jsx", "javascript", src))
	if findEntity(ents, "SCOPE.UIComponent", "SimpleComp") == nil {
		t.Error("expected SimpleComp UIComponent for .jsx file")
	}
}

// ---------------------------------------------------------------------------
// Screen without name prop — skipped (no entity emitted)
// ---------------------------------------------------------------------------

func TestRNScreenWithoutNameProp(t *testing.T) {
	// A Screen tag with no name= attribute should produce no route entity.
	src := `
import { createStackNavigator } from '@react-navigation/stack';
const Stack = createStackNavigator();
export default function App() {
  return (
    <Stack.Navigator>
      <Stack.Screen component={HomeScreen} />
    </Stack.Navigator>
  );
}
`
	ents := extractFull(t, "custom_js_react_native", fi("App.tsx", "typescript", src))
	routes := countBySubtype(ents, "route")
	if routes != 0 {
		t.Errorf("Screen without name= prop: expected 0 routes, got %d", routes)
	}
}

// ---------------------------------------------------------------------------
// Exported component name with both groups empty (coverage gap)
// ---------------------------------------------------------------------------

func TestRNExportedComponentNoName(t *testing.T) {
	// A file with react-native import but no PascalCase exported component.
	src := `
import { View } from 'react-native';
const helper = () => {};
`
	ents := extractFull(t, "custom_js_react_native", fi("util.tsx", "typescript", src))
	for _, e := range ents {
		if e.Kind == "SCOPE.UIComponent" {
			t.Errorf("expected no UIComponent, got %q", e.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// Language() accessor
// ---------------------------------------------------------------------------

func TestRNExtractorLanguage(t *testing.T) {
	e, ok := extreg.Get("custom_js_react_native")
	if !ok {
		t.Fatal("custom_js_react_native not registered")
	}
	if got := e.Language(); got != "custom_js_react_native" {
		t.Errorf("Language() = %q, want %q", got, "custom_js_react_native")
	}
}

// ---------------------------------------------------------------------------
// Screen detected by Screen tag pattern even without react-native import
// ---------------------------------------------------------------------------

func TestRNGateByScreenTag(t *testing.T) {
	src := `
import { createStackNavigator } from '@react-navigation/stack';
const Stack = createStackNavigator();
export default function App() {
  return <Stack.Screen name="Onboarding" component={OnboardingScreen} />;
}
`
	ents := extractFull(t, "custom_js_react_native", fi("App.tsx", "typescript", src))
	if findEntity(ents, "SCOPE.Operation", "route:Stack:Onboarding") == nil {
		t.Error("expected route:Stack:Onboarding detected by Screen tag pattern")
	}
}
