package resolve

// stdlib_builtins.go — per-language sets of bare-name stdlib targets that
// should NEVER produce a placeholder External entity in the graph. Issue #1085.
//
// These are the "pure stdlib" names: language builtins, core type constructors,
// and unambiguous stdlib methods that cannot be user-defined symbols. They are
// distinct from the dynamic-pattern catalog (which handles reflective dispatch
// and framework DSL names) and from the external allowlist (which handles
// real third-party packages).
//
// The synthesiser (internal/external) calls IsStdlibBuiltinTarget before
// emitting a placeholder entity; a true result means the edge should carry a
// "dynamic_target" property instead, and no entity should be created.
//
// Design rules (per issue #94 lesson):
//   - Only include names that are UNAMBIGUOUSLY a language builtin.
//   - Exclude names that collide with common user-defined methods even within
//     the same language (write, read, close, update, pop, clear, etc.).
//   - Do NOT include names that the per-import gate in classifyExternal folds
//     to a canonical package (e.g. Flask DSL names like "route",
//     "before_request" — those should still flow through to ext:flask).

// pythonStdlibBuiltinNames is the Python-specific set of bare-name stdlib
// targets. Populated here rather than in dynamic_patterns_python.go so it
// stays separate from the dispatch-pattern catalog and doesn't accidentally
// affect cross-language tests that verify catalog disjointness.
var pythonStdlibBuiltinNames = map[string]struct{}{
	// Core builtin functions and type constructors (PEP 3102 / builtins module)
	"abs":       {},
	"all":       {},
	"any":       {},
	"bool":      {},
	"callable":  {},
	"chr":       {},
	"dict":      {},
	"enumerate": {},
	"filter":    {},
	"float":     {},
	"format":    {},
	"frozenset": {},
	// getattr/setattr/hasattr/delattr/eval/exec/__import__ are covered by
	// pythonDynamicPatterns (they are reflective primitives, not simple
	// stdlib builtins). Do NOT duplicate them here.
	"hash":       {},
	"id":         {},
	"int":        {},
	"isinstance": {},
	"issubclass": {},
	"iter":       {},
	"len":        {},
	"list":       {},
	"map":        {},
	"max":        {},
	"min":        {},
	"next":       {},
	"object":     {},
	"open":       {},
	"ord":        {},
	"print":      {},
	"property":   {},
	"range":      {},
	"repr":       {},
	"reversed":   {},
	"round":      {},
	"set":        {},
	"slice":      {},
	"sorted":     {},
	"str":        {},
	"sum":        {},
	"super":      {},
	"tuple":      {},
	"type":       {},
	"vars":       {},
	"zip":        {},
	// Python stdlib exceptions — unambiguously built-in, not user-defined
	"Exception":           {},
	"ValueError":          {},
	"TypeError":           {},
	"KeyError":            {},
	"IndexError":          {},
	"AttributeError":      {},
	"RuntimeError":        {},
	"NotImplementedError": {},
	"StopIteration":       {},
	"FileNotFoundError":   {},
	// High-volume Python str/list/dict/set/file methods (bare-name after
	// receiver strip). Exact match only; collision-prone names (write, read,
	// close, update, pop, clear, append, remove, extend, items, keys, values)
	// deliberately excluded per issue #94 — misclassifying a real local method
	// as a stdlib builtin hides real bugs.
	"insert":     {},
	"setdefault": {},
	"startswith": {},
	"endswith":   {},
	"strip":      {},
	"lstrip":     {},
	"rstrip":     {},
	"split":      {},
	"rsplit":     {},
	"splitlines": {},
	"join":       {},
	"lower":      {},
	"upper":      {},
	"title":      {},
	"encode":     {},
	"decode":     {},
	"isdigit":    {},
	"isalpha":    {},
	"isalnum":    {},
	"readline":   {},
	"readlines":  {},
	"writelines": {},
	// flush, seek, tell — kept; collision-prone names (write/read/close)
	// are excluded above.
	"seek": {},
	"tell": {},
	// Python os/stdlib functions (bare-name, no module qualifier)
	"getcwd":          {},
	"listdir":         {},
	"makedirs":        {},
	"deepcopy":        {},
	"deque":           {},
	"defaultdict":     {},
	"OrderedDict":     {},
	"Counter":         {},
	"namedtuple":      {},
	"RawConfigParser": {},
	"ConfigParser":    {},
	// io module: BytesIO, StringIO appear at high volume and cannot be
	// user-defined under normal Python conventions.
	"BytesIO":  {},
	"StringIO": {},
}

// goStdlibBuiltinNames is the Go-specific set of bare-name builtin targets.
// These are the identifiers declared in the Go universe block — they cannot
// be imported from any package and should never produce a placeholder External
// entity. Type names that collide with common user-defined symbols (string,
// int, int64, byte, bool, float64, error) are intentionally excluded because
// they are often valid type-entity names inside a graph.
var goStdlibBuiltinNames = map[string]struct{}{
	// Built-in functions (spec: https://go.dev/ref/spec#Built-in_functions)
	"make":    {},
	"new":     {},
	"len":     {},
	"cap":     {},
	"append":  {},
	"copy":    {},
	"delete":  {},
	"print":   {},
	"println": {},
	"panic":   {},
	"recover": {},
	"close":   {},
	"complex": {},
	"real":    {},
	"imag":    {},
}

