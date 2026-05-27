package substrate

import "testing"

// TestEntryPointsRegistryT2Coverage verifies that every Phase 1B T2
// language (#2767) has a registered entry-point sniffer.
func TestEntryPointsRegistryT2Coverage(t *testing.T) {
	for _, lang := range []string{"ruby", "php", "rust", "csharp", "kotlin", "elixir", "scala", "c-cpp"} {
		if EntryPointSnifferFor(lang) == nil {
			t.Errorf("T2 language %q has no registered entry-point sniffer", lang)
		}
	}
}

func TestSniffRubyEntryPoints(t *testing.T) {
	src := `#!/usr/bin/env ruby

def main
  puts "hi"
end

def public_method
end

def _private_helper
end

def test_something
end

describe "a thing" do
  it "works" do
  end
end

Given /^a step$/ do
end
`
	eps := sniffRubyEntryPoints(src)
	kinds := map[string]EntryKind{}
	for _, e := range eps {
		kinds[e.Ident] = e.Kind
	}
	if kinds["__main__"] != EntryKindCLIMain {
		t.Errorf("shebang should yield __main__ cli_main, got %v", kinds["__main__"])
	}
	if kinds["main"] != EntryKindCLIMain {
		t.Errorf("def main should be cli_main, got %v", kinds["main"])
	}
	if kinds["public_method"] != EntryKindLibraryExport {
		t.Errorf("public_method should be library_export, got %v", kinds["public_method"])
	}
	if _, ok := kinds["_private_helper"]; ok {
		t.Errorf("_private_helper should not be an entry-point")
	}
	if kinds["test_something"] != EntryKindTestEntry {
		t.Errorf("test_something should be test_entry, got %v", kinds["test_something"])
	}
	if kinds["describe"] != EntryKindTestEntry {
		t.Errorf("describe block should be test_entry, got %v", kinds["describe"])
	}
	if kinds["it"] != EntryKindTestEntry {
		t.Errorf("it block should be test_entry, got %v", kinds["it"])
	}
	if kinds["Given"] != EntryKindTestEntry {
		t.Errorf("Given step should be test_entry, got %v", kinds["Given"])
	}
}

func TestSniffPHPEntryPoints(t *testing.T) {
	src := `<?php

function main() {}

class App {
    public function compute() {}

    private function helper() {}

    public function register() {}

    /**
     * @test
     */
    public function shouldDoThing() {}

    public function testLogin() {}
}
`
	eps := sniffPHPEntryPoints(src)
	kinds := map[string]EntryKind{}
	for _, e := range eps {
		kinds[e.Ident] = e.Kind
	}
	if kinds["__main__"] != EntryKindCLIMain {
		t.Errorf("<?php opening tag should yield __main__ cli_main, got %v", kinds["__main__"])
	}
	if kinds["main"] != EntryKindCLIMain {
		t.Errorf("function main should be cli_main, got %v", kinds["main"])
	}
	if kinds["compute"] != EntryKindLibraryExport {
		t.Errorf("compute should be library_export, got %v", kinds["compute"])
	}
	if _, ok := kinds["helper"]; ok {
		t.Errorf("private helper should not be an entry-point")
	}
	if kinds["register"] != EntryKindFrameworkLifecycle {
		t.Errorf("register should be framework_lifecycle, got %v", kinds["register"])
	}
	if kinds["shouldDoThing"] != EntryKindTestEntry {
		t.Errorf("@test should mark shouldDoThing as test_entry, got %v", kinds["shouldDoThing"])
	}
	if kinds["testLogin"] != EntryKindTestEntry {
		t.Errorf("testLogin should be test_entry, got %v", kinds["testLogin"])
	}
}

func TestSniffRustEntryPoints(t *testing.T) {
	src := `pub fn exported() {}

fn internal_helper() {}

fn main() {
    println!("hi");
}

#[test]
fn test_login() {}

#[tokio::test]
async fn test_async() {}

#[ctor::ctor]
fn lifecycle_init() {}

pub struct Widget;
`
	eps := sniffRustEntryPoints(src)
	kinds := map[string]EntryKind{}
	for _, e := range eps {
		kinds[e.Ident] = e.Kind
	}
	if kinds["main"] != EntryKindCLIMain {
		t.Errorf("fn main should be cli_main, got %v", kinds["main"])
	}
	if kinds["exported"] != EntryKindLibraryExport {
		t.Errorf("pub fn exported should be library_export, got %v", kinds["exported"])
	}
	if _, ok := kinds["internal_helper"]; ok {
		t.Errorf("non-pub fn internal_helper should not be an entry-point")
	}
	if kinds["test_login"] != EntryKindTestEntry {
		t.Errorf("#[test] fn test_login should be test_entry, got %v", kinds["test_login"])
	}
	if kinds["test_async"] != EntryKindTestEntry {
		t.Errorf("#[tokio::test] async fn test_async should be test_entry, got %v", kinds["test_async"])
	}
	if kinds["lifecycle_init"] != EntryKindFrameworkLifecycle {
		t.Errorf("#[ctor::ctor] fn lifecycle_init should be framework_lifecycle, got %v", kinds["lifecycle_init"])
	}
	if kinds["Widget"] != EntryKindLibraryExport {
		t.Errorf("pub struct Widget should be library_export, got %v", kinds["Widget"])
	}
}

