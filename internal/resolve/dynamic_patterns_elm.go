package resolve

import "regexp"

// elmDynamicPatterns are per-language patterns for Elm.
// Registered via init() into dynamicPatternsByLang.
//
// The Elm extractor (internal/extractors/elm/extractor.go) emits CALLS edges
// whose ToID is either a bare function name or a qualified Module.function form.
//
// Three categories of patterns:
//
//  1. Elm core stdlib — List, Dict, Maybe, Result, String, Tuple, Set, Array
//     functions that appear in every Elm codebase as qualified calls.
//     These are external package functions never indexed in-tree.
//
//  2. Browser / Html / Html.Attributes / Html.Events — TEA architecture
//     functions that appear in virtually every Elm app. Gated to lang=="elm"
//     to avoid collisions with generic "div", "text", "class" identifiers in
//     CSS/HTML extractors.
//
//  3. JSON (Json.Decode / Json.Encode) — commonly used in Elm apps for
//     port communication and HTTP. Pattern-matched on qualified forms.
//
// All patterns are gated to lang=="elm" (safer-bias rule).
var elmDynamicPatterns = []*regexp.Regexp{
	// ── List ────────────────────────────────────────────────────────────
	regexp.MustCompile(`^List\.map$`),         // List.map : (a -> b) -> List a -> List b
	regexp.MustCompile(`^List\.filter$`),      // List.filter : (a -> Bool) -> List a -> List a
	regexp.MustCompile(`^List\.foldl$`),       // List.foldl : (a -> b -> b) -> b -> List a -> b
	regexp.MustCompile(`^List\.foldr$`),       // List.foldr : (a -> b -> b) -> b -> List a -> b
	regexp.MustCompile(`^List\.length$`),      // List.length : List a -> Int
	regexp.MustCompile(`^List\.head$`),        // List.head : List a -> Maybe a
	regexp.MustCompile(`^List\.tail$`),        // List.tail : List a -> Maybe (List a)
	regexp.MustCompile(`^List\.isEmpty$`),     // List.isEmpty : List a -> Bool
	regexp.MustCompile(`^List\.member$`),      // List.member : a -> List a -> Bool
	regexp.MustCompile(`^List\.append$`),      // List.append : List a -> List a -> List a
	regexp.MustCompile(`^List\.concat$`),      // List.concat : List (List a) -> List a
	regexp.MustCompile(`^List\.concatMap$`),   // List.concatMap : (a -> List b) -> List a -> List b
	regexp.MustCompile(`^List\.reverse$`),     // List.reverse : List a -> List a
	regexp.MustCompile(`^List\.sort$`),        // List.sort : List comparable -> List comparable
	regexp.MustCompile(`^List\.sortBy$`),      // List.sortBy : (a -> comparable) -> List a -> List a
	regexp.MustCompile(`^List\.sortWith$`),    // List.sortWith : (a -> a -> Order) -> List a -> List a
	regexp.MustCompile(`^List\.take$`),        // List.take : Int -> List a -> List a
	regexp.MustCompile(`^List\.drop$`),        // List.drop : Int -> List a -> List a
	regexp.MustCompile(`^List\.partition$`),   // List.partition : (a -> Bool) -> List a -> (List a, List a)
	regexp.MustCompile(`^List\.unzip$`),       // List.unzip : List (a, b) -> (List a, List b)
	regexp.MustCompile(`^List\.indexedMap$`),  // List.indexedMap : (Int -> a -> b) -> List a -> List b
	regexp.MustCompile(`^List\.filterMap$`),   // List.filterMap : (a -> Maybe b) -> List a -> List b
	regexp.MustCompile(`^List\.map2$`),        // List.map2 : (a -> b -> c) -> List a -> List b -> List c
	regexp.MustCompile(`^List\.range$`),       // List.range : Int -> Int -> List Int
	regexp.MustCompile(`^List\.repeat$`),      // List.repeat : Int -> a -> List a
	regexp.MustCompile(`^List\.singleton$`),   // List.singleton : a -> List a
	regexp.MustCompile(`^List\.sum$`),         // List.sum : List number -> number
	regexp.MustCompile(`^List\.product$`),     // List.product : List number -> number
	regexp.MustCompile(`^List\.maximum$`),     // List.maximum : List comparable -> Maybe comparable
	regexp.MustCompile(`^List\.minimum$`),     // List.minimum : List comparable -> Maybe comparable
	regexp.MustCompile(`^List\.all$`),         // List.all : (a -> Bool) -> List a -> Bool
	regexp.MustCompile(`^List\.any$`),         // List.any : (a -> Bool) -> List a -> Bool
	regexp.MustCompile(`^List\.intersperse$`), // List.intersperse : a -> List a -> List a
	regexp.MustCompile(`^List\.map3$`),        // List.map3
	regexp.MustCompile(`^List\.map4$`),        // List.map4
	regexp.MustCompile(`^List\.map5$`),        // List.map5

	// ── Dict ─────────────────────────────────────────────────────────────
	regexp.MustCompile(`^Dict\.get$`),       // Dict.get : comparable -> Dict comparable v -> Maybe v
	regexp.MustCompile(`^Dict\.insert$`),    // Dict.insert : comparable -> v -> Dict comparable v -> Dict comparable v
	regexp.MustCompile(`^Dict\.remove$`),    // Dict.remove : comparable -> Dict comparable v -> Dict comparable v
	regexp.MustCompile(`^Dict\.update$`),    // Dict.update : comparable -> (Maybe v -> Maybe v) -> Dict comparable v -> Dict comparable v
	regexp.MustCompile(`^Dict\.empty$`),     // Dict.empty : Dict k v
	regexp.MustCompile(`^Dict\.singleton$`), // Dict.singleton : k -> v -> Dict k v
	regexp.MustCompile(`^Dict\.isEmpty$`),   // Dict.isEmpty : Dict k v -> Bool
	regexp.MustCompile(`^Dict\.member$`),    // Dict.member : comparable -> Dict comparable v -> Bool
	regexp.MustCompile(`^Dict\.size$`),      // Dict.size : Dict k v -> Int
	regexp.MustCompile(`^Dict\.keys$`),      // Dict.keys : Dict k v -> List k
	regexp.MustCompile(`^Dict\.values$`),    // Dict.values : Dict k v -> List v
	regexp.MustCompile(`^Dict\.toList$`),    // Dict.toList : Dict k v -> List (k, v)
	regexp.MustCompile(`^Dict\.fromList$`),  // Dict.fromList : List (comparable, v) -> Dict comparable v
	regexp.MustCompile(`^Dict\.map$`),       // Dict.map : (k -> a -> b) -> Dict k a -> Dict k b
	regexp.MustCompile(`^Dict\.filter$`),    // Dict.filter : (comparable -> v -> Bool) -> Dict comparable v -> Dict comparable v
	regexp.MustCompile(`^Dict\.foldl$`),     // Dict.foldl
	regexp.MustCompile(`^Dict\.foldr$`),     // Dict.foldr
	regexp.MustCompile(`^Dict\.partition$`), // Dict.partition
	regexp.MustCompile(`^Dict\.union$`),     // Dict.union
	regexp.MustCompile(`^Dict\.intersect$`), // Dict.intersect
	regexp.MustCompile(`^Dict\.diff$`),      // Dict.diff
	regexp.MustCompile(`^Dict\.merge$`),     // Dict.merge

	// ── Maybe ────────────────────────────────────────────────────────────
	regexp.MustCompile(`^Maybe\.withDefault$`), // Maybe.withDefault : a -> Maybe a -> a
	regexp.MustCompile(`^Maybe\.map$`),         // Maybe.map : (a -> b) -> Maybe a -> Maybe b
	regexp.MustCompile(`^Maybe\.map2$`),        // Maybe.map2
	regexp.MustCompile(`^Maybe\.map3$`),        // Maybe.map3
	regexp.MustCompile(`^Maybe\.andThen$`),     // Maybe.andThen : (a -> Maybe b) -> Maybe a -> Maybe b
	regexp.MustCompile(`^Maybe\.andMap$`),      // Maybe.andMap

	// ── Result ───────────────────────────────────────────────────────────
	regexp.MustCompile(`^Result\.andThen$`),     // Result.andThen : (a -> Result e b) -> Result e a -> Result e b
	regexp.MustCompile(`^Result\.map$`),         // Result.map : (a -> b) -> Result e a -> Result e b
	regexp.MustCompile(`^Result\.mapError$`),    // Result.mapError : (e -> f) -> Result e a -> Result f a
	regexp.MustCompile(`^Result\.withDefault$`), // Result.withDefault : a -> Result e a -> a
	regexp.MustCompile(`^Result\.toMaybe$`),     // Result.toMaybe : Result e a -> Maybe a
	regexp.MustCompile(`^Result\.fromMaybe$`),   // Result.fromMaybe : e -> Maybe a -> Result e a

	// ── String ───────────────────────────────────────────────────────────
	regexp.MustCompile(`^String\.fromInt$`),    // String.fromInt : Int -> String
	regexp.MustCompile(`^String\.fromFloat$`),  // String.fromFloat : Float -> String
	regexp.MustCompile(`^String\.toInt$`),      // String.toInt : String -> Maybe Int
	regexp.MustCompile(`^String\.toFloat$`),    // String.toFloat : String -> Maybe Float
	regexp.MustCompile(`^String\.split$`),      // String.split : String -> String -> List String
	regexp.MustCompile(`^String\.join$`),       // String.join : String -> List String -> String
	regexp.MustCompile(`^String\.words$`),      // String.words : String -> List String
	regexp.MustCompile(`^String\.lines$`),      // String.lines : String -> List String
	regexp.MustCompile(`^String\.length$`),     // String.length : String -> Int
	regexp.MustCompile(`^String\.isEmpty$`),    // String.isEmpty : String -> Bool
	regexp.MustCompile(`^String\.trim$`),       // String.trim : String -> String
	regexp.MustCompile(`^String\.trimLeft$`),   // String.trimLeft : String -> String
	regexp.MustCompile(`^String\.trimRight$`),  // String.trimRight : String -> String
	regexp.MustCompile(`^String\.append$`),     // String.append : String -> String -> String
	regexp.MustCompile(`^String\.concat$`),     // String.concat : List String -> String
	regexp.MustCompile(`^String\.contains$`),   // String.contains : String -> String -> Bool
	regexp.MustCompile(`^String\.startsWith$`), // String.startsWith : String -> String -> Bool
	regexp.MustCompile(`^String\.endsWith$`),   // String.endsWith : String -> String -> Bool
	regexp.MustCompile(`^String\.replace$`),    // String.replace : String -> String -> String -> String
	regexp.MustCompile(`^String\.slice$`),      // String.slice : Int -> Int -> String -> String
	regexp.MustCompile(`^String\.left$`),       // String.left : Int -> String -> String
	regexp.MustCompile(`^String\.right$`),      // String.right : Int -> String -> String
	regexp.MustCompile(`^String\.dropLeft$`),   // String.dropLeft : Int -> String -> String
	regexp.MustCompile(`^String\.dropRight$`),  // String.dropRight : Int -> String -> String
	regexp.MustCompile(`^String\.toLower$`),    // String.toLower : String -> String
	regexp.MustCompile(`^String\.toUpper$`),    // String.toUpper : String -> String
	regexp.MustCompile(`^String\.toList$`),     // String.toList : String -> List Char
	regexp.MustCompile(`^String\.fromList$`),   // String.fromList : List Char -> String
	regexp.MustCompile(`^String\.map$`),        // String.map
	regexp.MustCompile(`^String\.filter$`),     // String.filter
	regexp.MustCompile(`^String\.foldl$`),      // String.foldl
	regexp.MustCompile(`^String\.foldr$`),      // String.foldr
	regexp.MustCompile(`^String\.any$`),        // String.any
	regexp.MustCompile(`^String\.all$`),        // String.all
	regexp.MustCompile(`^String\.indices$`),    // String.indices
	regexp.MustCompile(`^String\.indexes$`),    // String.indexes
	regexp.MustCompile(`^String\.uncons$`),     // String.uncons

	// ── Browser / Browser.Navigation ────────────────────────────────────
	regexp.MustCompile(`^Browser\.sandbox$`),     // Browser.sandbox : { ... } -> Program flags model msg
	regexp.MustCompile(`^Browser\.element$`),     // Browser.element : { ... } -> Program flags model msg
	regexp.MustCompile(`^Browser\.document$`),    // Browser.document : { ... } -> Program flags model msg
	regexp.MustCompile(`^Browser\.application$`), // Browser.application : { ... } -> Program flags model msg

	// ── Html ─────────────────────────────────────────────────────────────
	regexp.MustCompile(`^Html\.div$`),    // Html.div : List (Attribute msg) -> List (Html msg) -> Html msg
	regexp.MustCompile(`^Html\.span$`),   // Html.span
	regexp.MustCompile(`^Html\.text$`),   // Html.text : String -> Html msg
	regexp.MustCompile(`^Html\.button$`), // Html.button
	regexp.MustCompile(`^Html\.input$`),  // Html.input
	regexp.MustCompile(`^Html\.p$`),      // Html.p
	regexp.MustCompile(`^Html\.h1$`),     // Html.h1
	regexp.MustCompile(`^Html\.h2$`),     // Html.h2
	regexp.MustCompile(`^Html\.h3$`),     // Html.h3
	regexp.MustCompile(`^Html\.ul$`),     // Html.ul
	regexp.MustCompile(`^Html\.ol$`),     // Html.ol
	regexp.MustCompile(`^Html\.li$`),     // Html.li
	regexp.MustCompile(`^Html\.a$`),      // Html.a
	regexp.MustCompile(`^Html\.img$`),    // Html.img
	regexp.MustCompile(`^Html\.form$`),   // Html.form
	regexp.MustCompile(`^Html\.label$`),  // Html.label
	regexp.MustCompile(`^Html\.map$`),    // Html.map : (a -> b) -> Html a -> Html b
	regexp.MustCompile(`^Html\.node$`),   // Html.node : String -> List (Attribute msg) -> List (Html msg) -> Html msg

	// ── Html.Attributes ──────────────────────────────────────────────────
	regexp.MustCompile(`^Attributes\.class$`),       // Attributes.class
	regexp.MustCompile(`^Attributes\.id$`),          // Attributes.id
	regexp.MustCompile(`^Attributes\.style$`),       // Attributes.style
	regexp.MustCompile(`^Attributes\.type_$`),       // Attributes.type_
	regexp.MustCompile(`^Attributes\.value$`),       // Attributes.value
	regexp.MustCompile(`^Attributes\.placeholder$`), // Attributes.placeholder
	regexp.MustCompile(`^Attributes\.href$`),        // Attributes.href
	regexp.MustCompile(`^Attributes\.src$`),         // Attributes.src
	regexp.MustCompile(`^Attributes\.disabled$`),    // Attributes.disabled
	regexp.MustCompile(`^Attributes\.checked$`),     // Attributes.checked
	regexp.MustCompile(`^Html\.Attributes\.class$`),
	regexp.MustCompile(`^Html\.Attributes\.id$`),
	regexp.MustCompile(`^Html\.Attributes\.style$`),
	regexp.MustCompile(`^Html\.Attributes\.type_$`),
	regexp.MustCompile(`^Html\.Attributes\.value$`),
	regexp.MustCompile(`^Html\.Attributes\.href$`),
	regexp.MustCompile(`^Html\.Attributes\.src$`),

	// ── Html.Events ──────────────────────────────────────────────────────
	regexp.MustCompile(`^Events\.onClick$`),     // Events.onClick
	regexp.MustCompile(`^Events\.onInput$`),     // Events.onInput
	regexp.MustCompile(`^Events\.onSubmit$`),    // Events.onSubmit
	regexp.MustCompile(`^Events\.onMouseOver$`), // Events.onMouseOver
	regexp.MustCompile(`^Events\.onMouseOut$`),  // Events.onMouseOut
	regexp.MustCompile(`^Events\.onFocus$`),     // Events.onFocus
	regexp.MustCompile(`^Events\.onBlur$`),      // Events.onBlur
	regexp.MustCompile(`^Html\.Events\.onClick$`),
	regexp.MustCompile(`^Html\.Events\.onInput$`),
	regexp.MustCompile(`^Html\.Events\.onSubmit$`),

	// ── Json.Decode ───────────────────────────────────────────────────────
	regexp.MustCompile(`^Decode\.field$`),        // Decode.field : String -> Decoder a -> Decoder a
	regexp.MustCompile(`^Decode\.at$`),           // Decode.at : List String -> Decoder a -> Decoder a
	regexp.MustCompile(`^Decode\.map$`),          // Decode.map : (a -> b) -> Decoder a -> Decoder b
	regexp.MustCompile(`^Decode\.map2$`),         // Decode.map2
	regexp.MustCompile(`^Decode\.map3$`),         // Decode.map3
	regexp.MustCompile(`^Decode\.andThen$`),      // Decode.andThen
	regexp.MustCompile(`^Decode\.succeed$`),      // Decode.succeed : a -> Decoder a
	regexp.MustCompile(`^Decode\.fail$`),         // Decode.fail : String -> Decoder a
	regexp.MustCompile(`^Decode\.string$`),       // Decode.string : Decoder String
	regexp.MustCompile(`^Decode\.int$`),          // Decode.int : Decoder Int
	regexp.MustCompile(`^Decode\.float$`),        // Decode.float : Decoder Float
	regexp.MustCompile(`^Decode\.bool$`),         // Decode.bool : Decoder Bool
	regexp.MustCompile(`^Decode\.list$`),         // Decode.list : Decoder a -> Decoder (List a)
	regexp.MustCompile(`^Decode\.nullable$`),     // Decode.nullable : Decoder a -> Decoder (Maybe a)
	regexp.MustCompile(`^Decode\.maybe$`),        // Decode.maybe : Decoder a -> Decoder (Maybe a)
	regexp.MustCompile(`^Decode\.decodeString$`), // Decode.decodeString : Decoder a -> String -> Result Error a
	regexp.MustCompile(`^Decode\.decodeValue$`),  // Decode.decodeValue : Decoder a -> Value -> Result Error a
	regexp.MustCompile(`^decodeString$`),         // bare decodeString when imported exposing(..)
	regexp.MustCompile(`^Json\.Decode\.field$`),
	regexp.MustCompile(`^Json\.Decode\.decodeString$`),

	// ── Json.Encode ───────────────────────────────────────────────────────
	regexp.MustCompile(`^Encode\.object$`), // Encode.object : List (String, Value) -> Value
	regexp.MustCompile(`^Encode\.list$`),   // Encode.list : (a -> Value) -> List a -> Value
	regexp.MustCompile(`^Encode\.string$`), // Encode.string : String -> Value
	regexp.MustCompile(`^Encode\.int$`),    // Encode.int : Int -> Value
	regexp.MustCompile(`^Encode\.float$`),  // Encode.float : Float -> Value
	regexp.MustCompile(`^Encode\.bool$`),   // Encode.bool : Bool -> Value
	regexp.MustCompile(`^Encode\.null$`),   // Encode.null : Value
	regexp.MustCompile(`^Encode\.dict$`),   // Encode.dict
	regexp.MustCompile(`^Json\.Encode\.object$`),
	regexp.MustCompile(`^Json\.Encode\.string$`),

	// ── Cmd / Sub ─────────────────────────────────────────────────────────
	regexp.MustCompile(`^Cmd\.none$`),  // Cmd.none : Cmd msg
	regexp.MustCompile(`^Cmd\.batch$`), // Cmd.batch : List (Cmd msg) -> Cmd msg
	regexp.MustCompile(`^Cmd\.map$`),   // Cmd.map : (a -> b) -> Cmd a -> Cmd b
	regexp.MustCompile(`^Sub\.none$`),  // Sub.none : Sub msg
	regexp.MustCompile(`^Sub\.batch$`), // Sub.batch : List (Sub msg) -> Sub msg
	regexp.MustCompile(`^Sub\.map$`),   // Sub.map : (a -> b) -> Sub a -> Sub b

	// ── Platform / Task ───────────────────────────────────────────────────
	regexp.MustCompile(`^Task\.perform$`), // Task.perform : (a -> msg) -> Task Never a -> Cmd msg
	regexp.MustCompile(`^Task\.attempt$`), // Task.attempt : (Result e a -> msg) -> Task e a -> Cmd msg
	regexp.MustCompile(`^Task\.map$`),     // Task.map
	regexp.MustCompile(`^Task\.andThen$`), // Task.andThen
	regexp.MustCompile(`^Task\.succeed$`), // Task.succeed
	regexp.MustCompile(`^Task\.fail$`),    // Task.fail

	// ── Http (elm/http) ───────────────────────────────────────────────────
	regexp.MustCompile(`^Http\.get$`),          // Http.get : { url : String, expect : Expect msg } -> Cmd msg
	regexp.MustCompile(`^Http\.post$`),         // Http.post
	regexp.MustCompile(`^Http\.request$`),      // Http.request
	regexp.MustCompile(`^Http\.expectJson$`),   // Http.expectJson
	regexp.MustCompile(`^Http\.expectString$`), // Http.expectString
	regexp.MustCompile(`^Http\.jsonBody$`),     // Http.jsonBody
	regexp.MustCompile(`^Http\.stringBody$`),   // Http.stringBody

	// ── Basics (auto-imported) ────────────────────────────────────────────
	regexp.MustCompile(`^modBy$`),       // modBy : Int -> Int -> Int
	regexp.MustCompile(`^remainderBy$`), // remainderBy : Int -> Int -> Int
	regexp.MustCompile(`^ceiling$`),     // ceiling : Float -> Int
	regexp.MustCompile(`^floor$`),       // floor : Float -> Int
	regexp.MustCompile(`^round$`),       // round : Float -> Int
	regexp.MustCompile(`^truncate$`),    // truncate : Float -> Int
	regexp.MustCompile(`^sqrt$`),        // sqrt : Float -> Float
	regexp.MustCompile(`^logBase$`),     // logBase : Float -> Float -> Float
	regexp.MustCompile(`^abs$`),         // abs : number -> number
	regexp.MustCompile(`^negate$`),      // negate : number -> number
	regexp.MustCompile(`^max$`),         // max : comparable -> comparable -> comparable
	regexp.MustCompile(`^min$`),         // min : comparable -> comparable -> comparable
	regexp.MustCompile(`^compare$`),     // compare : comparable -> comparable -> Order
	regexp.MustCompile(`^not$`),         // not : Bool -> Bool
	regexp.MustCompile(`^xor$`),         // xor : Bool -> Bool -> Bool
	regexp.MustCompile(`^identity$`),    // identity : a -> a
	regexp.MustCompile(`^always$`),      // always : a -> b -> a
	regexp.MustCompile(`^never$`),       // never : Never -> a
	regexp.MustCompile(`^toFloat$`),     // toFloat : Int -> Float
	regexp.MustCompile(`^toString$`),    // toString : a -> String
	regexp.MustCompile(`^isNaN$`),       // isNaN : Float -> Bool
	regexp.MustCompile(`^isInfinite$`),  // isInfinite : Float -> Bool
}

func init() {
	dynamicPatternsByLang["elm"] = elmDynamicPatterns
}