// javascriptStdlibBuiltinNames covers both JavaScript and TypeScript. These are
// the global/built-in objects that are always present in any JS/TS runtime and
// can never be resolved to a user-defined entity or a real npm package.
// Collision-prone names that are common method names in user code are excluded.
var javascriptStdlibBuiltinNames = map[string]struct{}{
	// Core language built-in globals (ECMAScript standard)
	"console":        {},
	"JSON":           {},
	"Math":           {},
	"Object":         {},
	"Array":          {},
	"String":         {},
	"Number":         {},
	"Boolean":        {},
	"Date":           {},
	"RegExp":         {},
	"Map":            {},
	"Set":            {},
	"WeakMap":        {},
	"WeakSet":        {},
	"Promise":        {},
	"Symbol":         {},
	"BigInt":         {},
	"Error":          {},
	"TypeError":      {},
	"RangeError":     {},
	"ReferenceError": {},
	// Browser globals
	"window":                {},
	"document":              {},
	"localStorage":          {},
	"sessionStorage":        {},
	"fetch":                 {},
	"URL":                   {},
	"URLSearchParams":       {},
	"Headers":               {},
	"Request":               {},
	"Response":              {},
	"FormData":              {},
	"Blob":                  {},
	"File":                  {},
	"FileReader":            {},
	"setTimeout":            {},
	"setInterval":           {},
	"clearTimeout":          {},
	"clearInterval":         {},
	"requestAnimationFrame": {},
	"cancelAnimationFrame":  {},
	// Node.js globals
	"process":    {},
	"Buffer":     {},
	"globalThis": {},
	"require":    {},
	"module":     {},
	"__dirname":  {},
	"__filename": {},
}

// rubyStdlibBuiltinNames covers Ruby built-in kernel methods and core stdlib
// classes that cannot be user-defined under Ruby conventions. Collision-prone
// names (String, Integer, Float, Array, Hash — which are valid class entities
// in a user codebase) are excluded deliberately.
var rubyStdlibBuiltinNames = map[string]struct{}{
	// Kernel / top-level output methods
	"puts":  {},
	"print": {},
	"p":     {},
	"pp":    {},
	"raise": {},
	// Class macro methods (Module-level DSL)
	"attr_accessor": {},
	"attr_reader":   {},
	"attr_writer":   {},
	// Forwardable module DSL
	"def_delegators": {},
	"delegate":       {},
	// Core stdlib classes that appear unambiguously as bare-name calls and
	// cannot collide with real user-defined class names under Ruby conventions.
	"Symbol": {},
	"Range":  {},
	"Regexp": {},
	"Time":   {},
	"Date":   {},
}

// javaStdlibBuiltinNames covers unambiguous Java stdlib bare-name symbols.
// Only package-qualified forms that cannot collide with user-defined types
// or common method names are included. java.lang.* symbols visible in every
// class without import (String, Integer, System, Math, Object, Thread, …)
// are intentionally excluded: they are valid user-entity names in a graph.
var javaStdlibBuiltinNames = map[string]struct{}{
	// java.lang top-level bare-name calls that are universally unambiguous.
	"println":           {},
	"printStackTrace":   {},
	"currentTimeMillis": {},
	"nanoTime":          {},
	"arraycopy":         {},
	"gc":                {},
	"exit":              {},
	"getenv":            {},
	// java.util — collection factory methods (Java 9+ static factories)
	"of":          {},
	"ofEntries":   {},
	"copyOf":      {},
	"copyOfRange": {},
	// java.util.Objects bare-name
	"requireNonNull":     {},
	"requireNonNullElse": {},
	"isNull":             {},
	"nonNull":            {},
	"checkIndex":         {},
	// java.util.Arrays bare-name
	"asList":       {},
	"fill":         {},
	"binarySearch": {},
	"deepToString": {},
	// java.util.Collections bare-name
	"emptyList":        {},
	"emptyMap":         {},
	"emptySet":         {},
	"singletonList":    {},
	"unmodifiableList": {},
	"unmodifiableMap":  {},
	"unmodifiableSet":  {},
	"synchronizedList": {},
	"nCopies":          {},
	"frequency":        {},
	"disjoint":         {},
	// java.util.concurrent — bare-name constructors and factory methods
	"newCachedThreadPool":             {},
	"newFixedThreadPool":              {},
	"newSingleThreadExecutor":         {},
	"newScheduledThreadPool":          {},
	"newVirtualThreadPerTaskExecutor": {},
	// java.util.stream bare-name pipeline terminals that appear as leaf stubs
	"toList":         {},
	"toSet":          {},
	"toMap":          {},
	"joining":        {},
	"counting":       {},
	"groupingBy":     {},
	"partitioningBy": {},
	"summarizingInt": {},
	"averagingInt":   {},
	"summingInt":     {},
	"minBy":          {},
	"maxBy":          {},
	// java.math
	"valueOf":        {},
	"parseLong":      {},
	"parseInt":       {},
	"parseDouble":    {},
	"parseFloat":     {},
	"parseByte":      {},
	"parseShort":     {},
	"toBinaryString": {},
	"toHexString":    {},
	"toOctalString":  {},
	"bitCount":       {},
	"reverse":        {},
	"signum":         {},
	"pow":            {},
	"abs":            {},
	"sqrt":           {},
	"floor":          {},
	"ceil":           {},
	"round":          {},
	"min":            {},
	"max":            {},
	"log":            {},
	"log10":          {},
	"sin":            {},
	"cos":            {},
	"tan":            {},
	"random":         {},
	// java.time factory methods
	"now":          {},
	"parse":        {},
	"ofEpochMilli": {},
	"ofInstant":    {},
	"between":      {},
	"toEpochMilli": {},
	// java.io / java.nio factory
	"newInputStream":      {},
	"newOutputStream":     {},
	"newBufferedReader":   {},
	"newBufferedWriter":   {},
	"readAllBytes":        {},
	"readString":          {},
	"writeString":         {},
	"createTempFile":      {},
	"createTempDirectory": {},
	// java.security
	"getInstance":    {},
	"generateSecret": {},
	"generateKey":    {},
	// java.net
	"openConnection":     {},
	"getResponseCode":    {},
	"setRequestMethod":   {},
	"setRequestProperty": {},
}

