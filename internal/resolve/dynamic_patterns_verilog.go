package resolve

import "regexp"

// verilogDynamicPatterns are per-language patterns for Verilog and SystemVerilog.
// Registered for both "verilog" and "systemverilog" via init().
//
// The Verilog extractor emits USES edges whose ToID is a module/interface type
// name found in instantiation statements.  Categories of patterns that should
// be resolved as Dynamic (not expected to resolve to an in-tree entity):
//
//  1. Verilog/SV built-in system tasks and functions — $display, $monitor,
//     $finish, etc.  These start with "$" and are always built-in.
//
//  2. SV system functions — $cast, $realtime, $urandom, $rose, $fell.
//
//  3. UVM macro call targets — `uvm_info, `uvm_error, `uvm_component_utils.
//     The extractor strips the backtick and emits the bare macro name.
//
//  4. Assertion keywords used as call-like constructs — assert, assume, cover,
//     assert property.
//
//  5. Common Verilog gate primitives — and, or, not, nand, nor, xor, xnor,
//     buf, mux.  These appear as module-level instantiation-lookalikes but are
//     language builtins.
//
//  6. Standard cell / IP primitive names common in synthesis flows —
//     BUFG, IBUF, OBUF (Xilinx), sky130_fd_sc_hd__* (SkyWater PDK), etc.
//     Matched by prefix patterns so new variants are covered automatically.
var verilogDynamicPatterns = []*regexp.Regexp{
	// ── 1. System tasks (all start with $) ───────────────────────────────
	regexp.MustCompile(`^\$`), // covers $display, $monitor, $finish, $stop, $time, etc.

	// ── 2. SV system functions (explicit list for clarity) ───────────────
	regexp.MustCompile(`^\$cast$`),          // $cast(dest, src) — checked dynamic cast
	regexp.MustCompile(`^\$realtime$`),      // $realtime — real-valued simulation time
	regexp.MustCompile(`^\$urandom$`),       // $urandom — SV PRNG
	regexp.MustCompile(`^\$urandom_range$`), // $urandom_range(max, min)
	regexp.MustCompile(`^\$rose$`),          // $rose(signal) — assertion clock edge
	regexp.MustCompile(`^\$fell$`),          // $fell(signal) — assertion clock edge
	regexp.MustCompile(`^\$stable$`),        // $stable(signal) — no change
	regexp.MustCompile(`^\$past$`),          // $past(signal, n) — past value
	regexp.MustCompile(`^\$changed$`),       // $changed(signal) — value changed
	regexp.MustCompile(`^\$isunknown$`),     // $isunknown(expr) — X/Z check
	regexp.MustCompile(`^\$onehot$`),        // $onehot(expr) — one-hot check
	regexp.MustCompile(`^\$onehot0$`),       // $onehot0(expr) — at most one-hot
	regexp.MustCompile(`^\$countones$`),     // $countones(expr)

	// ── 3. UVM macro targets ──────────────────────────────────────────────
	// Extractor strips the leading backtick; resulting identifier is registered.
	regexp.MustCompile(`^uvm_info$`),                  // `uvm_info(ID, MSG, UVM_LOW)
	regexp.MustCompile(`^uvm_error$`),                 // `uvm_error(ID, MSG)
	regexp.MustCompile(`^uvm_fatal$`),                 // `uvm_fatal(ID, MSG)
	regexp.MustCompile(`^uvm_warning$`),               // `uvm_warning(ID, MSG)
	regexp.MustCompile(`^uvm_component_utils$`),       // `uvm_component_utils(class_name)
	regexp.MustCompile(`^uvm_object_utils$`),          // `uvm_object_utils(class_name)
	regexp.MustCompile(`^uvm_component_utils_begin$`), // begin-end factory registration
	regexp.MustCompile(`^uvm_component_utils_end$`),
	regexp.MustCompile(`^uvm_object_utils_begin$`),
	regexp.MustCompile(`^uvm_object_utils_end$`),
	regexp.MustCompile(`^uvm_field_int$`), // `uvm_field_* macros
	regexp.MustCompile(`^uvm_field_string$`),
	regexp.MustCompile(`^uvm_field_object$`),
	regexp.MustCompile(`^uvm_field_array_int$`),

	// ── 4. Assertion keywords ─────────────────────────────────────────────
	regexp.MustCompile(`^assert$`),          // assert(condition)
	regexp.MustCompile(`^assume$`),          // assume property (...)
	regexp.MustCompile(`^cover$`),           // cover property (...)
	regexp.MustCompile(`^assert_property$`), // assert property(...) — space stripped

	// ── 5. Verilog gate primitives (structural) ───────────────────────────
	// These appear syntactically identical to module instantiations.
	regexp.MustCompile(`^and$`),
	regexp.MustCompile(`^or$`),
	regexp.MustCompile(`^not$`),
	regexp.MustCompile(`^nand$`),
	regexp.MustCompile(`^nor$`),
	regexp.MustCompile(`^xor$`),
	regexp.MustCompile(`^xnor$`),
	regexp.MustCompile(`^buf$`),
	regexp.MustCompile(`^bufif[01]$`),
	regexp.MustCompile(`^notif[01]$`),
	regexp.MustCompile(`^pullup$`),
	regexp.MustCompile(`^pulldown$`),
	regexp.MustCompile(`^tranif[01]$`),
	regexp.MustCompile(`^tran$`),
	regexp.MustCompile(`^rcmos$`),
	regexp.MustCompile(`^cmos$`),

	// ── 6. FPGA / standard-cell IP primitives ────────────────────────────
	// Xilinx/Vivado unisim primitives (BUFG, IBUF, OBUF, MMCME2_ADV, …)
	regexp.MustCompile(`^BUFG`),      // BUFG, BUFGCE, BUFGMUX, …
	regexp.MustCompile(`^IBUF`),      // IBUF, IBUFDS, IBUFGDS, …
	regexp.MustCompile(`^OBUF`),      // OBUF, OBUFT, OBUFDS, …
	regexp.MustCompile(`^IOBUF`),     // IOBUF, IOBUFDS
	regexp.MustCompile(`^MMCM`),      // MMCME2_ADV, MMCME4_ADV, …
	regexp.MustCompile(`^PLL`),       // PLLE2_ADV, PLLE4_ADV, …
	regexp.MustCompile(`^BRAM_`),     // BRAM_SDP_MACRO, BRAM_TDP_MACRO
	regexp.MustCompile(`^RAMB`),      // RAMB16, RAMB18E2, …
	regexp.MustCompile(`^LUT[1-6]$`), // LUT1 … LUT6
	regexp.MustCompile(`^FDRE$`),     // D flip-flop with reset (Xilinx)
	regexp.MustCompile(`^FDSE$`),     // D flip-flop with set (Xilinx)
	regexp.MustCompile(`^FDCE$`),     // D flip-flop CE (Xilinx)
	regexp.MustCompile(`^FDPE$`),     // D flip-flop PE (Xilinx)
	regexp.MustCompile(`^LDCE$`),     // Latch with CE (Xilinx)
	regexp.MustCompile(`^DSP48`),     // DSP48E1, DSP48E2 (Xilinx)
	// SkyWater PDK sky130 standard cells
	regexp.MustCompile(`^sky130_`), // sky130_fd_sc_hd__and2_1, etc.
	// Intel/Altera primitives
	regexp.MustCompile(`^altera_`),
	regexp.MustCompile(`^cyclone`),
	regexp.MustCompile(`^stratix`),
	regexp.MustCompile(`^GLOBAL$`),
	// Generic ASIC std-cell patterns
	regexp.MustCompile(`^SC_`), // standard-cell prefix
}

func init() {
	dynamicPatternsByLang["verilog"] = verilogDynamicPatterns
	dynamicPatternsByLang["systemverilog"] = verilogDynamicPatterns
}
