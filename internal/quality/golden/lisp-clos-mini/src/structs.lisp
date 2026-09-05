;;;; Neighbouring def-forms that must NOT acquire a superclass edge, plus the
;;;; one malformed shape that separates a positional anchor from a producer
;;;; that grabs the next parenthesised group.

(in-package :zoo)

(defstruct segment (start) (end))

(defgeneric speak (thing other))

;; Deliberately malformed CLOS: the superclass list is MISSING, so the first
;; parenthesised group after the name is the SLOT list. A producer anchored on
;; position emits nothing here; one that takes "the next group" reports
;; `lonely EXTENDS name`.
(defclass lonely ((name :initarg :name)))

(defun segment-length (s)
  (- (segment-end s) (segment-start s)))