// kotlinStdlibBuiltinNames covers unambiguous Kotlin stdlib bare-name symbols.
// kotlin.* and kotlinx.* top-level functions that appear as bare leaf stubs
// after receiver-stripping by the Kotlin extractor.
var kotlinStdlibBuiltinNames = map[string]struct{}{
	// kotlin.collections factory functions
	"listOf":        {},
	"mutableListOf": {},
	"arrayListOf":   {},
	"setOf":         {},
	"mutableSetOf":  {},
	"hashSetOf":     {},
	"linkedSetOf":   {},
	"mapOf":         {},
	"mutableMapOf":  {},
	"hashMapOf":     {},
	"linkedMapOf":   {},
	"emptyList":     {},
	"emptySet":      {},
	"emptyMap":      {},
	"emptyArray":    {},
	// kotlin.* top-level functions
	"arrayOf":        {},
	"intArrayOf":     {},
	"longArrayOf":    {},
	"doubleArrayOf":  {},
	"floatArrayOf":   {},
	"booleanArrayOf": {},
	"byteArrayOf":    {},
	"charArrayOf":    {},
	"shortArrayOf":   {},
	"lazy":           {},
	"lazyOf":         {},
	"run":            {},
	"let":            {},
	"also":           {},
	"apply":          {},
	"with":           {},
	"takeIf":         {},
	"takeUnless":     {},
	"repeat":         {},
	"check":          {},
	"require":        {},
	"requireNotNull": {},
	"checkNotNull":   {},
	"error":          {},
	"TODO":           {},
	"println":        {},
	"print":          {},
	"readLine":       {},
	// kotlinx.coroutines top-level stubs
	"launch":            {},
	"async":             {},
	"runBlocking":       {},
	"withContext":       {},
	"delay":             {},
	"flow":              {},
	"collect":           {},
	"emit":              {},
	"channelFlow":       {},
	"callbackFlow":      {},
	"stateFlow":         {},
	"sharedFlow":        {},
	"supervisorScope":   {},
	"coroutineScope":    {},
	"withTimeout":       {},
	"withTimeoutOrNull": {},
	// kotlinx.serialization
	"encodeToString":        {},
	"decodeFromString":      {},
	"encodeToJsonElement":   {},
	"decodeFromJsonElement": {},
}

// scalaStdlibBuiltinNames covers unambiguous Scala stdlib bare-name symbols.
// These are companion-object factory methods and top-level Predef functions.
var scalaStdlibBuiltinNames = map[string]struct{}{
	// Predef / console IO
	"println":    {},
	"print":      {},
	"readLine":   {},
	"readInt":    {},
	"readLong":   {},
	"readDouble": {},
	// scala.util.Try / Option bare constructors (when not qualifier-qualified)
	"Success": {},
	"Failure": {},
	"Some":    {},
	"None":    {},
	// scala.concurrent bare futures
	"Future":  {},
	"Promise": {},
	// scala.math
	"abs":   {},
	"max":   {},
	"min":   {},
	"sqrt":  {},
	"pow":   {},
	"ceil":  {},
	"floor": {},
	"round": {},
	"log":   {},
	// scala.collection bare factory invocations (qualified forms handled by JVM patterns)
	"toList":     {},
	"toSet":      {},
	"toMap":      {},
	"toSeq":      {},
	"toVector":   {},
	"toArray":    {},
	"toIterator": {},
	"toStream":   {},
	// Predef type-conversion ops
	"identity":   {},
	"implicitly": {},
	"locally":    {},
}

