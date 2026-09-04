;;;; Subclasses: single, multiple, and the multi-line form with slots after it.

(in-package :zoo)

;; (defclass ghost (phantom) ())

(defclass dog (animal)
  ((breed :initarg :breed)))

(defclass hybrid (animal named)
  ())

(defclass poodle
    (dog named)
  ((size :initarg :size)
   (age :initform 0)))

(defparameter *doc*
  "(defclass spectre (phantom) ())")

(defun describe-dog (d)
  (animal-name d))
