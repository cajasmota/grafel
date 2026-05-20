package resolve

import "regexp"

// lispDynamicPatterns covers all three Lisp-family dialects.
// Each dialect slice is registered separately under its language key.
//
// Three dialect categories:
//
//  1. Common Lisp — CLOS operations (make-instance, defmethod, slot-value),
//     CL stdlib (mapcar, reduce, format, getf, setf, car, cdr, ...).
//
//  2. Scheme — R5RS/R7RS stdlib (map, for-each, apply, call/cc,
//     dynamic-wind, string operations, list operations, arithmetic).
//
//  3. Racket — contracts (define/contract, provide/contract),
//     DrRacket bindings (require/typed, provide/contract),
//     racket/* standard library functions.
//
// All patterns are gated to their respective dialect language key
// (lang=="commonlisp", lang=="scheme", lang=="racket") following the
// safer-bias rule to prevent cross-language collisions.

// ============================================================
// Common Lisp dynamic patterns
// ============================================================

var commonlispDynamicPatterns = []*regexp.Regexp{
	// ── CLOS operations ──────────────────────────────────────
	regexp.MustCompile(`^make-instance$`),     // (make-instance 'class-name ...) — CLOS constructor
	regexp.MustCompile(`^slot-value$`),        // (slot-value obj 'slot) — CLOS slot accessor
	regexp.MustCompile(`^slot-boundp$`),       // (slot-boundp obj 'slot)
	regexp.MustCompile(`^slot-exists-p$`),     // (slot-exists-p obj 'slot)
	regexp.MustCompile(`^class-of$`),          // (class-of obj) — returns class
	regexp.MustCompile(`^find-class$`),        // (find-class 'name)
	regexp.MustCompile(`^initialize-instance$`), // (initialize-instance obj ...) — CLOS init
	regexp.MustCompile(`^change-class$`),      // (change-class obj new-class)
	regexp.MustCompile(`^reinitialize-instance$`),
	regexp.MustCompile(`^compute-applicable-methods$`),
	regexp.MustCompile(`^no-applicable-method$`),
	regexp.MustCompile(`^no-next-method$`),

	// ── CL stdlib: list operations ────────────────────────────
	regexp.MustCompile(`^car$`),               // first element
	regexp.MustCompile(`^cdr$`),               // rest of list
	regexp.MustCompile(`^cadr$`),              // second element
	regexp.MustCompile(`^caddr$`),             // third element
	regexp.MustCompile(`^cadddr$`),            // fourth element
	regexp.MustCompile(`^cons$`),              // (cons head tail)
	regexp.MustCompile(`^list$`),              // (list ...)
	regexp.MustCompile(`^list\*$`),            // (list* ...)
	regexp.MustCompile(`^append$`),            // (append list1 list2 ...)
	regexp.MustCompile(`^nconc$`),             // destructive append
	regexp.MustCompile(`^reverse$`),           // (reverse list)
	regexp.MustCompile(`^nreverse$`),          // destructive reverse
	regexp.MustCompile(`^length$`),            // (length seq)
	regexp.MustCompile(`^elt$`),               // (elt seq index)
	regexp.MustCompile(`^nth$`),               // (nth n list)
	regexp.MustCompile(`^nthcdr$`),            // (nthcdr n list)
	regexp.MustCompile(`^last$`),              // (last list)
	regexp.MustCompile(`^butlast$`),           // (butlast list)
	regexp.MustCompile(`^first$`),             // (first list) — alias for car
	regexp.MustCompile(`^second$`),            // (second list)
	regexp.MustCompile(`^third$`),             // (third list)
	regexp.MustCompile(`^rest$`),              // (rest list) — alias for cdr
	regexp.MustCompile(`^mapcar$`),            // (mapcar fn list ...) — map over list
	regexp.MustCompile(`^maplist$`),           // (maplist fn list)
	regexp.MustCompile(`^mapc$`),              // (mapc fn list) — side-effect map
	regexp.MustCompile(`^mapcan$`),            // (mapcan fn list) — flatmap
	regexp.MustCompile(`^reduce$`),            // (reduce fn list)
	regexp.MustCompile(`^remove$`),            // (remove item list)
	regexp.MustCompile(`^remove-if$`),         // (remove-if pred list)
	regexp.MustCompile(`^remove-if-not$`),     // (remove-if-not pred list)
	regexp.MustCompile(`^delete$`),            // destructive remove
	regexp.MustCompile(`^delete-if$`),         // destructive remove-if
	regexp.MustCompile(`^find$`),              // (find item list)
	regexp.MustCompile(`^find-if$`),           // (find-if pred list)
	regexp.MustCompile(`^find-if-not$`),       // (find-if-not pred list)
	regexp.MustCompile(`^position$`),          // (position item list)
	regexp.MustCompile(`^position-if$`),       // (position-if pred list)
	regexp.MustCompile(`^count$`),             // (count item list)
	regexp.MustCompile(`^count-if$`),          // (count-if pred list)
	regexp.MustCompile(`^sort$`),              // (sort list pred)
	regexp.MustCompile(`^stable-sort$`),       // (stable-sort list pred)
	regexp.MustCompile(`^member$`),            // (member item list)
	regexp.MustCompile(`^assoc$`),             // (assoc key alist)
	regexp.MustCompile(`^rassoc$`),            // (rassoc val alist)
	regexp.MustCompile(`^getf$`),              // (getf plist key) — property list access
	regexp.MustCompile(`^setf$`),              // (setf place value)
	regexp.MustCompile(`^pushnew$`),           // (pushnew item place)
	regexp.MustCompile(`^push$`),              // (push item place)
	regexp.MustCompile(`^pop$`),               // (pop place)
	regexp.MustCompile(`^subseq$`),            // (subseq seq start end)
	regexp.MustCompile(`^copy-list$`),         // (copy-list list)
	regexp.MustCompile(`^copy-seq$`),          // (copy-seq seq)
	regexp.MustCompile(`^coerce$`),            // (coerce object type)
	regexp.MustCompile(`^apply$`),             // (apply fn args)
	regexp.MustCompile(`^funcall$`),           // (funcall fn args...)

	// ── CL stdlib: string / IO ────────────────────────────────
	regexp.MustCompile(`^format$`),            // (format dest ctrl-str args...) — formatted output
	regexp.MustCompile(`^write$`),             // (write obj)
	regexp.MustCompile(`^writeln$`),           // (writeln obj)
	regexp.MustCompile(`^read$`),              // (read)
	regexp.MustCompile(`^read-line$`),         // (read-line)
	regexp.MustCompile(`^print$`),             // (print obj)
	regexp.MustCompile(`^princ$`),             // (princ obj)
	regexp.MustCompile(`^terpri$`),            // (terpri) — newline
	regexp.MustCompile(`^fresh-line$`),        // (fresh-line)
	regexp.MustCompile(`^string$`),            // (string char)
	regexp.MustCompile(`^string-upcase$`),     // (string-upcase s)
	regexp.MustCompile(`^string-downcase$`),   // (string-downcase s)
	regexp.MustCompile(`^string=`),            // string comparisons
	regexp.MustCompile(`^string<`),
	regexp.MustCompile(`^string>`),
	regexp.MustCompile(`^concatenate$`),       // (concatenate 'string ...)
	regexp.MustCompile(`^string-trim$`),       // (string-trim chars s)
	regexp.MustCompile(`^string-left-trim$`),
	regexp.MustCompile(`^string-right-trim$`),
	regexp.MustCompile(`^char$`),              // (char string index)
	regexp.MustCompile(`^char=`),              // char comparison
	regexp.MustCompile(`^char<`),
	regexp.MustCompile(`^char>`),
	regexp.MustCompile(`^with-open-file$`),    // (with-open-file (var file) ...)
	regexp.MustCompile(`^open$`),              // (open path)
	regexp.MustCompile(`^close$`),             // (close stream)
	regexp.MustCompile(`^pathname$`),          // (pathname string)
	regexp.MustCompile(`^namestring$`),        // (namestring path)

	// ── CL stdlib: arithmetic / predicates ────────────────────
	regexp.MustCompile(`^zerop$`),             // (zerop n)
	regexp.MustCompile(`^plusp$`),             // (plusp n)
	regexp.MustCompile(`^minusp$`),            // (minusp n)
	regexp.MustCompile(`^evenp$`),             // (evenp n)
	regexp.MustCompile(`^oddp$`),              // (oddp n)
	regexp.MustCompile(`^numberp$`),           // (numberp x)
	regexp.MustCompile(`^integerp$`),          // (integerp x)
	regexp.MustCompile(`^stringp$`),           // (stringp x)
	regexp.MustCompile(`^symbolp$`),           // (symbolp x)
	regexp.MustCompile(`^consp$`),             // (consp x)
	regexp.MustCompile(`^listp$`),             // (listp x)
	regexp.MustCompile(`^null$`),              // (null x) — empty-list check
	regexp.MustCompile(`^atom$`),              // (atom x)
	regexp.MustCompile(`^not$`),               // (not x)
	regexp.MustCompile(`^equal$`),             // (equal x y)
	regexp.MustCompile(`^equalp$`),            // (equalp x y)
	regexp.MustCompile(`^eql$`),               // (eql x y)
	regexp.MustCompile(`^eq$`),                // (eq x y) — identity
	regexp.MustCompile(`^max$`),               // (max a b ...)
	regexp.MustCompile(`^min$`),               // (min a b ...)
	regexp.MustCompile(`^abs$`),               // (abs n)
	regexp.MustCompile(`^mod$`),               // (mod n m)
	regexp.MustCompile(`^rem$`),               // (rem n m)
	regexp.MustCompile(`^floor$`),             // (floor n)
	regexp.MustCompile(`^ceiling$`),           // (ceiling n)
	regexp.MustCompile(`^round$`),             // (round n)
	regexp.MustCompile(`^truncate$`),          // (truncate n)
	regexp.MustCompile(`^sqrt$`),              // (sqrt n)
	regexp.MustCompile(`^expt$`),              // (expt base exp)
	regexp.MustCompile(`^log$`),               // (log n) / (log n base)
	regexp.MustCompile(`^gcd$`),               // (gcd a b)
	regexp.MustCompile(`^lcm$`),               // (lcm a b)
	regexp.MustCompile(`^incf$`),              // (incf var) — increment
	regexp.MustCompile(`^decf$`),              // (decf var) — decrement
	regexp.MustCompile(`^1\+$`),               // (1+ n)
	regexp.MustCompile(`^1-$`),                // (1- n)

	// ── CL stdlib: hash tables ────────────────────────────────
	regexp.MustCompile(`^make-hash-table$`),   // (make-hash-table)
	regexp.MustCompile(`^gethash$`),           // (gethash key table)
	regexp.MustCompile(`^puthash$`),           // (puthash key val table)
	regexp.MustCompile(`^remhash$`),           // (remhash key table)
	regexp.MustCompile(`^maphash$`),           // (maphash fn table)
	regexp.MustCompile(`^hash-table-count$`),  // (hash-table-count table)
	regexp.MustCompile(`^hash-table-keys$`),   // (hash-table-keys table)
	regexp.MustCompile(`^hash-table-values$`), // (hash-table-values table)

	// ── CL stdlib: conditions / restarts ──────────────────────
	regexp.MustCompile(`^error$`),             // (error condition ...)
	regexp.MustCompile(`^warn$`),              // (warn format ...)
	regexp.MustCompile(`^signal$`),            // (signal condition)
	regexp.MustCompile(`^handler-bind$`),      // (handler-bind ...)
	regexp.MustCompile(`^handler-case$`),      // (handler-case ...)
	regexp.MustCompile(`^ignore-errors$`),     // (ignore-errors ...)
	regexp.MustCompile(`^restart-case$`),      // (restart-case ...)
	regexp.MustCompile(`^invoke-restart$`),    // (invoke-restart name)
}