// rustStdlibBuiltinNames covers unambiguous Rust stdlib bare-name symbols —
// only the ones that cannot collide with user-defined methods.
// Rust receiver-typed patterns are handled by rustDynamicPatterns; this set
// covers only bare top-level macro-like names.
var rustStdlibBuiltinNames = map[string]struct{}{
	// Rust built-in macros — always top-level, never user-defined
	"println":         {},
	"print":           {},
	"eprintln":        {},
	"eprint":          {},
	"dbg":             {},
	"todo":            {},
	"unimplemented":   {},
	"unreachable":     {},
	"panic":           {},
	"assert":          {},
	"assert_eq":       {},
	"assert_ne":       {},
	"debug_assert":    {},
	"debug_assert_eq": {},
	"debug_assert_ne": {},
	"format":          {},
	"write":           {},
	"writeln":         {},
	"vec":             {},
	"include_str":     {},
	"include_bytes":   {},
	"env":             {},
	"concat":          {},
	"stringify":       {},
	"cfg":             {},
	// std::mem bare stubs
	"size_of":   {},
	"align_of":  {},
	"transmute": {},
	"swap":      {},
	"replace":   {},
	"take":      {},
	"forget":    {},
	"drop":      {},
	// std::cmp
	"min":     {},
	"max":     {},
	"clamp":   {},
	"Reverse": {},
	// std::convert
	"From":    {},
	"Into":    {},
	"TryFrom": {},
	"TryInto": {},
}

// swiftStdlibBuiltinNames covers unambiguous Swift stdlib bare-name symbols.
// Foundation / Combine / SwiftUI / UIKit method stubs are handled by
// swiftDynamicPatterns; this set covers only the Swift standard library
// global functions that cannot collide with user-defined names.
var swiftStdlibBuiltinNames = map[string]struct{}{
	// Swift stdlib global functions.
	// fatalError/precondition/preconditionFailure excluded: they appear in the
	// Vapor DSL bare-name catalog for swift and must not be intercepted here
	// (issue #94 rule — collisions between stdlib and framework DSL names stay
	// in the DSL catalog so the ext:<pkg> placeholder still gets emitted).
	"print":                     {},
	"debugPrint":                {},
	"dump":                      {},
	"readLine":                  {},
	"abs":                       {},
	"min":                       {},
	"max":                       {},
	"swap":                      {},
	"zip":                       {},
	"stride":                    {},
	"strideof":                  {},
	"sizeof":                    {},
	"assert":                    {},
	"assertionFailure":          {},
	"withUnsafePointer":         {},
	"withUnsafeMutablePointer":  {},
	"withUnsafeBytes":           {},
	"withUnsafeMutableBytes":    {},
	"unsafeBitCast":             {},
	"unsafeDowncast":            {},
	"type":                      {}, // type(of:)
	"isKnownUniquelyReferenced": {},
	"autoreleasepool":           {},
}

// csharpStdlibBuiltinNames covers unambiguous C# / .NET stdlib bare-name
// symbols that appear at the top level or as static method stubs.
// System.* and Microsoft.* framework DSL names are handled by
// csharpDynamicPatterns; this set covers only the C# language-level
// global functions and well-known System top-level static stubs.
var csharpStdlibBuiltinNames = map[string]struct{}{
	// Console / debug
	"WriteLine": {},
	"Write":     {},
	"ReadLine":  {},
	"ReadKey":   {},
	"Clear":     {},
	// Math — Min/Max excluded: they collide with EF Core/LINQ query operators
	// that are classified via the csharp bare-name DSL catalog (issue #94 rule).
	"Abs":     {},
	"Pow":     {},
	"Sqrt":    {},
	"Floor":   {},
	"Ceiling": {},
	"Round":   {},
	"Log":     {},
	"Log10":   {},
	"Sin":     {},
	"Cos":     {},
	"Tan":     {},
	"Sign":    {},
	"Clamp":   {},
	// String static methods
	"IsNullOrEmpty":      {},
	"IsNullOrWhiteSpace": {},
	"Concat":             {},
	"Format":             {},
	"Join":               {},
	"Compare":            {},
	"CompareOrdinal":     {},
	"Intern":             {},
	"IsInterned":         {},
	"ReferenceEquals":    {},
	// Convert static methods
	"ToInt32":          {},
	"ToInt64":          {},
	"ToDouble":         {},
	"ToBoolean":        {},
	"ToString":         {},
	"ToBase64String":   {},
	"FromBase64String": {},
	"ChangeType":       {},
	// Enum
	"GetValues": {},
	"GetNames":  {},
	"Parse":     {},
	"TryParse":  {},
	"IsDefined": {},
	"HasFlag":   {},
	// Array / Span — Find excluded: it collides with EF Core's Find query operator.
	"Copy":         {},
	"Resize":       {},
	"Reverse":      {},
	"Sort":         {},
	"BinarySearch": {},
	"Fill":         {},
	"IndexOf":      {},
	"LastIndexOf":  {},
	"Exists":       {},
	"FindAll":      {},
	"FindIndex":    {},
	// GC
	"Collect":          {},
	"GetTotalMemory":   {},
	"SuppressFinalize": {},
	// Guid
	"NewGuid": {},
	"Empty":   {},
	// DateTime
	"Now":         {},
	"UtcNow":      {},
	"Today":       {},
	"FromBinary":  {},
	"DaysInMonth": {},
	"IsLeapYear":  {},
	"SpecifyKind": {},
}