func TestSniffCSharpEntryPoints(t *testing.T) {
	src := `namespace X;

public class Program
{
    public static async Task Main(string[] args) {}

    [Test]
    public void ShouldDoThing() {}

    [Fact]
    public void ItWorks() {}

    [OneTimeSetUp]
    public void Init() {}

    public void Compute() {}

    public void ConfigureServices(IServiceCollection s) {}

    private void Helper() {}
}
`
	eps := sniffCSharpEntryPoints(src)
	kinds := map[string]EntryKind{}
	for _, e := range eps {
		kinds[e.Ident] = e.Kind
	}
	if kinds["Main"] != EntryKindCLIMain {
		t.Errorf("Main should be cli_main, got %v", kinds["Main"])
	}
	if kinds["ShouldDoThing"] != EntryKindTestEntry {
		t.Errorf("[Test] should mark ShouldDoThing as test_entry, got %v", kinds["ShouldDoThing"])
	}
	if kinds["ItWorks"] != EntryKindTestEntry {
		t.Errorf("[Fact] should mark ItWorks as test_entry, got %v", kinds["ItWorks"])
	}
	if kinds["Init"] != EntryKindFrameworkLifecycle {
		t.Errorf("[OneTimeSetUp] should mark Init as framework_lifecycle, got %v", kinds["Init"])
	}
	if kinds["Compute"] != EntryKindLibraryExport {
		t.Errorf("Compute should be library_export, got %v", kinds["Compute"])
	}
	if kinds["ConfigureServices"] != EntryKindFrameworkLifecycle {
		t.Errorf("ConfigureServices should be framework_lifecycle, got %v", kinds["ConfigureServices"])
	}
	if _, ok := kinds["Helper"]; ok {
		t.Errorf("private Helper should not be an entry-point")
	}
}

func TestSniffKotlinEntryPoints(t *testing.T) {
	src := `package x

fun main(args: Array<String>) {}

class App {
    @Test
    fun shouldWork() {}

    @PostConstruct
    fun initialise() {}

    fun compute() {}

    private fun helper() {}
}

object Singleton

class PublicClass
`
	eps := sniffKotlinEntryPoints(src)
	kinds := map[string]EntryKind{}
	for _, e := range eps {
		kinds[e.Ident] = e.Kind
	}
	if kinds["main"] != EntryKindCLIMain {
		t.Errorf("fun main should be cli_main, got %v", kinds["main"])
	}
	if kinds["shouldWork"] != EntryKindTestEntry {
		t.Errorf("@Test should mark shouldWork as test_entry, got %v", kinds["shouldWork"])
	}
	if kinds["initialise"] != EntryKindFrameworkLifecycle {
		t.Errorf("@PostConstruct should mark initialise as framework_lifecycle, got %v", kinds["initialise"])
	}
	if kinds["compute"] != EntryKindLibraryExport {
		t.Errorf("compute should be library_export, got %v", kinds["compute"])
	}
	if _, ok := kinds["helper"]; ok {
		t.Errorf("private helper should not be an entry-point")
	}
	if kinds["Singleton"] != EntryKindLibraryExport {
		t.Errorf("object Singleton should be library_export, got %v", kinds["Singleton"])
	}
	if kinds["PublicClass"] != EntryKindLibraryExport {
		t.Errorf("class PublicClass should be library_export, got %v", kinds["PublicClass"])
	}
}

func TestSniffElixirEntryPoints(t *testing.T) {
	src := `defmodule MyApp do
  def main(args) do
    :ok
  end

  def public_fn do
    :ok
  end

  defp private_fn do
    :ok
  end

  def init(state) do
    {:ok, state}
  end

  def handle_call(_, _, state) do
    {:reply, :ok, state}
  end

  test "a thing works" do
    assert true
  end
end
`
	eps := sniffElixirEntryPoints(src)
	kinds := map[string]EntryKind{}
	idents := map[string]bool{}
	for _, e := range eps {
		kinds[e.Ident] = e.Kind
		idents[e.Ident] = true
	}
	if kinds["main"] != EntryKindCLIMain {
		t.Errorf("def main should be cli_main, got %v", kinds["main"])
	}
	if kinds["public_fn"] != EntryKindLibraryExport {
		t.Errorf("def public_fn should be library_export, got %v", kinds["public_fn"])
	}
	if idents["private_fn"] {
		t.Errorf("defp private_fn should not be an entry-point")
	}
	if kinds["init"] != EntryKindFrameworkLifecycle {
		t.Errorf("def init should be framework_lifecycle, got %v", kinds["init"])
	}
	if kinds["handle_call"] != EntryKindFrameworkLifecycle {
		t.Errorf("def handle_call should be framework_lifecycle, got %v", kinds["handle_call"])
	}
	if kinds["a thing works"] != EntryKindTestEntry {
		t.Errorf("test macro should yield test_entry with label, got %v", kinds["a thing works"])
	}
}

