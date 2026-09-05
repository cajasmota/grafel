;;;; Root classes of the zoo model.
;;;;
;;;; Both classes here declare an EMPTY superclass list `()` followed by a slot
;;;; list. In CLOS the superclass list is mandatory and positional, so `()` is
;;;; how a root class is written — and because these two slot lists name their
;;;; slots with no options, they are FLAT: a producer that treats `()` as
;;;; "nothing here" and reads the next group emits the slot names as parents.

(in-package :zoo)

(defclass animal ()
  (name legs))

(defclass named ()
  (label))

(defun animal-legs (a)
  (slot-value a 'legs))