// phpStdlibBuiltinNames covers unambiguous PHP built-in function bare-names.
// PHP has hundreds of built-in functions; only the ones that are universally
// unambiguous (i.e. cannot collide with common user-defined method names) are
// listed here. Collision-prone names (str_replace, array_map, file_get_contents)
// that are common user-function names are deliberately excluded.
var phpStdlibBuiltinNames = map[string]struct{}{
	// String functions
	"strlen":                  {},
	"strtolower":              {},
	"strtoupper":              {},
	"ucfirst":                 {},
	"lcfirst":                 {},
	"ucwords":                 {},
	"trim":                    {},
	"ltrim":                   {},
	"rtrim":                   {},
	"str_pad":                 {},
	"str_repeat":              {},
	"str_word_count":          {},
	"substr":                  {},
	"substr_count":            {},
	"substr_replace":          {},
	"strpos":                  {},
	"strrpos":                 {},
	"strstr":                  {},
	"stristr":                 {},
	"strcmp":                  {},
	"strcasecmp":              {},
	"strncmp":                 {},
	"htmlspecialchars":        {},
	"htmlspecialchars_decode": {},
	"htmlentities":            {},
	"strip_tags":              {},
	"nl2br":                   {},
	"wordwrap":                {},
	"chunk_split":             {},
	"explode":                 {},
	"implode":                 {},
	"sprintf":                 {},
	"printf":                  {},
	"sscanf":                  {},
	"number_format":           {},
	"money_format":            {},
	"md5":                     {},
	"sha1":                    {},
	"crc32":                   {},
	"base64_encode":           {},
	"base64_decode":           {},
	"urlencode":               {},
	"urldecode":               {},
	"rawurlencode":            {},
	"rawurldecode":            {},
	"http_build_query":        {},
	"parse_str":               {},
	// Array functions — count excluded: collides with Laravel query builder count().
	"sizeof":           {},
	"array_keys":       {},
	"array_values":     {},
	"array_merge":      {},
	"array_push":       {},
	"array_pop":        {},
	"array_shift":      {},
	"array_unshift":    {},
	"array_slice":      {},
	"array_splice":     {},
	"array_search":     {},
	"array_unique":     {},
	"array_flip":       {},
	"array_reverse":    {},
	"array_combine":    {},
	"array_diff":       {},
	"array_intersect":  {},
	"array_fill":       {},
	"array_chunk":      {},
	"array_column":     {},
	"array_key_exists": {},
	"in_array":         {},
	"sort":             {},
	"rsort":            {},
	"asort":            {},
	"arsort":           {},
	"ksort":            {},
	"krsort":           {},
	"usort":            {},
	"uasort":           {},
	"uksort":           {},
	"compact":          {},
	"extract":          {},
	"range":            {},
	"shuffle":          {},
	// Math functions
	"abs":          {},
	"ceil":         {},
	"floor":        {},
	"round":        {},
	"max":          {},
	"min":          {},
	"pow":          {},
	"sqrt":         {},
	"log":          {},
	"log10":        {},
	"fmod":         {},
	"intdiv":       {},
	"pi":           {},
	"rand":         {},
	"mt_rand":      {},
	"random_int":   {},
	"random_bytes": {},
	// Type-checking and conversion
	"is_array":   {},
	"is_bool":    {},
	"is_float":   {},
	"is_int":     {},
	"is_null":    {},
	"is_numeric": {},
	"is_object":  {},
	"is_string":  {},
	"isset":      {},
	"empty":      {},
	"unset":      {},
	"intval":     {},
	"floatval":   {},
	"strval":     {},
	"boolval":    {},
	"settype":    {},
	"gettype":    {},
	"var_dump":   {},
	"var_export": {},
	"print_r":    {},
	// I/O
	"fopen":             {},
	"fclose":            {},
	"fread":             {},
	"fwrite":            {},
	"fgets":             {},
	"fputs":             {},
	"feof":              {},
	"fflush":            {},
	"fseek":             {},
	"ftell":             {},
	"rewind":            {},
	"flock":             {},
	"ftruncate":         {},
	"file_get_contents": {},
	"file_put_contents": {},
	"file_exists":       {},
	"is_file":           {},
	"is_dir":            {},
	// mkdir/basename/dirname/realpath excluded: they also appear in Python's
	// stdlib catalog; the cross-lang gate test verifies those names are not
	// suppressed when lang=php, so we cannot add them here (issue #94 rule).
	"rmdir":    {},
	"rename":   {},
	"unlink":   {},
	"copy":     {},
	"glob":     {},
	"scandir":  {},
	"pathinfo": {},
	// JSON
	"json_encode":     {},
	"json_decode":     {},
	"json_last_error": {},
	// Date/time
	"time":        {},
	"microtime":   {},
	"date":        {},
	"strtotime":   {},
	"mktime":      {},
	"checkdate":   {},
	"date_create": {},
	"date_format": {},
	"date_diff":   {},
	// Misc
	"defined":               {},
	"define":                {},
	"constant":              {},
	"function_exists":       {},
	"class_exists":          {},
	"method_exists":         {},
	"property_exists":       {},
	"get_class":             {},
	"get_parent_class":      {},
	"is_a":                  {},
	"instanceof":            {},
	"call_user_func":        {},
	"call_user_func_array":  {},
	"header":                {},
	"headers_sent":          {},
	"ob_start":              {},
	"ob_end_clean":          {},
	"ob_get_clean":          {},
	"ob_flush":              {},
	"session_start":         {},
	"session_destroy":       {},
	"die":                   {},
	"exit":                  {},
	"error_reporting":       {},
	"set_error_handler":     {},
	"trigger_error":         {},
	"debug_backtrace":       {},
	"debug_print_backtrace": {},
}

