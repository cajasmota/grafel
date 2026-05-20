package verilog_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/verilog"
	"github.com/cajasmota/archigraph/internal/types"
)

// ── Helpers ──────────────────────────────────────────────────────────────────

func runVerilog(t *testing.T, src, path, lang string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get(lang)
	if !ok {
		t.Fatalf("%s extractor not registered", lang)
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: lang,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func vFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func vFindSubtype(ents []types.EntityRecord, name, kind, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind && ents[i].Subtype == subtype {
			return &ents[i]
		}
	}
	return nil
}

func vHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
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

func vHasRelPartial(ents []types.EntityRecord, name, kind, edgeKind, toIDContains string) bool {
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

// ── Registration tests ────────────────────────────────────────────────────────

func TestVerilog_Registered(t *testing.T) {
	_, ok := extractor.Get("verilog")
	if !ok {
		t.Fatal("verilog extractor not registered")
	}
}

func TestSystemVerilog_Registered(t *testing.T) {
	_, ok := extractor.Get("systemverilog")
	if !ok {
		t.Fatal("systemverilog extractor not registered")
	}
}

func TestVerilog_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("verilog")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.v",
		Content:  []byte{},
		Language: "verilog",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected 0 entities, got %d", len(ents))
	}
}

// ── Module declaration ────────────────────────────────────────────────────────

func TestVerilog_ModuleDeclaration(t *testing.T) {
	src := `
module adder (
    input  wire [7:0] a,
    input  wire [7:0] b,
    output wire [8:0] sum
);
    assign sum = a + b;
endmodule
`
	ents := runVerilog(t, src, "adder.v", "verilog")
	m := vFindSubtype(ents, "adder", "SCOPE.Component", "module")
	if m == nil {
		t.Fatal("expected SCOPE.Component(module) adder")
	}
}

func TestVerilog_ModuleWithParameters(t *testing.T) {
	src := `
module fifo #(
    parameter WIDTH = 8,
    parameter DEPTH = 16
) (
    input  wire        clk,
    input  wire        rst,
    input  wire [WIDTH-1:0] din,
    output wire [WIDTH-1:0] dout
);
endmodule
`
	ents := runVerilog(t, src, "fifo.v", "verilog")
	m := vFindSubtype(ents, "fifo", "SCOPE.Component", "module")
	if m == nil {
		t.Fatal("expected SCOPE.Component(module) fifo")
	}
}

// ── SV interface, package, class ─────────────────────────────────────────────

func TestSV_InterfaceDeclaration(t *testing.T) {
	src := `
interface bus_if (input logic clk);
    logic [31:0] addr;
    logic [31:0] data;
    logic        valid;
    modport master (output addr, data, valid);
    modport slave  (input  addr, data, valid);
endinterface
`
	ents := runVerilog(t, src, "bus_if.sv", "systemverilog")
	m := vFindSubtype(ents, "bus_if", "SCOPE.Component", "interface")
	if m == nil {
		t.Fatal("expected SCOPE.Component(interface) bus_if")
	}
}

func TestSV_PackageDeclaration(t *testing.T) {
	src := `
package alu_pkg;
    typedef enum logic [1:0] {
        ADD = 2'b00,
        SUB = 2'b01,
        AND = 2'b10,
        OR  = 2'b11
    } alu_op_t;

    function automatic logic [7:0] clamp(input logic [7:0] val);
        return val;
    endfunction
endpackage
`
	ents := runVerilog(t, src, "alu_pkg.sv", "systemverilog")
	p := vFindSubtype(ents, "alu_pkg", "SCOPE.Component", "package")
	if p == nil {
		t.Fatal("expected SCOPE.Component(package) alu_pkg")
	}
	// Function inside package.
	if vFind(ents, "alu_pkg.clamp", "SCOPE.Operation") == nil {
		t.Error("expected SCOPE.Operation alu_pkg.clamp")
	}
}

