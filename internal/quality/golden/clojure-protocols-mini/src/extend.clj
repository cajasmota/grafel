(ns app.extend
  (:require [app.protocols :refer [Shape Sized]])
  (:gen-class
    :name app.Extend
    :extends java.util.AbstractList
    :implements [java.lang.Runnable java.io.Closeable]))

(defrecord Triangle [base height])

(defrecord Hexagon [side])

(extend-type Triangle
  Shape
  (area [this] (* 0.5 (:base this) (:height this))))

(extend-protocol Sized
  Hexagon
  (size [this] (:side this)))

(defprotocol Weighted
  (weight [this]))

(extend-protocol Weighted
  Triangle
  (weight [this] (:base this)))

(comment
  (defrecord Scratch [z]
    Ghost
    (area [_] 0)))

(def sample-doc "
(defrecord Spectre [q]
  Phantom
  (area [_] 0))
")