// elixirStdlibBuiltinNames covers unambiguous Elixir stdlib bare-name
// symbols from Kernel and the core stdlib modules. Framework / OTP names
// are handled by elixirDynamicPatterns; this set covers only the Elixir
// Kernel functions that are auto-imported into every module.
var elixirStdlibBuiltinNames = map[string]struct{}{
	// Kernel auto-imports — always in scope, never user-defined
	"is_integer":    {},
	"is_float":      {},
	"is_binary":     {},
	"is_boolean":    {},
	"is_atom":       {},
	"is_list":       {},
	"is_map":        {},
	"is_tuple":      {},
	"is_function":   {},
	"is_pid":        {},
	"is_port":       {},
	"is_reference":  {},
	"is_struct":     {},
	"is_exception":  {},
	"is_nil":        {},
	"is_number":     {},
	"length":        {},
	"hd":            {},
	"tl":            {},
	"elem":          {},
	"put_elem":      {},
	"tuple_size":    {},
	"map_size":      {},
	"byte_size":     {},
	"bit_size":      {},
	"abs":           {},
	"div":           {},
	"rem":           {},
	"max":           {},
	"min":           {},
	"round":         {},
	"floor":         {},
	"ceil":          {},
	"trunc":         {},
	"to_string":     {},
	"to_charlist":   {},
	"inspect":       {},
	"raise":         {},
	"reraise":       {},
	"throw":         {},
	"exit":          {},
	"send":          {},
	"self":          {},
	"spawn":         {},
	"spawn_link":    {},
	"spawn_monitor": {},
	"apply":         {},
	"make_ref":      {},
	"node":          {},
	"nodes":         {},
	"not":           {},
	"and":           {},
	"or":            {},
	"in":            {},
	"binding":       {},
	"dbg":           {},
	"tap":           {},
	"then":          {},
	// Enum module (very high-volume; Enum.* qualified forms handled by dynamic patterns)
	// bare forms post receiver-strip:
	"map":             {},
	"filter":          {},
	"reduce":          {},
	"each":            {},
	"flat_map":        {},
	"any?":            {},
	"all?":            {},
	"count":           {},
	"find":            {},
	"group_by":        {},
	"sort":            {},
	"sort_by":         {},
	"uniq":            {},
	"zip":             {},
	"into":            {},
	"to_list":         {},
	"member?":         {},
	"empty?":          {},
	"chunk_every":     {},
	"chunk_by":        {},
	"take":            {},
	"drop":            {},
	"take_while":      {},
	"drop_while":      {},
	"with_index":      {},
	"flat_map_reduce": {},
	"split_while":     {},
	"min_by":          {},
	"max_by":          {},
	"sum":             {},
	"product":         {},
	"frequencies":     {},
	"reverse":         {},
	"concat":          {},
	"dedup":           {},
	"join":            {},
	// IO / Logger bare forms
	"puts":     {},
	"gets":     {},
	"write":    {},
	"binread":  {},
	"binwrite": {},
	// String module bare forms
	"upcase":        {},
	"downcase":      {},
	"capitalize":    {},
	"trim":          {},
	"trim_leading":  {},
	"trim_trailing": {},
	"split":         {},
	"starts_with?":  {},
	"ends_with?":    {},
	"contains?":     {},
	"replace":       {},
	"slice":         {},
	"at":            {},
	"graphemes":     {},
	"codepoints":    {},
	"valid?":        {},
	"pad_leading":   {},
	"pad_trailing":  {},
	"match?":        {},
	// Map module bare forms
	"new":         {},
	"put":         {},
	"get":         {},
	"delete":      {},
	"merge":       {},
	"update":      {},
	"has_key?":    {},
	"keys":        {},
	"values":      {},
	"from_struct": {},
	"equal?":      {},
	"intersect?":  {},
	"pop":         {},
	"fetch":       {},
	"fetch!":      {},
	// (take/drop/split/zip covered by Enum section above)
	// List module bare forms
	"flatten":    {},
	"last":       {},
	"first":      {},
	"wrap":       {},
	"duplicate":  {},
	"unzip":      {},
	"keydelete":  {},
	"keyreplace": {},
	"keymember?": {},
	"keyfind":    {},
	"keystore":   {},
	"keytake":    {},
}

