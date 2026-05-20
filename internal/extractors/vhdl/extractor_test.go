package vhdl_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/vhdl"
	"github.com/cajasmota/archigraph/internal/types"
)

// ── Helpers ──────────────────────────────────────────────────────────────────

func runVHDL(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("vhdl")
	if !ok {
		t.Fatal("vhdl extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "vhdl",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func vhFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func vhFindSubtype(ents []types.EntityRecord, name, kind, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind && ents[i].Subtype == subtype {
			return &ents[i]
		}
	}
	return nil
}

func vhHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	for i := range ents {
		if ents[i].Name != name || ents[i].Kind != kind {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == edgeKind && r.ToID == toID {
				return true
			}
		}
	}
	return false
}

func vhHasRelPartial(ents []types.EntityRecord, name, kind, edgeKind, toIDContains string) bool {
	for i := range ents {
		if ents[i].Name != name || ents[i].Kind != kind {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == edgeKind && strings.Contains(r.ToID, toIDContains) {
				return true
			}
		}
	}
	return false
}

// ── Registration ──────────────────────────────────────────────────────────────

func TestVHDL_Registered(t *testing.T) {
	_, ok := extractor.Get("vhdl")
	if !ok {
		t.Fatal("vhdl extractor not registered")
	}
}

func TestVHDL_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("vhdl")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.vhd",
		Content:  []byte{},
		Language: "vhdl",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected 0 entities, got %d", len(ents))
	}
}

// ── Entity declaration ────────────────────────────────────────────────────────

func TestVHDL_EntityDeclaration(t *testing.T) {
	src := `
library ieee;
use ieee.std_logic_1164.all;

entity CounterTop is
  port (
    clk   : in  std_logic;
    rst   : in  std_logic;
    count : out std_logic_vector(7 downto 0)
  );
end entity CounterTop;
`
	ents := runVHDL(t, src, "counter.vhd")
	e := vhFindSubtype(ents, "CounterTop", "SCOPE.Component", "entity")
	if e == nil {
		t.Fatal("expected SCOPE.Component(entity) CounterTop")
	}
}

// ── Architecture declaration ──────────────────────────────────────────────────

func TestVHDL_ArchitectureDeclaration(t *testing.T) {
	src := `
entity AluCore is
  port (
    a, b : in  std_logic_vector(7 downto 0);
    result : out std_logic_vector(7 downto 0)
  );
end entity AluCore;

architecture rtl of AluCore is
begin
  result <= a or b;
end architecture rtl;
`
	ents := runVHDL(t, src, "alu.vhd")

	if vhFindSubtype(ents, "AluCore", "SCOPE.Component", "entity") == nil {
		t.Fatal("expected SCOPE.Component(entity) AluCore")
	}
	arch := vhFindSubtype(ents, "rtl_of_AluCore", "SCOPE.Component", "architecture")
	if arch == nil {
		t.Fatal("expected SCOPE.Component(architecture) rtl_of_AluCore")
	}
	// PORT_OF edge: architecture → entity.
	if !vhHasRel(ents, "rtl_of_AluCore", "SCOPE.Component", "PORT_OF", "AluCore") {
		t.Error("expected PORT_OF edge: rtl_of_AluCore → AluCore")
	}
}

// ── Package declaration ───────────────────────────────────────────────────────

func TestVHDL_PackageDeclaration(t *testing.T) {
	src := `
package alu_pkg is
  type alu_op_t is (OP_ADD, OP_SUB, OP_AND, OP_OR);

  function to_slv (val : integer; width : integer) return std_logic_vector;
end package alu_pkg;
`
	ents := runVHDL(t, src, "alu_pkg.vhd")
	p := vhFindSubtype(ents, "alu_pkg", "SCOPE.Component", "package")
	if p == nil {
		t.Fatal("expected SCOPE.Component(package) alu_pkg")
	}
}

// ── Package body declaration ──────────────────────────────────────────────────

