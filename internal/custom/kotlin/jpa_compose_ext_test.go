package kotlin_test

// jpa_compose_ext_test.go — tests for JPA migration, Compose context,
// deep link, native imports, and branch conditions extractors.
//
// Issue #3275 — Kotlin ORM + UI residual cells.

import (
	"testing"
)

// ===========================================================================
// kotlinJPAMigrationExtractor tests
// ===========================================================================

func TestKotlinJPAMigration_FlywayClass(t *testing.T) {
	src := `
package db.migration

import org.flywaydb.core.api.migration.BaseJavaMigration
import org.flywaydb.core.api.migration.Context

class V2__Add_orders_table : BaseJavaMigration() {
    override fun migrate(context: Context) {
        context.connection.createStatement().execute(
            "CREATE TABLE orders (id BIGINT PRIMARY KEY, user_id BIGINT)"
        )
    }
}
`
	ents := extract(t, "custom_kotlin_jpa_migration", fi("V2__Add_orders_table.kt", "kotlin", src))
	hasMigration := false
	for _, e := range ents {
		if e.Subtype == "migration" {
			hasMigration = true
			break
		}
	}
	if !hasMigration {
		t.Errorf("expected migration entity from Flyway class; got %v", ents)
	}
}

func TestKotlinJPAMigration_FlywayRepeatableClass(t *testing.T) {
	src := `
class R__Refresh_views : BaseJavaMigration() {
    override fun migrate(context: Context) {
        // refresh
    }
}
`
	ents := extract(t, "custom_kotlin_jpa_migration", fi("R__Refresh_views.kt", "kotlin", src))
	hasMigration := false
	for _, e := range ents {
		if e.Subtype == "migration" {
			hasMigration = true
		}
	}
	if !hasMigration {
		t.Error("expected repeatable migration entity")
	}
}

func TestKotlinJPAMigration_LiquibaseChangeSet(t *testing.T) {
	src := `
import liquibase.change.custom.CustomSqlChange
import org.springframework.stereotype.Component

@ChangeSet(order = "001", id = "createUsersTable", author = "dev")
fun createUsers(db: Database) {
    // ...
}
`
	ents := extract(t, "custom_kotlin_jpa_migration", fi("Migration.kt", "kotlin", src))
	hasMigration := false
	for _, e := range ents {
		if e.Subtype == "migration" {
			hasMigration = true
		}
	}
	if !hasMigration {
		t.Error("expected Liquibase changeset migration entity")
	}
}

func TestKotlinJPAMigration_FlywayConfig(t *testing.T) {
	src := `
import org.flywaydb.core.Flyway

@Configuration
class FlywayConfig {
    @Bean
    fun flyway(dataSource: DataSource): Flyway {
        return Flyway.configure().dataSource(dataSource).load()
    }
}
`
	ents := extract(t, "custom_kotlin_jpa_migration", fi("FlywayConfig.kt", "kotlin", src))
	hasMigration := false
	for _, e := range ents {
		if e.Subtype == "migration" {
			hasMigration = true
		}
	}
	if !hasMigration {
		t.Error("expected migration entity from Flyway config bean")
	}
}

func TestKotlinJPAMigration_SpringLiquibase(t *testing.T) {
	src := `
import liquibase.integration.spring.SpringLiquibase

@Bean
fun liquibase(dataSource: DataSource): SpringLiquibase {
    return SpringLiquibase().apply {
        this.dataSource = dataSource
        changeLog = "classpath:db/changelog/db.changelog-master.yaml"
    }
}
`
	ents := extract(t, "custom_kotlin_jpa_migration", fi("LiquibaseConfig.kt", "kotlin", src))
	hasMigration := false
	for _, e := range ents {
		if e.Subtype == "migration" {
			hasMigration = true
		}
	}
	if !hasMigration {
		t.Error("expected migration entity from SpringLiquibase bean")
	}
}