// clojureStdlibBuiltinNames covers unambiguous Clojure clojure.core bare-name
// symbols. Framework / OTP names are handled by clojureDynamicPatterns; this
// set covers only the Clojure core functions that are always in scope.
var clojureStdlibBuiltinNames = map[string]struct{}{
	// clojure.core collection functions
	"conj":         {},
	"cons":         {},
	"assoc":        {},
	"dissoc":       {},
	"merge":        {},
	"get":          {},
	"get-in":       {},
	"assoc-in":     {},
	"update":       {},
	"update-in":    {},
	"select-keys":  {},
	"keys":         {},
	"vals":         {},
	"count":        {},
	"empty?":       {},
	"seq":          {},
	"first":        {},
	"rest":         {},
	"last":         {},
	"next":         {},
	"ffirst":       {},
	"second":       {},
	"nth":          {},
	"take":         {},
	"drop":         {},
	"take-while":   {},
	"drop-while":   {},
	"filter":       {},
	"filterv":      {},
	"remove":       {},
	"map":          {},
	"mapv":         {},
	"map-indexed":  {},
	"keep":         {},
	"keep-indexed": {},
	"reduce":       {},
	"reduce-kv":    {},
	"run!":         {},
	"doseq":        {},
	"for":          {},
	"some":         {},
	"every?":       {},
	"any?":         {},
	"not-any?":     {},
	"not-every?":   {},
	"contains?":    {},
	"empty":        {},
	"not-empty":    {},
	"vec":          {},
	"vector":       {},
	"hash-map":     {},
	"hash-set":     {},
	"sorted-map":   {},
	"sorted-set":   {},
	"list":         {},
	"into":         {},
	"flatten":      {},
	"concat":       {},
	"interpose":    {},
	"interleave":   {},
	"partition":    {},
	"partition-by": {},
	"group-by":     {},
	"frequencies":  {},
	"distinct":     {},
	"dedupe":       {},
	"sort":         {},
	"sort-by":      {},
	"reverse":      {},
	"shuffle":      {},
	"rand-nth":     {},
	"subvec":       {},
	"split-at":     {},
	"split-with":   {},
	"zip":          {},
	"zipmap":       {},
	// clojure.core IO / system
	"println":     {},
	"print":       {},
	"prn":         {},
	"pr":          {},
	"pr-str":      {},
	"str":         {},
	"format":      {},
	"read-string": {},
	"load-string": {},
	"slurp":       {},
	"spit":        {},
	"rand":        {},
	"rand-int":    {},
	"gensym":      {},
	"identity":    {},
	"constantly":  {},
	"juxt":        {},
	"partial":     {},
	"comp":        {},
	"memoize":     {},
	"complement":  {},
	"fnil":        {},
	"apply":       {},
	"eval":        {},
	"macroexpand": {},
	// clojure.core type predicates
	"nil?":     {},
	"boolean?": {},
	"number?":  {},
	"integer?": {},
	"float?":   {},
	"string?":  {},
	"symbol?":  {},
	"keyword?": {},
	"vector?":  {},
	"map?":     {},
	"set?":     {},
	"list?":    {},
	"seq?":     {},
	"coll?":    {},
	"fn?":      {},
	"ifn?":     {},
	"var?":     {},
	// clojure.core type coercion
	"int":       {},
	"long":      {},
	"float":     {},
	"double":    {},
	"num":       {},
	"char":      {},
	"boolean":   {},
	"name":      {},
	"namespace": {},
	"keyword":   {},
	"symbol":    {},
	// clojure.core arithmetic
	"inc":   {},
	"dec":   {},
	"mod":   {},
	"quot":  {},
	"rem":   {},
	"abs":   {},
	"max":   {},
	"min":   {},
	"zero?": {},
	"pos?":  {},
	"neg?":  {},
	"even?": {},
	"odd?":  {},
	// clojure.core string
	"subs": {},
	// java.lang interop — bare calls from Clojure land
	"Math/abs":                 {},
	"Math/max":                 {},
	"Math/min":                 {},
	"Math/pow":                 {},
	"Math/sqrt":                {},
	"Math/floor":               {},
	"Math/ceil":                {},
	"Math/round":               {},
	"Math/log":                 {},
	"Math/log10":               {},
	"Math/sin":                 {},
	"Math/cos":                 {},
	"Math/tan":                 {},
	"Math/random":              {},
	"System/currentTimeMillis": {},
	"System/exit":              {},
	"System/getenv":            {},
}