func TestSV_ClassDeclaration(t *testing.T) {
	src := `
class transaction;
    rand logic [7:0] data;
    rand logic [3:0] addr;

    function new(logic [7:0] d, logic [3:0] a);
        data = d;
        addr = a;
    endfunction

    task display();
        $display("data=%0h addr=%0h", data, addr);
    endtask
endclass
`
	ents := runVerilog(t, src, "transaction.sv", "systemverilog")
	c := vFindSubtype(ents, "transaction", "SCOPE.Component", "class")
	if c == nil {
		t.Fatal("expected SCOPE.Component(class) transaction")
	}
	if vFind(ents, "transaction.new", "SCOPE.Operation") == nil {
		t.Error("expected SCOPE.Operation transaction.new")
	}
	if vFind(ents, "transaction.display", "SCOPE.Operation") == nil {
		t.Error("expected SCOPE.Operation transaction.display")
	}
}

func TestSV_ClassExtends(t *testing.T) {
	src := `
class driver extends base_driver;
    function new();
    endfunction
endclass
`
	ents := runVerilog(t, src, "driver.sv", "systemverilog")
	if !vHasRel(ents, "driver", "SCOPE.Component", "EXTENDS", "base_driver") {
		t.Error("expected EXTENDS base_driver")
	}
}

// ── Function and task extraction ──────────────────────────────────────────────

func TestVerilog_FunctionInModule(t *testing.T) {
	src := `
module alu (
    input  wire [7:0] a, b,
    input  wire [1:0] op,
    output reg  [7:0] result
);
    function automatic [7:0] add_op;
        input [7:0] x, y;
        add_op = x + y;
    endfunction

    function automatic [7:0] sub_op;
        input [7:0] x, y;
        sub_op = x - y;
    endfunction

    always @(*) begin
        case (op)
            2'b00: result = add_op(a, b);
            2'b01: result = sub_op(a, b);
            default: result = 8'h00;
        endcase
    end
endmodule
`
	ents := runVerilog(t, src, "alu.v", "verilog")
	if vFind(ents, "alu.add_op", "SCOPE.Operation") == nil {
		t.Error("expected SCOPE.Operation alu.add_op")
	}
	if vFind(ents, "alu.sub_op", "SCOPE.Operation") == nil {
		t.Error("expected SCOPE.Operation alu.sub_op")
	}
}

func TestVerilog_TaskInModule(t *testing.T) {
	src := `
module mem_ctrl (
    input wire clk,
    input wire rst
);
    task automatic reset_mem;
        integer i;
        for (i = 0; i < 256; i = i + 1) begin
            mem[i] = 8'h00;
        end
    endtask

    task automatic write_mem;
        input [7:0] addr;
        input [7:0] data;
        mem[addr] = data;
    endtask
endmodule
`
	ents := runVerilog(t, src, "mem_ctrl.v", "verilog")
	if vFindSubtype(ents, "mem_ctrl.reset_mem", "SCOPE.Operation", "task") == nil {
		t.Error("expected task mem_ctrl.reset_mem")
	}
	if vFindSubtype(ents, "mem_ctrl.write_mem", "SCOPE.Operation", "task") == nil {
		t.Error("expected task mem_ctrl.write_mem")
	}
}

// ── Include / import extraction ───────────────────────────────────────────────

func TestVerilog_Include(t *testing.T) {
	src := "`include \"defines.vh\"\n`include \"params.vh\"\n\nmodule top;\nendmodule\n"
	ents := runVerilog(t, src, "top.v", "verilog")
	if !vHasRel(ents, "defines", "SCOPE.Component", "IMPORTS", "defines.vh") {
		t.Error("expected IMPORTS edge for defines.vh")
	}
	if !vHasRel(ents, "params", "SCOPE.Component", "IMPORTS", "params.vh") {
		t.Error("expected IMPORTS edge for params.vh")
	}
}

