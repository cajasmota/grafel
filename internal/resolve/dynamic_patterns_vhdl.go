package resolve

import "regexp"

// vhdlDynamicPatterns are per-language patterns for VHDL.
// Registered for "vhdl" via init().
//
// The VHDL extractor emits USES edges whose ToID is a component type name
// found in component instantiation statements ("inst : CompType port map (...)").
// The following categories should be resolved as Dynamic (not expected to
// resolve to an in-tree entity):
//
//  1. IEEE standard libraries — std_logic_1164, numeric_std, std_logic_arith,
//     std_logic_unsigned, std_logic_signed, math_real, math_complex.
//     These are part of the IEEE library and will never appear as in-tree entities.
//
//  2. VITAL (VHDL Initiative Towards ASIC Libraries) primitives — used for
//     ASIC/FPGA timing simulation.  Names follow the VITAL_Primitives /
//     VITAL_Timing naming convention.
//
//  3. Common synthesisable conversion/arithmetic functions that appear as
//     component-lookalike calls in older VHDL code — to_integer, to_unsigned,
//     to_signed, resize, unsigned, signed.  In VHDL these are functions, not
//     components, but older code using component configurations may expose them.
//
//  4. Simulation support: assert / report are VHDL keywords that the regex may
//     occasionally surface as a component label if the design is unusual.
//
//  5. Well-known vendor IP primitive prefixes — UNISIM/Xilinx, ALTERA/Intel,
//     SkyWater PDK sky130, Mentor/Cadence generic std-cell prefixes.
var vhdlDynamicPatterns = []*regexp.Regexp{
	// ── 1. IEEE standard packages ─────────────────────────────────────────
	regexp.MustCompile(`^std_logic_1164$`),    // IEEE.std_logic_1164
	regexp.MustCompile(`^std_logic_arith$`),   // IEEE.std_logic_arith (obsolete but common)
	regexp.MustCompile(`^std_logic_unsigned$`), // IEEE.std_logic_unsigned (obsolete)
	regexp.MustCompile(`^std_logic_signed$`),  // IEEE.std_logic_signed (obsolete)
	regexp.MustCompile(`^numeric_std$`),       // IEEE.numeric_std (modern replacement)
	regexp.MustCompile(`^numeric_bit$`),       // IEEE.numeric_bit
	regexp.MustCompile(`^math_real$`),         // IEEE.math_real
	regexp.MustCompile(`^math_complex$`),      // IEEE.math_complex
	regexp.MustCompile(`^std_textio$`),        // STD.textio
	regexp.MustCompile(`^textio$`),            // shorthand alias

	// ── 2. VITAL primitives ───────────────────────────────────────────────
	regexp.MustCompile(`^VITAL_`),   // VITAL_Primitives, VITAL_Timing, etc.
	regexp.MustCompile(`^vital_`),   // lowercase variant
	regexp.MustCompile(`^VITALComp`), // VITALComponent (Synopsys naming)

	// ── 3. Synthesisable conversion functions (may surface as identifiers) ─
	regexp.MustCompile(`^to_integer$`),
	regexp.MustCompile(`^to_unsigned$`),
	regexp.MustCompile(`^to_signed$`),
	regexp.MustCompile(`^to_std_logic_vector$`),
	regexp.MustCompile(`^to_bit_vector$`),
	regexp.MustCompile(`^to_bit$`),
	regexp.MustCompile(`^resize$`),
	regexp.MustCompile(`^unsigned$`),
	regexp.MustCompile(`^signed$`),
	regexp.MustCompile(`^std_logic_vector$`),
	regexp.MustCompile(`^std_ulogic_vector$`),

	// ── 4. Simulation / assertion identifiers ─────────────────────────────
	regexp.MustCompile(`^assert$`),
	regexp.MustCompile(`^report$`),
	regexp.MustCompile(`^severity$`),

	// ── 5. Vendor IP primitive prefixes ──────────────────────────────────
	// Xilinx/Vivado UNISIM primitives.
	regexp.MustCompile(`^BUFG`),    // BUFG, BUFGCE, BUFGMUX
	regexp.MustCompile(`^IBUF`),    // IBUF, IBUFDS, IBUFGDS
	regexp.MustCompile(`^OBUF`),    // OBUF, OBUFT, OBUFDS
	regexp.MustCompile(`^IOBUF`),   // IOBUF, IOBUFDS
	regexp.MustCompile(`^MMCM`),    // MMCME2_ADV, MMCME4_ADV
	regexp.MustCompile(`^PLL`),     // PLLE2_ADV, PLLE4_ADV
	regexp.MustCompile(`^BRAM_`),   // BRAM_SDP_MACRO, BRAM_TDP_MACRO
	regexp.MustCompile(`^RAMB`),    // RAMB16, RAMB18E2
	regexp.MustCompile(`^LUT[1-6]$`), // LUT1 … LUT6
	regexp.MustCompile(`^FDRE$`),   // D flip-flop with reset
	regexp.MustCompile(`^FDSE$`),   // D flip-flop with set
	regexp.MustCompile(`^FDCE$`),   // D flip-flop CE
	regexp.MustCompile(`^FDPE$`),   // D flip-flop PE
	regexp.MustCompile(`^LDCE$`),   // Latch with CE
	regexp.MustCompile(`^DSP48`),   // DSP48E1, DSP48E2
	// SkyWater PDK standard cells.
	regexp.MustCompile(`^sky130_`),
	// Intel/Altera Quartus primitives.
	regexp.MustCompile(`^altera_`),
	regexp.MustCompile(`^cyclone`),
	regexp.MustCompile(`^stratix`),
	regexp.MustCompile(`^GLOBAL$`),
	// Generic ASIC std-cell prefixes.
	regexp.MustCompile(`^SC_`),
	regexp.MustCompile(`^gf_`),  // GlobalFoundries primitives
	regexp.MustCompile(`^tsmc_`), // TSMC IP cells
}

func init() {
	dynamicPatternsByLang["vhdl"] = vhdlDynamicPatterns
}