// erlangStdlibBuiltinNames covers unambiguous Erlang stdlib bare-name
// symbols — only the BIFs auto-imported into every Erlang module. OTP-
// qualified forms (gen_server:*, lists:*, maps:*) are handled by
// erlangDynamicPatterns via the qualified-form catch-all.
var erlangStdlibBuiltinNames = map[string]struct{}{
	// Erlang auto-imported BIFs (erlang:* available without qualification)
	"self":            {},
	"spawn":           {},
	"spawn_link":      {},
	"spawn_monitor":   {},
	"make_ref":        {},
	"whereis":         {},
	"register":        {},
	"unregister":      {},
	"monitor":         {},
	"demonitor":       {},
	"send":            {},
	"halt":            {},
	"abs":             {},
	"hd":              {},
	"tl":              {},
	"length":          {},
	"node":            {},
	"nodes":           {},
	"size":            {},
	"tuple_size":      {},
	"byte_size":       {},
	"bit_size":        {},
	"map_size":        {},
	"element":         {},
	"setelement":      {},
	"tuple_to_list":   {},
	"list_to_tuple":   {},
	"throw":           {},
	"exit":            {},
	"apply":           {},
	"round":           {},
	"trunc":           {},
	"floor":           {},
	"ceil":            {},
	"float":           {},
	"max":             {},
	"min":             {},
	"integer_to_list": {},
	"list_to_integer": {},
	"float_to_list":   {},
	"list_to_float":   {},
	"atom_to_list":    {},
	"list_to_atom":    {},
	"binary_to_list":  {},
	"list_to_binary":  {},
	"atom_to_binary":  {},
	"binary_to_atom":  {},
	"term_to_binary":  {},
	"binary_to_term":  {},
}

// stdlibBuiltinsByLang maps a normalised language tag to its per-language
// stdlib-builtin name set. Only languages with a non-trivial builtin surface
// that produces significant External entity noise AND that do NOT already have
// a per-language bare-name catalog in internal/external.classifyExternal are
// listed here.
//
// Languages with existing per-language bare-name catalogs in classifyExternal
// (go, javascript/typescript, python, ruby, java, kotlin, scala, rust, swift,
// csharp) are handled upstream by those catalogs which emit ext:<name>
// placeholders. Adding them here would intercept them BEFORE classifyExternal
// and change the observable behavior (Synthesized count goes to 0 instead of 1),
// breaking tests that verify the existing ext:<name> emission.
//
// The new entries (php, elixir, clojure, erlang) do not have per-language
// catalogs in classifyExternal and would otherwise generate noisy placeholder
// External entities for every stdlib call. Issue #1085 multi-lang extension.
var stdlibBuiltinsByLang = map[string]map[string]struct{}{
	"python":     pythonStdlibBuiltinNames,
	"go":         goStdlibBuiltinNames,
	"javascript": javascriptStdlibBuiltinNames,
	"typescript": javascriptStdlibBuiltinNames, // same set; TS is a JS superset
	"ruby":       rubyStdlibBuiltinNames,
	// Scripting — no existing per-language catalog in classifyExternal.
	"php": phpStdlibBuiltinNames,
	// BEAM family — no existing per-language catalog in classifyExternal.
	"elixir":  elixirStdlibBuiltinNames,
	"clojure": clojureStdlibBuiltinNames,
	"erlang":  erlangStdlibBuiltinNames,
	// NOTE: java, kotlin, scala, rust, swift, csharp intentionally omitted
	// here — they are already handled by per-language bare-name catalogs in
	// internal/external.classifyExternal. Their stdlib sets are declared above
	// as documentation and for use via RegisterExtraStdlibFilter.
}

// extraStdlibFilter is a user-extensible set of additional names to suppress.
// It is keyed by normalised language tag → set of bare names. Populated via
// RegisterExtraStdlibFilter (called from the daemon / config loader when the
// group config carries an extra_stdlib_filter table). Issue #1206.
var extraStdlibFilter = map[string]map[string]struct{}{}

// RegisterExtraStdlibFilter merges user-supplied names into the extra filter
// table. Safe to call from init or from the daemon after loading group config.
// Callers pass the raw (un-normalised) language tag; normalisation is applied
// internally so the lookup always matches.
//
// Concurrency: not goroutine-safe during indexing; call before Synthesize.
func RegisterExtraStdlibFilter(lang string, names []string) {
	nl := normalizeLang(lang)
	if nl == "" {
		return
	}
	set, ok := extraStdlibFilter[nl]
	if !ok {
		set = make(map[string]struct{}, len(names))
		extraStdlibFilter[nl] = set
	}
	for _, n := range names {
		if n != "" {
			set[n] = struct{}{}
		}
	}
}

// IsStdlibBuiltinTarget reports whether stub is an unambiguous stdlib builtin
// for the given language — i.e. a bare-name call that should NEVER produce a
// placeholder External entity. The caller (internal/external.Synthesize) uses
// this to stamp "dynamic_target" on the edge and skip entity creation.
//
// Returns false for empty/unknown languages and for names that are not in the
// per-language stdlib-builtin set (those continue through classifyExternal so
// real third-party packages still get their placeholder entities).
//
// User-supplied names from RegisterExtraStdlibFilter are checked after the
// built-in table, allowing per-group suppression of framework stdlibs (e.g.
// django.contrib.auth names). Issue #1206.
func IsStdlibBuiltinTarget(stub, lang string) bool {
	if stub == "" || lang == "" {
		return false
	}
	nl := normalizeLang(lang)
	builtins, ok := stdlibBuiltinsByLang[nl]
	if ok {
		if _, found := builtins[stub]; found {
			return true
		}
	}
	// Check user-supplied extra filter.
	if extra, ok := extraStdlibFilter[nl]; ok {
		if _, found := extra[stub]; found {
			return true
		}
	}
	return false
}