func TestSV_PackageImport(t *testing.T) {
	src := `
import alu_pkg::*;
import uvm_pkg::uvm_component;

module tb_alu;
endmodule
`
	ents := runVerilog(t, src, "tb_alu.sv", "systemverilog")
	if !vHasRel(ents, "alu_pkg", "SCOPE.Component", "IMPORTS", "alu_pkg") {
		t.Error("expected IMPORTS edge for alu_pkg")
	}
	if !vHasRel(ents, "uvm_pkg", "SCOPE.Component", "IMPORTS", "uvm_pkg") {
		t.Error("expected IMPORTS edge for uvm_pkg")
	}
}

// ── Module instantiation (USES edges) ────────────────────────────────────────

func TestVerilog_ModuleInstantiation(t *testing.T) {
	src := `
module top (
    input wire clk,
    input wire rst
);
    wire [7:0] a, b, result;
    wire [1:0] op;

    alu u_alu (
        .a(a),
        .b(b),
        .op(op),
        .result(result)
    );

    controller u_ctrl (
        .clk(clk),
        .rst(rst),
        .op(op)
    );
endmodule
`
	ents := runVerilog(t, src, "top.v", "verilog")
	if !vHasRel(ents, "top", "SCOPE.Component", "USES", "alu") {
		t.Error("expected USES edge for alu instantiation")
	}
	if !vHasRel(ents, "top", "SCOPE.Component", "USES", "controller") {
		t.Error("expected USES edge for controller instantiation")
	}
}

// ── CONTAINS edges ────────────────────────────────────────────────────────────

func TestVerilog_ContainsEdges(t *testing.T) {
	src := `
module alu (input [7:0] a, b, output [7:0] r);
    function automatic [7:0] compute;
        input [7:0] x, y;
        compute = x + y;
    endfunction

    task automatic log_result;
        input [7:0] val;
        $display("result=%h", val);
    endtask
endmodule
`
	ents := runVerilog(t, src, "alu.v", "verilog")
	if !vHasRelPartial(ents, "alu", "SCOPE.Component", "CONTAINS", "alu.compute") {
		t.Error("expected CONTAINS edge to alu.compute")
	}
	if !vHasRelPartial(ents, "alu", "SCOPE.Component", "CONTAINS", "alu.log_result") {
		t.Error("expected CONTAINS edge to alu.log_result")
	}
}

// ── Language tagging ──────────────────────────────────────────────────────────

func TestVerilog_LanguageTagOnRelationships(t *testing.T) {
	src := "`include \"defs.vh\"\nmodule foo;\nendmodule\n"
	ents := runVerilog(t, src, "foo.v", "verilog")
	for _, ent := range ents {
		for _, r := range ent.Relationships {
			if r.Kind == "IMPORTS" || r.Kind == "USES" || r.Kind == "CONTAINS" || r.Kind == "EXTENDS" {
				if r.Properties == nil || r.Properties["language"] != "verilog" {
					t.Errorf("relationship %s → %q missing language=verilog tag", r.Kind, r.ToID)
				}
			}
		}
	}
}

func TestSV_LanguageTagOnRelationships(t *testing.T) {
	src := "import pkg::*;\nmodule bar;\nendmodule\n"
	ents := runVerilog(t, src, "bar.sv", "systemverilog")
	for _, ent := range ents {
		for _, r := range ent.Relationships {
			if r.Kind == "IMPORTS" || r.Kind == "USES" || r.Kind == "CONTAINS" {
				if r.Properties == nil || r.Properties["language"] != "systemverilog" {
					t.Errorf("relationship %s → %q missing language=systemverilog tag", r.Kind, r.ToID)
				}
			}
		}
	}
}