// ============================================================
// Scheme dynamic patterns
// ============================================================

var schemeDynamicPatterns = []*regexp.Regexp{
	// ── R5RS/R7RS: list operations ────────────────────────────
	regexp.MustCompile(`^cons$`),              // (cons head tail)
	regexp.MustCompile(`^car$`),               // (car pair)
	regexp.MustCompile(`^cdr$`),               // (cdr pair)
	regexp.MustCompile(`^cadr$`),              // (cadr pair) — second
	regexp.MustCompile(`^caddr$`),             // (caddr pair) — third
	regexp.MustCompile(`^list$`),              // (list ...)
	regexp.MustCompile(`^length$`),            // (length list)
	regexp.MustCompile(`^append$`),            // (append list ...)
	regexp.MustCompile(`^reverse$`),           // (reverse list)
	regexp.MustCompile(`^list-ref$`),          // (list-ref list k)
	regexp.MustCompile(`^list-tail$`),         // (list-tail list k)
	regexp.MustCompile(`^map$`),               // (map proc list ...) — functional map
	regexp.MustCompile(`^for-each$`),          // (for-each proc list ...) — side-effect map
	regexp.MustCompile(`^filter$`),            // (filter pred list) — SRFI-1
	regexp.MustCompile(`^fold$`),              // (fold f init list) — SRFI-1
	regexp.MustCompile(`^fold-right$`),        // (fold-right f init list) — SRFI-1
	regexp.MustCompile(`^reduce$`),            // (reduce f init list) — SRFI-1
	regexp.MustCompile(`^find$`),              // (find pred list) — SRFI-1
	regexp.MustCompile(`^any$`),               // (any pred list) — SRFI-1
	regexp.MustCompile(`^every$`),             // (every pred list) — SRFI-1
	regexp.MustCompile(`^take$`),              // (take list n) — SRFI-1
	regexp.MustCompile(`^drop$`),              // (drop list n) — SRFI-1
	regexp.MustCompile(`^iota$`),              // (iota count) — SRFI-1
	regexp.MustCompile(`^delete$`),            // (delete x list) — SRFI-1
	regexp.MustCompile(`^member$`),            // (member x list)
	regexp.MustCompile(`^assoc$`),             // (assoc key alist)
	regexp.MustCompile(`^assq$`),              // (assq key alist)
	regexp.MustCompile(`^assv$`),              // (assv key alist)
	regexp.MustCompile(`^null\?$`),            // (null? x)
	regexp.MustCompile(`^pair\?$`),            // (pair? x)
	regexp.MustCompile(`^list\?$`),            // (list? x)
	regexp.MustCompile(`^last-pair$`),         // (last-pair list)
	regexp.MustCompile(`^set-car!$`),          // (set-car! pair val)
	regexp.MustCompile(`^set-cdr!$`),          // (set-cdr! pair val)

	// ── R5RS/R7RS: higher-order / continuations ───────────────
	regexp.MustCompile(`^apply$`),             // (apply proc args)
	regexp.MustCompile(`^call/cc$`),           // (call/cc proc)
	regexp.MustCompile(`^call-with-current-continuation$`), // long form
	regexp.MustCompile(`^dynamic-wind$`),      // (dynamic-wind in thunk out)
	regexp.MustCompile(`^values$`),            // (values ...)
	regexp.MustCompile(`^call-with-values$`),  // (call-with-values producer consumer)

	// ── R5RS/R7RS: string operations ─────────────────────────
	regexp.MustCompile(`^string\?$`),          // (string? x)
	regexp.MustCompile(`^string$`),            // (string char ...)
	regexp.MustCompile(`^string-length$`),     // (string-length s)
	regexp.MustCompile(`^string-ref$`),        // (string-ref s k)
	regexp.MustCompile(`^string-set!$`),       // (string-set! s k ch)
	regexp.MustCompile(`^string=\?$`),         // (string=? s1 s2)
	regexp.MustCompile(`^string<\?$`),         // (string<? s1 s2)
	regexp.MustCompile(`^string>\?$`),         // (string>? s1 s2)
	regexp.MustCompile(`^string-append$`),     // (string-append s1 s2 ...)
	regexp.MustCompile(`^substring$`),         // (substring s start end)
	regexp.MustCompile(`^string->list$`),      // (string->list s)
	regexp.MustCompile(`^list->string$`),      // (list->string chars)
	regexp.MustCompile(`^string-copy$`),       // (string-copy s)
	regexp.MustCompile(`^string-upcase$`),     // SRFI-13
	regexp.MustCompile(`^string-downcase$`),   // SRFI-13
	regexp.MustCompile(`^string-contains$`),   // SRFI-13
	regexp.MustCompile(`^string-split$`),      // SRFI-13
	regexp.MustCompile(`^string-join$`),       // SRFI-13

	// ── R5RS/R7RS: IO ─────────────────────────────────────────
	regexp.MustCompile(`^display$`),           // (display obj)
	regexp.MustCompile(`^newline$`),           // (newline)
	regexp.MustCompile(`^write$`),             // (write obj)
	regexp.MustCompile(`^read$`),              // (read)
	regexp.MustCompile(`^read-char$`),         // (read-char)
	regexp.MustCompile(`^peek-char$`),         // (peek-char)
	regexp.MustCompile(`^open-input-file$`),   // (open-input-file path)
	regexp.MustCompile(`^open-output-file$`),  // (open-output-file path)
	regexp.MustCompile(`^close-input-port$`),  // (close-input-port p)
	regexp.MustCompile(`^close-output-port$`), // (close-output-port p)
	regexp.MustCompile(`^call-with-port$`),    // R7RS
	regexp.MustCompile(`^with-output-to-file$`),
	regexp.MustCompile(`^with-input-from-file$`),

	// ── R5RS/R7RS: arithmetic / predicates ───────────────────
	regexp.MustCompile(`^number\?$`),          // (number? x)
	regexp.MustCompile(`^integer\?$`),         // (integer? x)
	regexp.MustCompile(`^zero\?$`),            // (zero? n)
	regexp.MustCompile(`^positive\?$`),        // (positive? n)
	regexp.MustCompile(`^negative\?$`),        // (negative? n)
	regexp.MustCompile(`^odd\?$`),             // (odd? n)
	regexp.MustCompile(`^even\?$`),            // (even? n)
	regexp.MustCompile(`^equal\?$`),           // (equal? a b)
	regexp.MustCompile(`^eqv\?$`),             // (eqv? a b)
	regexp.MustCompile(`^eq\?$`),              // (eq? a b)
	regexp.MustCompile(`^not$`),               // (not x)
	regexp.MustCompile(`^boolean\?$`),         // (boolean? x)
	regexp.MustCompile(`^max$`),               // (max a b ...)
	regexp.MustCompile(`^min$`),               // (min a b ...)
	regexp.MustCompile(`^abs$`),               // (abs n)
	regexp.MustCompile(`^modulo$`),            // (modulo n m)
	regexp.MustCompile(`^remainder$`),         // (remainder n m)
	regexp.MustCompile(`^quotient$`),          // (quotient n m)
	regexp.MustCompile(`^gcd$`),               // (gcd a b)
	regexp.MustCompile(`^lcm$`),               // (lcm a b)
	regexp.MustCompile(`^floor$`),             // (floor n)
	regexp.MustCompile(`^ceiling$`),           // (ceiling n)
	regexp.MustCompile(`^round$`),             // (round n)
	regexp.MustCompile(`^truncate$`),          // (truncate n)
	regexp.MustCompile(`^sqrt$`),              // (sqrt n)
	regexp.MustCompile(`^expt$`),              // (expt base exp)
	regexp.MustCompile(`^exact->inexact$`),    // (exact->inexact n)
	regexp.MustCompile(`^inexact->exact$`),    // (inexact->exact n)
	regexp.MustCompile(`^number->string$`),    // (number->string n)
	regexp.MustCompile(`^string->number$`),    // (string->number s)
	regexp.MustCompile(`^symbol->string$`),    // (symbol->string sym)
	regexp.MustCompile(`^string->symbol$`),    // (string->symbol s)
	regexp.MustCompile(`^char->integer$`),     // (char->integer ch)
	regexp.MustCompile(`^integer->char$`),     // (integer->char n)
	regexp.MustCompile(`^vector->list$`),      // (vector->list v)
	regexp.MustCompile(`^list->vector$`),      // (list->vector lst)

	// ── R7RS: misc ────────────────────────────────────────────
	regexp.MustCompile(`^error$`),             // (error msg ...)
	regexp.MustCompile(`^make-vector$`),       // (make-vector k)
	regexp.MustCompile(`^vector-ref$`),        // (vector-ref v k)
	regexp.MustCompile(`^vector-set!$`),       // (vector-set! v k x)
	regexp.MustCompile(`^vector-length$`),     // (vector-length v)
	regexp.MustCompile(`^make-parameter$`),    // (make-parameter val) — R7RS parameters
	regexp.MustCompile(`^parameterize$`),      // (parameterize ...)
	regexp.MustCompile(`^with-exception-handler$`),
	regexp.MustCompile(`^raise$`),             // (raise obj)
	regexp.MustCompile(`^raise-continuable$`), // (raise-continuable obj)
}