func TestKotlinJPAMigration_NoMatch(t *testing.T) {
	src := `
data class User(val id: Long, val name: String)
`
	ents := extract(t, "custom_kotlin_jpa_migration", fi("User.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestKotlinJPAMigration_WrongLanguage(t *testing.T) {
	src := `class V2__Add_orders_table extends BaseJavaMigration {}`
	ents := extract(t, "custom_kotlin_jpa_migration", fi("V2.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should produce no entities, got %d", len(ents))
	}
}

// ===========================================================================
// composeContextExtractor tests
// ===========================================================================

func TestComposeContext_CompositionLocalDef(t *testing.T) {
	src := `
val LocalTheme = compositionLocalOf { DefaultTheme() }
val LocalNavController = staticCompositionLocalOf<NavController> { error("not provided") }
`
	ents := extract(t, "custom_kotlin_compose_context", fi("AppLocals.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Pattern", "LocalTheme") {
		t.Error("expected LocalTheme context_provider entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "LocalNavController") {
		t.Error("expected LocalNavController context_provider entity")
	}
}

func TestComposeContext_CompositionLocalProvider(t *testing.T) {
	src := `
@Composable
fun AppRoot() {
    val theme = rememberTheme()
    CompositionLocalProvider(LocalTheme provides theme) {
        AppContent()
    }
}
`
	ents := extract(t, "custom_kotlin_compose_context", fi("AppRoot.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "context_provider" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected context_provider entity from CompositionLocalProvider")
	}
}

func TestComposeContext_LocalCurrent(t *testing.T) {
	src := `
@Composable
fun ThemedButton(onClick: () -> Unit) {
    val theme = LocalTheme.current
    val nav = LocalNavController.current
    Button(onClick = onClick) { Text("Click") }
}
`
	ents := extract(t, "custom_kotlin_compose_context", fi("Button.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "context_consumer" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected context_consumer entity from LocalXxx.current")
	}
}

func TestKmpContext_ExpectActualClass(t *testing.T) {
	src := `
expect class AppContext

actual class AppContext(val context: android.content.Context)
`
	ents := extract(t, "custom_kotlin_compose_context", fi("AppContext.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "context_provider" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected context_provider entity from KMP expect/actual context class; got %v", ents)
	}
}

func TestComposeContext_NoMatch(t *testing.T) {
	src := `data class User(val id: Long)`
	ents := extract(t, "custom_kotlin_compose_context", fi("User.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ===========================================================================
// composeDeepLinkExtractor tests
// ===========================================================================

func TestComposeDeepLink_NavDeepLinkURI(t *testing.T) {
	src := `
NavHost(navController, startDestination = "home") {
    composable(
        route = "profile/{userId}",
        deepLinks = listOf(navDeepLink { uriPattern = "app://profile/{userId}" })
    ) { backStack ->
        ProfileScreen(userId = backStack.arguments?.getString("userId"))
    }
}
`
	ents := extract(t, "custom_kotlin_compose_deeplink", fi("AppNav.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Operation", "app://profile/{userId}") {
		t.Errorf("expected deep_link entity with URI pattern; got %v", ents)
	}
}

func TestComposeDeepLink_MultipleDeepLinks(t *testing.T) {
	src := `
composable(
    route = "news/{articleId}",
    deepLinks = listOf(
        navDeepLink { uriPattern = "https://example.com/news/{articleId}" },
        navDeepLink { uriPattern = "myapp://news/{articleId}" }
    )
) { ... }
`
	ents := extract(t, "custom_kotlin_compose_deeplink", fi("News.kt", "kotlin", src))
	count := 0
	for _, e := range ents {
		if e.Subtype == "deep_link" {
			count++
		}
	}
	if count < 2 {
		t.Errorf("expected >=2 deep_link entities, got %d", count)
	}
}

func TestComposeDeepLink_StandaloneUriPattern(t *testing.T) {
	src := `
val deepLink = navDeepLink {
    uriPattern = "https://example.com/users/{userId}"
}
`
	ents := extract(t, "custom_kotlin_compose_deeplink", fi("DeepLink.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "deep_link" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected deep_link entity from standalone navDeepLink")
	}
}

func TestComposeDeepLink_NoMatch(t *testing.T) {
	src := `
NavHost(navController, startDestination = "home") {
    composable("home") { HomeScreen() }
}
`
	ents := extract(t, "custom_kotlin_compose_deeplink", fi("Nav.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("expected no deep link entities, got %d", len(ents))
	}
}

// ===========================================================================
// kotlinNativeImportsExtractor tests
// ===========================================================================

func TestNativeImports_SystemLoadLibrary(t *testing.T) {
	src := `
class MainActivity : AppCompatActivity() {
    companion object {
        init {
            System.loadLibrary("native-lib")
        }
    }
    external fun stringFromJNI(): String
}
`
	ents := extract(t, "custom_kotlin_native_imports", fi("MainActivity.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Component", "native-lib") {
		t.Errorf("expected native-lib native_library entity; got %v", ents)
	}
}

func TestNativeImports_ExternalFun(t *testing.T) {
	src := `
external fun computeHash(data: ByteArray): String
external fun encryptData(key: ByteArray, data: ByteArray): ByteArray
`
	ents := extract(t, "custom_kotlin_native_imports", fi("NativeLib.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Operation", "computeHash") {
		t.Error("expected computeHash native_function entity")
	}
	if !containsEntity(ents, "SCOPE.Operation", "encryptData") {
		t.Error("expected encryptData native_function entity")
	}
}

func TestNativeImports_CInteropImport(t *testing.T) {
	src := `
import platform.Foundation.NSString
import platform.UIKit.UIViewController
import cnames.structs.CFArray
`
	ents := extract(t, "custom_kotlin_native_imports", fi("iOSPlatform.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "native_import" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected native_import entity from cinterop import; got %v", ents)
	}
}

func TestNativeImports_KmpActualNative(t *testing.T) {
	src := `
actual fun nativeCrypto(data: ByteArray): ByteArray = cryptoImpl(data)
`
	ents := extract(t, "custom_kotlin_native_imports", fi("NativeKt.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "native_function" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected native_function from actual fun delegation; got %v", ents)
	}
}

func TestNativeImports_CNameAnnotation(t *testing.T) {
	src := `
@CName("kotlin_lib_init")
fun initKotlinLib() {
    // exported as C symbol
}
`
	ents := extract(t, "custom_kotlin_native_imports", fi("Export.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Operation", "kotlin_lib_init") {
		t.Errorf("expected native_export from @CName; got %v", ents)
	}
}

func TestNativeImports_NoMatch(t *testing.T) {
	src := `
data class User(val id: Long, val name: String)

fun greet(user: User) = "Hello ${user.name}"
`
	ents := extract(t, "custom_kotlin_native_imports", fi("User.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ===========================================================================
// kotlinBranchCondExtractor tests
// ===========================================================================

func TestBranchCond_IfCondition(t *testing.T) {
	src := `
fun processUser(user: User) {
    if (user.isLoggedIn && user.role == "admin") {
        showAdminPanel()
    }
    if (items.size > 0) {
        render(items)
    }
}
`
	ents := extract(t, "custom_kotlin_branch_conditions", fi("UserLogic.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "branch_condition" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected branch_condition entity from if; got %v", ents)
	}
}

func TestBranchCond_WhenSubject(t *testing.T) {
	src := `
@Composable
fun StatusBadge(status: OrderStatus) {
    val color = when (status) {
        OrderStatus.PENDING -> Color.Yellow
        OrderStatus.SHIPPED -> Color.Blue
        OrderStatus.DELIVERED -> Color.Green
    }
    Badge(color = color)
}
`
	ents := extract(t, "custom_kotlin_branch_conditions", fi("Status.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "branch_condition" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected branch_condition entity from when; got %v", ents)
	}
}

func TestBranchCond_FilterLambda(t *testing.T) {
	src := `
val activeUsers = users.filter { it.isActive && it.score > 50 }
val firstAdmin = users.firstOrNull { it.role == "admin" }
`
	ents := extract(t, "custom_kotlin_branch_conditions", fi("Filter.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "branch_condition" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected branch_condition entity from filter lambda; got %v", ents)
	}
}

func TestBranchCond_InlineTernary(t *testing.T) {
	src := `
val label = if (count > 0) "has items" else "empty"
val prefix = if (user.isAdmin) "Admin:" else ""
`
	ents := extract(t, "custom_kotlin_branch_conditions", fi("Ternary.kt", "kotlin", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "branch_condition" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected branch_condition from inline if; got %v", ents)
	}
}

func TestBranchCond_NoMatch(t *testing.T) {
	src := `data class User(val name: String, val id: Long)`
	ents := extract(t, "custom_kotlin_branch_conditions", fi("User.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestBranchCond_WrongLanguage(t *testing.T) {
	src := `if (x > 0) { doSomething(); }`
	ents := extract(t, "custom_kotlin_branch_conditions", fi("Code.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should produce no entities, got %d", len(ents))
	}
}
