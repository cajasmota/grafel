package vhdl_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/vhdl"
	"github.com/cajasmota/grafel/internal/types"
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

// ── Port topology (#5381) ─────────────────────────────────────────────────────

func vhFindPort(ents []types.EntityRecord, name string) *types.EntityRecord {
	return vhFindSubtype(ents, name, "SCOPE.Schema", "port")
}

func TestVHDL_EntityPorts(t *testing.T) {
	src := `
entity Adder is
  port (
    clk  : in    std_logic;
    a, b : in    std_logic_vector(7 downto 0);
    sum  : out   std_logic_vector(8 downto 0);
    bus  : inout std_logic
  );
end entity Adder;
`
	ents := runVHDL(t, src, "adder.vhd")

	for _, tc := range []struct{ name, dir, width string }{
		{"Adder.clk", "in", ""},
		{"Adder.a", "in", "(7 downto 0)"},
		{"Adder.b", "in", "(7 downto 0)"},
		{"Adder.sum", "out", "(8 downto 0)"},
		{"Adder.bus", "inout", ""},
	} {
		p := vhFindPort(ents, tc.name)
		if p == nil {
			t.Errorf("missing port %s", tc.name)
			continue
		}
		if p.Properties["direction"] != tc.dir {
			t.Errorf("%s: direction=%q want %q", tc.name, p.Properties["direction"], tc.dir)
		}
		if p.Properties["width"] != tc.width {
			t.Errorf("%s: width=%q want %q", tc.name, p.Properties["width"], tc.width)
		}
	}

	// CONTAINS edges entity → ports.
	if !vhHasRelPartial(ents, "Adder", "SCOPE.Component", "CONTAINS", "Adder.clk") {
		t.Error("expected CONTAINS edge Adder → Adder.clk")
	}
}

func TestVHDL_PortDedup(t *testing.T) {
	src := `
entity Dut is
  port (
    clk : in std_logic
  );
end entity Dut;
`
	ents := runVHDL(t, src, "dut.vhd")
	count := 0
	for i := range ents {
		if ents[i].Name == "Dut.clk" && ents[i].Subtype == "port" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 Dut.clk port entity, got %d", count)
	}
}

// ── Instance topology props (#5381) ───────────────────────────────────────────

func TestVHDL_InstanceTopology(t *testing.T) {
	src := `
entity Top is
end entity Top;

architecture rtl of Top is
begin
  u_dut : CounterTop port map (clk => clk_s);
  u_cfg : ParamBlock generic map (W => 8) port map (clk => clk_s);
end architecture rtl;
`
	ents := runVHDL(t, src, "top.vhd")

	arch := vhFindSubtype(ents, "rtl_of_Top", "SCOPE.Component", "architecture")
	if arch == nil {
		t.Fatal("missing architecture rtl_of_Top")
	}
	got := map[string]types.RelationshipRecord{}
	for _, r := range arch.Relationships {
		if r.Kind == "USES" {
			got[r.ToID] = r
		}
	}
	dut, ok := got["CounterTop"]
	if !ok {
		t.Fatal("missing USES edge → CounterTop")
	}
	if dut.Properties["instance_name"] != "u_dut" {
		t.Errorf("CounterTop instance_name=%q want u_dut", dut.Properties["instance_name"])
	}
	if dut.Properties["component_type"] != "CounterTop" {
		t.Errorf("CounterTop component_type=%q want CounterTop", dut.Properties["component_type"])
	}
	cfg, ok := got["ParamBlock"]
	if !ok {
		t.Fatal("missing USES edge → ParamBlock")
	}
	if cfg.Properties["parameterized"] != "true" {
		t.Errorf("ParamBlock should be flagged parameterized; props=%v", cfg.Properties)
	}
}

// ── Sim/synth tool detection (#5381) ──────────────────────────────────────────

func vhFindTool(ents []types.EntityRecord, label string) *types.EntityRecord {
	return vhFindSubtype(ents, label, "SCOPE.Component", "tool")
}

func TestVHDL_GhdlPragma(t *testing.T) {
	src := `
architecture rtl of Foo is
begin
  -- pragma translate_off
  assert false report "sim only" severity note;
  -- pragma translate_on
end architecture rtl;
`
	ents := runVHDL(t, src, "foo.vhd")
	tool := vhFindTool(ents, "GHDL")
	if tool == nil {
		t.Fatal("expected GHDL tool entity from translate_off pragma")
	}
	if tool.Properties["tool"] != "ghdl" {
		t.Errorf("tool prop=%q want ghdl", tool.Properties["tool"])
	}
}

func TestVHDL_ModelsimPragma(t *testing.T) {
	src := `
architecture rtl of Foo is
begin
  -- synthesis off
  assert false;
  -- synthesis on
end architecture rtl;
`
	ents := runVHDL(t, src, "foo.vhd")
	if vhFindTool(ents, "ModelSim/QuestaSim") == nil {
		t.Fatal("expected ModelSim/QuestaSim tool entity from -- synthesis off")
	}
}

func TestVHDL_VivadoAttrs(t *testing.T) {
	src := `
architecture rtl of Foo is
  attribute keep       : string;
  attribute mark_debug : string;
begin
end architecture rtl;
`
	ents := runVHDL(t, src, "foo.vhd")
	if vhFindTool(ents, "Vivado") == nil {
		t.Fatal("expected Vivado tool entity from attribute keep / mark_debug")
	}
}

func TestVHDL_QuartusAttrs(t *testing.T) {
	src := `
architecture rtl of Foo is
  attribute altera_attribute : string;
  attribute preserve         : boolean;
begin
end architecture rtl;
`
	ents := runVHDL(t, src, "foo.vhd")
	if vhFindTool(ents, "Quartus") == nil {
		t.Fatal("expected Quartus tool entity from altera_attribute / preserve")
	}
}

func TestVHDL_YosysAttr(t *testing.T) {
	src := `
architecture rtl of Foo is
  attribute blackbox : boolean;
begin
end architecture rtl;
`
	ents := runVHDL(t, src, "foo.vhd")
	if vhFindTool(ents, "Yosys") == nil {
		t.Fatal("expected Yosys tool entity from attribute blackbox")
	}
}

func TestVHDL_NoToolFalsePositive(t *testing.T) {
	ents := runVHDL(t, counterSrc, "counter.vhd")
	for i := range ents {
		if ents[i].Subtype == "tool" {
			t.Errorf("unexpected tool entity %q on plain counter", ents[i].Name)
		}
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