// ============================================================
// Racket dynamic patterns
// ============================================================

var racketDynamicPatterns = []*regexp.Regexp{
	// ── Contracts ─────────────────────────────────────────────
	regexp.MustCompile(`^->$`),                // (-> domain ... range) — function contract
	regexp.MustCompile(`^->i$`),               // (->i ...) — dependent contract
	regexp.MustCompile(`^->*$`),               // (->* ...) — opt-arg contract
	regexp.MustCompile(`^contract\?$`),        // (contract? x)
	regexp.MustCompile(`^flat-contract$`),     // (flat-contract pred)
	regexp.MustCompile(`^flat-contract-predicate$`),
	regexp.MustCompile(`^and/c$`),             // (and/c c1 c2)
	regexp.MustCompile(`^or/c$`),              // (or/c c1 c2)
	regexp.MustCompile(`^not/c$`),             // (not/c c)
	regexp.MustCompile(`^any/c$`),             // any/c — always satisfied
	regexp.MustCompile(`^none/c$`),            // none/c — never satisfied
	regexp.MustCompile(`^listof$`),            // (listof c)
	regexp.MustCompile(`^vectorof$`),          // (vectorof c)
	regexp.MustCompile(`^hash/c$`),            // (hash/c key-c val-c)
	regexp.MustCompile(`^between/c$`),         // (between/c lo hi)
	regexp.MustCompile(`^integer-in$`),        // (integer-in lo hi)
	regexp.MustCompile(`^provide/contract$`),  // (provide/contract ...)
	regexp.MustCompile(`^require/typed$`),     // (require/typed ...)

	// ── racket/list ───────────────────────────────────────────
	regexp.MustCompile(`^first$`),             // (first lst)
	regexp.MustCompile(`^second$`),            // (second lst)
	regexp.MustCompile(`^third$`),             // (third lst)
	regexp.MustCompile(`^fourth$`),            // (fourth lst)
	regexp.MustCompile(`^rest$`),              // (rest lst)
	regexp.MustCompile(`^last$`),              // (last lst)
	regexp.MustCompile(`^take$`),              // (take lst n)
	regexp.MustCompile(`^drop$`),              // (drop lst n)
	regexp.MustCompile(`^take-right$`),        // (take-right lst n)
	regexp.MustCompile(`^drop-right$`),        // (drop-right lst n)
	regexp.MustCompile(`^filter$`),            // (filter pred lst)
	regexp.MustCompile(`^foldl$`),             // (foldl f init lst)
	regexp.MustCompile(`^foldr$`),             // (foldr f init lst)
	regexp.MustCompile(`^andmap$`),            // (andmap pred lst)
	regexp.MustCompile(`^ormap$`),             // (ormap pred lst)
	regexp.MustCompile(`^count$`),             // (count pred lst)
	regexp.MustCompile(`^flatten$`),           // (flatten lst)
	regexp.MustCompile(`^remove$`),            // (remove x lst)
	regexp.MustCompile(`^remove-duplicates$`), // (remove-duplicates lst)
	regexp.MustCompile(`^sort$`),              // (sort lst <)
	regexp.MustCompile(`^append-map$`),        // (append-map f lst)
	regexp.MustCompile(`^list-update$`),       // (list-update lst k f)
	regexp.MustCompile(`^findf$`),             // (findf pred lst)
	regexp.MustCompile(`^index-of$`),          // (index-of lst x)
	regexp.MustCompile(`^indexes-of$`),        // (indexes-of lst x)
	regexp.MustCompile(`^range$`),             // (range n) or (range lo hi)
	regexp.MustCompile(`^make-list$`),         // (make-list n x)

	// ── racket/string ─────────────────────────────────────────
	regexp.MustCompile(`^string-split$`),      // (string-split s)
	regexp.MustCompile(`^string-join$`),       // (string-join lst sep)
	regexp.MustCompile(`^string-contains$`),   // (string-contains s needle)
	regexp.MustCompile(`^string-replace$`),    // (string-replace s from to)
	regexp.MustCompile(`^string-trim$`),       // (string-trim s)
	regexp.MustCompile(`^string-prefix\?$`),   // (string-prefix? pre s)
	regexp.MustCompile(`^string-suffix\?$`),   // (string-suffix? suf s)
	regexp.MustCompile(`^string-upcase$`),     // (string-upcase s)
	regexp.MustCompile(`^string-downcase$`),   // (string-downcase s)
	regexp.MustCompile(`^~a$`),                // format shorthand
	regexp.MustCompile(`^~v$`),                // format shorthand
	regexp.MustCompile(`^format$`),            // (format "~a" ...)

	// ── racket/match ──────────────────────────────────────────
	regexp.MustCompile(`^match$`),             // (match val clause ...)
	regexp.MustCompile(`^match\*$`),           // (match* ...)
	regexp.MustCompile(`^match-define$`),      // (match-define pat val)
	regexp.MustCompile(`^match-lambda$`),      // (match-lambda clause ...)
	regexp.MustCompile(`^match-lambda\*$`),    // (match-lambda* ...)

	// ── racket/hash ───────────────────────────────────────────
	regexp.MustCompile(`^hash$`),              // (hash k v ...)
	regexp.MustCompile(`^make-hash$`),         // (make-hash)
	regexp.MustCompile(`^hash-ref$`),          // (hash-ref h k)
	regexp.MustCompile(`^hash-set$`),          // (hash-set h k v)
	regexp.MustCompile(`^hash-set!$`),         // (hash-set! h k v)
	regexp.MustCompile(`^hash-remove$`),       // (hash-remove h k)
	regexp.MustCompile(`^hash-remove!$`),      // (hash-remove! h k)
	regexp.MustCompile(`^hash-has-key\?$`),    // (hash-has-key? h k)
	regexp.MustCompile(`^hash-keys$`),         // (hash-keys h)
	regexp.MustCompile(`^hash-values$`),       // (hash-values h)
	regexp.MustCompile(`^hash-count$`),        // (hash-count h)
	regexp.MustCompile(`^hash-update$`),       // (hash-update h k f)
	regexp.MustCompile(`^hash-update!$`),      // (hash-update! h k f)
	regexp.MustCompile(`^hash-for-each$`),     // (hash-for-each h proc)
	regexp.MustCompile(`^hash-map$`),          // (hash-map h proc)
	regexp.MustCompile(`^hash\?$`),            // (hash? x)

	// ── racket: IO / ports ────────────────────────────────────
	regexp.MustCompile(`^displayln$`),         // (displayln x)
	regexp.MustCompile(`^println$`),           // (println x)
	regexp.MustCompile(`^printf$`),            // (printf fmt ...)
	regexp.MustCompile(`^current-output-port$`),
	regexp.MustCompile(`^current-input-port$`),
	regexp.MustCompile(`^with-output-to-string$`),
	regexp.MustCompile(`^open-input-string$`), // (open-input-string s)
	regexp.MustCompile(`^port->string$`),      // (port->string p)
	regexp.MustCompile(`^file->string$`),      // (file->string path)
	regexp.MustCompile(`^file->lines$`),       // (file->lines path)
	regexp.MustCompile(`^display-to-file$`),   // (display-to-file s path)
	regexp.MustCompile(`^write-to-file$`),     // (write-to-file val path)

	// ── racket: arithmetic / predicates ──────────────────────
	regexp.MustCompile(`^exact\?$`),           // (exact? n)
	regexp.MustCompile(`^inexact\?$`),         // (inexact? n)
	regexp.MustCompile(`^exact->inexact$`),    // (exact->inexact n)
	regexp.MustCompile(`^inexact->exact$`),    // (inexact->exact n)
	regexp.MustCompile(`^zero\?$`),            // (zero? n)
	regexp.MustCompile(`^positive\?$`),        // (positive? n)
	regexp.MustCompile(`^negative\?$`),        // (negative? n)
	regexp.MustCompile(`^odd\?$`),             // (odd? n)
	regexp.MustCompile(`^even\?$`),            // (even? n)
	regexp.MustCompile(`^equal\?$`),           // (equal? a b)
	regexp.MustCompile(`^eqv\?$`),             // (eqv? a b)
	regexp.MustCompile(`^eq\?$`),              // (eq? a b)
	regexp.MustCompile(`^not$`),               // (not x)
	regexp.MustCompile(`^void$`),              // (void) — no-op result
	regexp.MustCompile(`^void\?$`),            // (void? x)
	regexp.MustCompile(`^current-seconds$`),   // (current-seconds) — unix time
	regexp.MustCompile(`^random$`),            // (random n)
	regexp.MustCompile(`^error$`),             // (error msg ...)
	regexp.MustCompile(`^raise$`),             // (raise exn)
	regexp.MustCompile(`^raise-argument-error$`),
	regexp.MustCompile(`^raise-type-error$`),
	regexp.MustCompile(`^exn:fail\?$`),        // (exn:fail? x) — exception predicate
	regexp.MustCompile(`^with-handlers$`),     // (with-handlers ([pred handler] ...) ...)
	regexp.MustCompile(`^dynamic-require$`),   // (dynamic-require mod-path binding)
	regexp.MustCompile(`^symbol\?$`),          // (symbol? x)
	regexp.MustCompile(`^procedure\?$`),       // (procedure? x)
	regexp.MustCompile(`^apply$`),             // (apply proc args)
	regexp.MustCompile(`^map$`),               // (map f lst)
	regexp.MustCompile(`^for-each$`),          // (for-each f lst)
	regexp.MustCompile(`^values$`),            // (values ...)
	regexp.MustCompile(`^call-with-values$`),  // (call-with-values ...)
	regexp.MustCompile(`^call/cc$`),           // (call/cc ...)
}

func init() {
	dynamicPatternsByLang["commonlisp"] = commonlispDynamicPatterns
	dynamicPatternsByLang["scheme"] = schemeDynamicPatterns
	dynamicPatternsByLang["racket"] = racketDynamicPatterns
}