func TestVHDL_PackageBodyDeclaration(t *testing.T) {
	src := `
package body alu_pkg is
  function to_slv (val : integer; width : integer) return std_logic_vector is
    variable result : std_logic_vector(width-1 downto 0);
  begin
    result := std_logic_vector(to_unsigned(val, width));
    return result;
  end function to_slv;
end package body alu_pkg;
`
	ents := runVHDL(t, src, "alu_pkg.vhd")
	pb := vhFindSubtype(ents, "alu_pkg_body", "SCOPE.Component", "package_body")
	if pb == nil {
		t.Fatal("expected SCOPE.Component(package_body) alu_pkg_body")
	}
	// PORT_OF edge: body → package.
	if !vhHasRel(ents, "alu_pkg_body", "SCOPE.Component", "PORT_OF", "alu_pkg") {
		t.Error("expected PORT_OF edge: alu_pkg_body → alu_pkg")
	}
}

// ── Function extraction ───────────────────────────────────────────────────────

func TestVHDL_FunctionInPackageBody(t *testing.T) {
	src := `
package body math_pkg is
  function clamp (val : integer; lo, hi : integer) return integer is
  begin
    if val < lo then return lo;
    elsif val > hi then return hi;
    else return val;
    end if;
  end function clamp;

  function abs_val (val : integer) return integer is
  begin
    if val < 0 then return -val; else return val; end if;
  end function abs_val;
end package body math_pkg;
`
	ents := runVHDL(t, src, "math_pkg.vhd")
	if vhFind(ents, "math_pkg_body.clamp", "SCOPE.Operation") == nil {
		t.Error("expected SCOPE.Operation math_pkg_body.clamp")
	}
	if vhFind(ents, "math_pkg_body.abs_val", "SCOPE.Operation") == nil {
		t.Error("expected SCOPE.Operation math_pkg_body.abs_val")
	}
}

// ── Procedure extraction ──────────────────────────────────────────────────────

func TestVHDL_ProcedureInArchitecture(t *testing.T) {
	src := `
entity CounterTop is
  port (clk : in std_logic);
end entity CounterTop;

architecture rtl of CounterTop is
  procedure reset_count (signal cnt : out integer) is
  begin
    cnt <= 0;
  end procedure reset_count;
begin
end architecture rtl;
`
	ents := runVHDL(t, src, "counter.vhd")
	if vhFindSubtype(ents, "rtl_of_CounterTop.reset_count", "SCOPE.Operation", "procedure") == nil {
		t.Error("expected SCOPE.Operation(procedure) rtl_of_CounterTop.reset_count")
	}
}

// ── Library / use imports ─────────────────────────────────────────────────────

func TestVHDL_LibraryImports(t *testing.T) {
	src := `
library ieee;
use ieee.std_logic_1164.all;
use ieee.numeric_std.all;

entity CounterTop is
end entity CounterTop;
`
	ents := runVHDL(t, src, "counter.vhd")
	if !vhHasRel(ents, "ieee", "SCOPE.Component", "IMPORTS", "ieee") {
		t.Error("expected IMPORTS edge for ieee")
	}
}

// ── Component instantiation (USES edges) ─────────────────────────────────────

func TestVHDL_ComponentInstantiation(t *testing.T) {
	src := `
entity TbCounterTop is
end entity TbCounterTop;

architecture tb of TbCounterTop is
begin
  u_dut : CounterTop port map (
    clk   => clk_s,
    rst   => rst_s,
    count => count_s
  );

  u_clk : ClockGen port map (
    clk => clk_s
  );
end architecture tb;
`
	ents := runVHDL(t, src, "tb.vhd")
	if !vhHasRel(ents, "tb_of_TbCounterTop", "SCOPE.Component", "USES", "CounterTop") {
		t.Error("expected USES edge: tb_of_TbCounterTop → CounterTop")
	}
	if !vhHasRel(ents, "tb_of_TbCounterTop", "SCOPE.Component", "USES", "ClockGen") {
		t.Error("expected USES edge: tb_of_TbCounterTop → ClockGen")
	}
}