// ── Synthetic ALU + testbench fixture (≥80% entity recall) ───────────────────
//
// aluSrc: a simple 8-bit ALU with four operations.
// Expected entities:
//   - SCOPE.Component(module): alu
//   - SCOPE.Operation(function): alu.add_op, alu.sub_op, alu.and_op, alu.or_op
const aluSrc = `
// 8-bit ALU with four operations
module alu (
    input  wire [7:0]  a,
    input  wire [7:0]  b,
    input  wire [1:0]  op,    // 00=ADD 01=SUB 10=AND 11=OR
    output reg  [7:0]  result
);

    function automatic [7:0] add_op;
        input [7:0] x, y;
        add_op = x + y;
    endfunction

    function automatic [7:0] sub_op;
        input [7:0] x, y;
        sub_op = x - y;
    endfunction

    function automatic [7:0] and_op;
        input [7:0] x, y;
        and_op = x & y;
    endfunction

    function automatic [7:0] or_op;
        input [7:0] x, y;
        or_op = x | y;
    endfunction

    always @(*) begin
        case (op)
            2'b00: result = add_op(a, b);
            2'b01: result = sub_op(a, b);
            2'b10: result = and_op(a, b);
            2'b11: result = or_op(a, b);
            default: result = 8'h00;
        endcase
    end

endmodule
`

// tbSrc: a testbench that instantiates alu.
// Expected entities:
//   - SCOPE.Component(module): tb_alu
//   - USES edge: tb_alu → alu
//   - SCOPE.Operation(task): tb_alu.run_test, tb_alu.check_result
//
// Note: the backtick in "`include" is embedded via string concatenation to
// avoid terminating the Go raw-string literal.
var tbSrc = "`" + `include "defines.vh"

// Testbench for 8-bit ALU
module tb_alu;
    reg  [7:0] a, b;
    reg  [1:0] op;
    wire [7:0] result;

    // Instantiate DUT
    alu uut (
        .a(a),
        .b(b),
        .op(op),
        .result(result)
    );

    task automatic run_test;
        input [7:0] ta, tb_val;
        input [1:0] top;
        a  = ta;
        b  = tb_val;
        op = top;
    endtask

    task automatic check_result;
        input [7:0] expected;
        if (result !== expected) begin
            $display("FAIL");
        end
    endtask

    initial begin
        run_test(8'hAA, 8'h55, 2'b00);
        check_result(8'hFF);
        $finish;
    end
endmodule
`

func TestVerilog_ALUFixture(t *testing.T) {
	ents := runVerilog(t, aluSrc, "alu.v", "verilog")

	type check struct {
		name    string
		kind    string
		subtype string
	}
	expected := []check{
		{"alu", "SCOPE.Component", "module"},
		{"alu.add_op", "SCOPE.Operation", "function"},
		{"alu.sub_op", "SCOPE.Operation", "function"},
		{"alu.and_op", "SCOPE.Operation", "function"},
		{"alu.or_op", "SCOPE.Operation", "function"},
	}

	hit, miss := 0, 0
	for _, ex := range expected {
		if vFindSubtype(ents, ex.name, ex.kind, ex.subtype) != nil {
			hit++
		} else {
			miss++
			t.Logf("MISS: %s %s(%s)", ex.kind, ex.name, ex.subtype)
		}
	}
	recall := float64(hit) / float64(len(expected))
	t.Logf("ALU fixture recall: %d/%d = %.0f%%", hit, len(expected), recall*100)
	if recall < 0.80 {
		t.Errorf("entity recall %.0f%% < 80%% threshold", recall*100)
	}
}