func TestSniffScalaEntryPoints(t *testing.T) {
	src := `package x

object MyApp extends App {
  def main(args: Array[String]): Unit = ()
}

object Runner extends IOApp.Simple

class Service {
  def compute(): String = ""

  private def helper(): Unit = ()

  @Test
  def shouldWork(): Unit = ()

  @BeforeAll
  def setUp(): Unit = ()
}

object Helpers

test("a test") {}
`
	eps := sniffScalaEntryPoints(src)
	kinds := map[string]EntryKind{}
	idents := map[string]bool{}
	for _, e := range eps {
		kinds[e.Ident] = e.Kind
		idents[e.Ident] = true
	}
	if kinds["MyApp"] != EntryKindCLIMain {
		t.Errorf("object MyApp extends App should be cli_main, got %v", kinds["MyApp"])
	}
	if kinds["Runner"] != EntryKindCLIMain {
		t.Errorf("object Runner extends IOApp.Simple should be cli_main, got %v", kinds["Runner"])
	}
	if kinds["compute"] != EntryKindLibraryExport {
		t.Errorf("def compute should be library_export, got %v", kinds["compute"])
	}
	if idents["helper"] {
		t.Errorf("private def helper should not be an entry-point")
	}
	if kinds["shouldWork"] != EntryKindTestEntry {
		t.Errorf("@Test should mark shouldWork as test_entry, got %v", kinds["shouldWork"])
	}
	if kinds["setUp"] != EntryKindFrameworkLifecycle {
		t.Errorf("@BeforeAll should mark setUp as framework_lifecycle, got %v", kinds["setUp"])
	}
	if kinds["Service"] != EntryKindLibraryExport {
		t.Errorf("class Service should be library_export, got %v", kinds["Service"])
	}
	if kinds["Helpers"] != EntryKindLibraryExport {
		t.Errorf("object Helpers should be library_export, got %v", kinds["Helpers"])
	}
}

func TestSniffCCPPEntryPoints(t *testing.T) {
	src := `#include <stdio.h>

int main(int argc, char *argv[]) {
    return 0;
}

void exported_fn() {
}

static void internal_fn() {
}

__attribute__((constructor))
void ctor_hook() {
}

TEST(Suite, Name) {
}

TEST_F(MyFixture, Works) {
}

TEST_CASE("a thing", "[tag]") {
}

BOOST_AUTO_TEST_CASE(my_case) {
}
`
	eps := sniffCCPPEntryPoints(src)
	kinds := map[string]EntryKind{}
	idents := map[string]bool{}
	for _, e := range eps {
		kinds[e.Ident] = e.Kind
		idents[e.Ident] = true
	}
	if kinds["main"] != EntryKindCLIMain {
		t.Errorf("int main should be cli_main, got %v", kinds["main"])
	}
	if kinds["exported_fn"] != EntryKindLibraryExport {
		t.Errorf("exported_fn should be library_export, got %v", kinds["exported_fn"])
	}
	if idents["internal_fn"] {
		t.Errorf("static internal_fn should not be an entry-point")
	}
	if kinds["__constructor"] != EntryKindFrameworkLifecycle {
		t.Errorf("__attribute__((constructor)) should yield framework_lifecycle, got %v", kinds["__constructor"])
	}
	if kinds["Suite_Name"] != EntryKindTestEntry {
		t.Errorf("TEST(Suite, Name) should yield test_entry with Suite_Name, got %v", kinds["Suite_Name"])
	}
	if kinds["MyFixture_Works"] != EntryKindTestEntry {
		t.Errorf("TEST_F(MyFixture, Works) should yield test_entry, got %v", kinds["MyFixture_Works"])
	}
	if kinds["a thing"] != EntryKindTestEntry {
		t.Errorf("TEST_CASE(\"a thing\") should yield test_entry, got %v", kinds["a thing"])
	}
	if kinds["my_case"] != EntryKindTestEntry {
		t.Errorf("BOOST_AUTO_TEST_CASE(my_case) should yield test_entry, got %v", kinds["my_case"])
	}
}
