(ns app.protocols)

(defprotocol Shape
  "A two-dimensional shape."
  (area [this] "Surface area of the shape."))

(defprotocol Sized
  :extend-via-metadata true
  (size [this]))