// ── CONTAINS edges ────────────────────────────────────────────────────────────

func TestVHDL_ContainsEdges(t *testing.T) {
	src := `
package body math_pkg is
  function add (a, b : integer) return integer is
  begin return a + b; end function add;
end package body math_pkg;
`
	ents := runVHDL(t, src, "math_pkg.vhd")
	if !vhHasRelPartial(ents, "math_pkg_body", "SCOPE.Component", "CONTAINS", "math_pkg_body.add") {
		t.Error("expected CONTAINS edge to math_pkg_body.add")
	}
}

// ── Language tagging on relationships ────────────────────────────────────────

func TestVHDL_LanguageTagOnRelationships(t *testing.T) {
	src := `
library ieee;
use ieee.std_logic_1164.all;

entity Foo is end entity Foo;
`
	ents := runVHDL(t, src, "foo.vhd")
	for _, ent := range ents {
		for _, r := range ent.Relationships {
			if r.Kind == "IMPORTS" || r.Kind == "USES" || r.Kind == "CONTAINS" || r.Kind == "PORT_OF" {
				if r.Properties == nil || r.Properties["language"] != "vhdl" {
					t.Errorf("relationship %s → %q missing language=vhdl tag", r.Kind, r.ToID)
				}
			}
		}
	}
}

// ── Synthetic fixture: counter + testbench ────────────────────────────────────
//
// counterSrc: 8-bit up-counter with synchronous reset.
// Expected entities:
//   - SCOPE.Component(entity):       CounterTop
//   - SCOPE.Component(architecture): rtl_of_CounterTop
//   - PORT_OF edge:                  rtl_of_CounterTop → CounterTop
const counterSrc = `
library ieee;
use ieee.std_logic_1164.all;
use ieee.numeric_std.all;

-- 8-bit up-counter with synchronous reset
entity CounterTop is
  port (
    clk   : in  std_logic;
    rst   : in  std_logic;
    en    : in  std_logic;
    count : out std_logic_vector(7 downto 0)
  );
end entity CounterTop;

architecture rtl of CounterTop is
  signal cnt_reg : unsigned(7 downto 0);

  procedure do_reset (signal cnt : out unsigned) is
  begin
    cnt <= (others => '0');
  end procedure do_reset;

begin
  process (clk)
  begin
    if rising_edge(clk) then
      if rst = '1' then
        cnt_reg <= (others => '0');
      elsif en = '1' then
        cnt_reg <= cnt_reg + 1;
      end if;
    end if;
  end process;

  count <= std_logic_vector(cnt_reg);
end architecture rtl;
`

// tbSrc: testbench for CounterTop.
// Expected entities:
//   - SCOPE.Component(entity):       TbCounterTop
//   - SCOPE.Component(architecture): tb_of_TbCounterTop
//   - USES edge:                     tb_of_TbCounterTop → CounterTop
const tbCounterSrc = `
library ieee;
use ieee.std_logic_1164.all;

entity TbCounterTop is
end entity TbCounterTop;

architecture tb of TbCounterTop is
  signal clk_s   : std_logic := '0';
  signal rst_s   : std_logic := '1';
  signal en_s    : std_logic := '0';
  signal count_s : std_logic_vector(7 downto 0);
begin
  -- Instantiate DUT
  u_dut : CounterTop port map (
    clk   => clk_s,
    rst   => rst_s,
    en    => en_s,
    count => count_s
  );

  -- Clock generation: 10 ns period
  clk_s <= not clk_s after 5 ns;

  -- Stimulus
  stim_proc : process
  begin
    rst_s <= '1';
    wait for 20 ns;
    rst_s <= '0';
    en_s  <= '1';
    wait for 100 ns;
    assert false report "Simulation complete" severity note;
    wait;
  end process;
end architecture tb;
`