func TestVerilog_TestbenchFixture(t *testing.T) {
	ents := runVerilog(t, tbSrc, "tb_alu.v", "verilog")

	type check struct {
		name    string
		kind    string
		subtype string
	}
	expected := []check{
		{"tb_alu", "SCOPE.Component", "module"},
		{"tb_alu.run_test", "SCOPE.Operation", "task"},
		{"tb_alu.check_result", "SCOPE.Operation", "task"},
	}

	hit, miss := 0, 0
	for _, ex := range expected {
		if vFindSubtype(ents, ex.name, ex.kind, ex.subtype) != nil {
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

	// USES edge: tb_alu instantiates alu.
	if !vHasRel(ents, "tb_alu", "SCOPE.Component", "USES", "alu") {
		t.Error("expected USES edge: tb_alu → alu")
	}

	// IMPORTS: `include "defines.vh"
	if !vHasRel(ents, "defines", "SCOPE.Component", "IMPORTS", "defines.vh") {
		t.Error("expected IMPORTS edge for defines.vh")
	}
}

// TestVerilog_NoFalsePositives verifies that common Verilog keywords do not
// appear as USES edges.
func TestVerilog_NoFalsePositives(t *testing.T) {
	ents := runVerilog(t, aluSrc, "alu.v", "verilog")

	falsePositiveCandidates := []string{
		"always", "begin", "end", "if", "else", "case", "endcase",
		"input", "output", "wire", "reg", "assign",
		"for", "while", "initial", "forever",
	}

	for _, ent := range ents {
		for _, rel := range ent.Relationships {
			if rel.Kind != "USES" {
				continue
			}
			for _, kw := range falsePositiveCandidates {
				if rel.ToID == kw {
					t.Errorf("false positive USES edge: %s → %q (should be filtered)", ent.Name, kw)
				}
			}
		}
	}
}

// ── SV comprehensive fixture ──────────────────────────────────────────────────

// svAluPkg: SystemVerilog package + module using the package.
// Expected entities:
//   - SCOPE.Component(package): alu_sv_pkg
//   - SCOPE.Operation(function): alu_sv_pkg.clamp8
//   - SCOPE.Component(module): alu_sv
//   - IMPORTS edge: alu_sv_pkg
const svAluPkg = `
package alu_sv_pkg;
    typedef enum logic [1:0] {
        OP_ADD = 2'b00,
        OP_SUB = 2'b01,
        OP_AND = 2'b10,
        OP_OR  = 2'b11
    } alu_op_e;

    function automatic logic [7:0] clamp8;
        input logic [8:0] val;
        clamp8 = val[8] ? 8'hFF : val[7:0];
    endfunction
endpackage

import alu_sv_pkg::*;

module alu_sv (
    input  logic [7:0]   a,
    input  logic [7:0]   b,
    input  alu_sv_pkg::alu_op_e op,
    output logic [7:0]   result
);
    always_comb begin
        unique case (op)
            alu_sv_pkg::OP_ADD: result = clamp8({1'b0, a} + {1'b0, b});
            alu_sv_pkg::OP_SUB: result = a - b;
            alu_sv_pkg::OP_AND: result = a & b;
            alu_sv_pkg::OP_OR:  result = a | b;
            default:            result = 8'h00;
        endcase
    end
endmodule
`

func TestSV_ALUPackageFixture(t *testing.T) {
	ents := runVerilog(t, svAluPkg, "alu_sv.sv", "systemverilog")

	type check struct {
		name    string
		kind    string
		subtype string
	}
	expected := []check{
		{"alu_sv_pkg", "SCOPE.Component", "package"},
		{"alu_sv_pkg.clamp8", "SCOPE.Operation", "function"},
		{"alu_sv", "SCOPE.Component", "module"},
	}

	hit, miss := 0, 0
	for _, ex := range expected {
		if vFindSubtype(ents, ex.name, ex.kind, ex.subtype) != nil {
			hit++
		} else {
			miss++
			t.Logf("MISS: %s %s(%s)", ex.kind, ex.name, ex.subtype)
		}
	}
	recall := float64(hit) / float64(len(expected))
	t.Logf("SV ALU package fixture recall: %d/%d = %.0f%%", hit, len(expected), recall*100)
	if recall < 0.80 {
		t.Errorf("entity recall %.0f%% < 80%% threshold", recall*100)
	}

	// Import of the package.
	if !vHasRel(ents, "alu_sv_pkg", "SCOPE.Component", "IMPORTS", "alu_sv_pkg") {
		t.Error("expected IMPORTS edge for alu_sv_pkg")
	}
}
