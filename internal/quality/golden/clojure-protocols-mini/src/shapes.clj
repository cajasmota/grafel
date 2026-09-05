(ns app.shapes
  (:require [app.protocols :refer [Shape Sized]]))

(defn compute-area [r]
  (* 3 r r))

(defrecord Circle [radius]
  Shape
  (area [_] (compute-area radius))
  Sized
  (size [_] radius)
  java.util.Map$Entry
  (getKey [_] :radius))

(deftype Square [side]
  Shape
  (area [_]
    (let [helper compute-area
          edge side]
      (helper edge))))

(defrecord Plain [x])