func TestVHDL_CounterFixture(t *testing.T) {
	ents := runVHDL(t, counterSrc, "counter.vhd")

	type check struct {
		name    string
		kind    string
		subtype string
	}
	expected := []check{
		{"CounterTop", "SCOPE.Component", "entity"},
		{"rtl_of_CounterTop", "SCOPE.Component", "architecture"},
		{"rtl_of_CounterTop.do_reset", "SCOPE.Operation", "procedure"},
	}

	hit, miss := 0, 0
	for _, ex := range expected {
		if vhFindSubtype(ents, ex.name, ex.kind, ex.subtype) != nil {
			hit++
		} else {
			miss++
			t.Logf("MISS: %s %s(%s)", ex.kind, ex.name, ex.subtype)
		}
	}
	recall := float64(hit) / float64(len(expected))
	t.Logf("Counter fixture recall: %d/%d = %.0f%%", hit, len(expected), recall*100)
	if recall < 0.80 {
		t.Errorf("entity recall %.0f%% < 80%% threshold", recall*100)
	}

	// PORT_OF edge: rtl architecture → entity.
	if !vhHasRel(ents, "rtl_of_CounterTop", "SCOPE.Component", "PORT_OF", "CounterTop") {
		t.Error("expected PORT_OF edge: rtl_of_CounterTop → CounterTop")
	}

	// IMPORTS: ieee library.
	if !vhHasRel(ents, "ieee", "SCOPE.Component", "IMPORTS", "ieee") {
		t.Error("expected IMPORTS edge for ieee")
	}
}

func TestVHDL_TestbenchFixture(t *testing.T) {
	ents := runVHDL(t, tbCounterSrc, "tb_counter.vhd")

	type check struct {
		name    string
		kind    string
		subtype string
	}
	expected := []check{
		{"TbCounterTop", "SCOPE.Component", "entity"},
		{"tb_of_TbCounterTop", "SCOPE.Component", "architecture"},
	}

	hit, miss := 0, 0
	for _, ex := range expected {
		if vhFindSubtype(ents, ex.name, ex.kind, ex.subtype) != nil {
			hit++
		} else {
			miss++
			t.Logf("MISS: %s %s(%s)", ex.kind, ex.name, ex.subtype)
		}
	}
	recall := float64(hit) / float64(len(expected))
	t.Logf("Testbench fixture recall: %d/%d = %.0f%%", hit, len(expected), recall*100)
	if recall < 0.80 {
		t.Errorf("entity recall %.0f%% < 80%% threshold", recall*100)
	}

	// USES edge: testbench instantiates CounterTop.
	if !vhHasRel(ents, "tb_of_TbCounterTop", "SCOPE.Component", "USES", "CounterTop") {
		t.Error("expected USES edge: tb_of_TbCounterTop → CounterTop")
	}

	// IMPORTS: ieee library.
	if !vhHasRel(ents, "ieee", "SCOPE.Component", "IMPORTS", "ieee") {
		t.Error("expected IMPORTS edge for ieee")
	}
}

// TestVHDL_NoFalsePositives verifies that VHDL keywords do not appear as USES edges.
func TestVHDL_NoFalsePositives(t *testing.T) {
	ents := runVHDL(t, counterSrc, "counter.vhd")

	falsePositiveCandidates := []string{
		"begin", "end", "if", "else", "elsif", "then",
		"process", "signal", "when", "case", "for", "loop",
		"wait", "port", "map", "is", "in", "out",
	}

	for _, ent := range ents {
		for _, rel := range ent.Relationships {
			if rel.Kind != "USES" {
				continue
			}
			toLower := strings.ToLower(rel.ToID)
			for _, kw := range falsePositiveCandidates {
				if toLower == kw {
					t.Errorf("false positive USES edge: %s → %q (should be filtered)", ent.Name, rel.ToID)
				}
			}
		}
	}
}
