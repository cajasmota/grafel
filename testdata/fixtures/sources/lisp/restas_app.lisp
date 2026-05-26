;;; Lisp common-restas fixture.
;;; Demonstrates: HTTP endpoints, simple queue (CL-NAIVE-STORE), consumer, DB access (simple key-value store).

(in-package :restas-app)

;;; ── Package & dependencies ────────────────────────────────────────────────

(restas:define-application restas-app
  :domain "0.0.0.0"
  :port 8080)

;;; ── In-memory DB (alist-based for fixture simplicity) ────────────────────

(defvar *items-store* '()
  "In-memory item store: list of plists (:id :name :quantity).")

(defvar *next-id* 1
  "Auto-increment ID counter.")

(defvar *event-queue* '()
  "Simple in-process event queue.")

(defun db-find-item (id)
  (find id *items-store* :key (lambda (item) (getf item :id))))

(defun db-all-items ()
  *items-store*)

(defun db-save-item (item)
  (push item *items-store*)
  item)

(defun db-delete-item (id)
  (let ((old-len (length *items-store*)))
    (setf *items-store* (remove-if (lambda (item) (= (getf item :id) id)) *items-store*))
    (< (length *items-store*) old-len)))

;;; ── Event queue (producer) ────────────────────────────────────────────────

(defun enqueue-event (event-type payload)
  "Enqueue an event to the internal queue."
  (push (list :type event-type :payload payload :timestamp (get-universal-time))
        *event-queue*))

;;; ── Event consumer ────────────────────────────────────────────────────────

(defun process-next-event ()
  "Process the oldest event from the queue."
  (when *event-queue*
    (let ((event (car (last *event-queue*))))
      (setf *event-queue* (butlast *event-queue*))
      (format t "Processing event ~A: ~A~%" (getf event :type) (getf event :payload))
      event)))

(defun start-event-loop ()
  "Spawn a simple background thread to drain the event queue."
  (bt:make-thread
   (lambda ()
     (loop
       (when *event-queue*
         (process-next-event))
       (sleep 0.1)))
   :name "event-consumer"))

;;; ── HTTP Routes ──────────────────────────────────────────────────────────

(restas:define-route health ("/health" :method :get)
  (setf (hunchentoot:content-type*) "application/json")
  "{\"status\":\"ok\"}")

(restas:define-route list-items ("/api/items" :method :get)
  (setf (hunchentoot:content-type*) "application/json")
  (let ((items (db-all-items)))
    (format nil "[~{~A~^,~}]"
            (mapcar (lambda (item)
                      (format nil "{\"id\":~A,\"name\":\"~A\",\"quantity\":~A}"
                              (getf item :id)
                              (getf item :name)
                              (getf item :quantity)))
                    items))))

(restas:define-route get-item ("/api/items/:id" :method :get)
  (let* ((id (parse-integer (hunchentoot:parameter "id")))
         (item (db-find-item id)))
    (if item
        (progn
          (setf (hunchentoot:content-type*) "application/json")
          (format nil "{\"id\":~A,\"name\":\"~A\",\"quantity\":~A}"
                  (getf item :id)
                  (getf item :name)
                  (getf item :quantity)))
        (progn
          (setf (hunchentoot:return-code*) hunchentoot:+http-not-found+)
          "{\"error\":\"not found\"}"))))

(restas:define-route create-item ("/api/items" :method :post)
  (let* ((body (hunchentoot:raw-post-data :force-text t))
         (name (extract-json-field body "name"))
         (quantity (parse-integer (extract-json-field body "quantity")))
         (id *next-id*))
    (incf *next-id*)
    (let ((item (list :id id :name name :quantity quantity)))
      (db-save-item item)
      (enqueue-event :item-created item)
      (setf (hunchentoot:content-type*) "application/json")
      (setf (hunchentoot:return-code*) hunchentoot:+http-created+)
      (format nil "{\"id\":~A,\"name\":\"~A\",\"quantity\":~A}" id name quantity))))

(restas:define-route delete-item ("/api/items/:id" :method :delete)
  (let* ((id (parse-integer (hunchentoot:parameter "id")))
         (deleted (db-delete-item id)))
    (if deleted
        (progn
          (enqueue-event :item-deleted (list :id id))
          (setf (hunchentoot:return-code*) hunchentoot:+http-no-content+)
          "")
        (progn
          (setf (hunchentoot:return-code*) hunchentoot:+http-not-found+)
          "{\"error\":\"not found\"}"))))

;;; ── Helpers ──────────────────────────────────────────────────────────────

(defun extract-json-field (json-string field-name)
  "Naive JSON field extractor for fixture use."
  (let* ((pattern (format nil "\"~A\":\"?([^,}\"]+)\"?" field-name))
         (start (search (format nil "\"~A\":" field-name) json-string)))
    (when start
      (let* ((value-start (+ start (length (format nil "\"~A\":" field-name))))
             (is-string (char= (char json-string value-start) #\"))
             (real-start (if is-string (1+ value-start) value-start))
             (end-char (if is-string #\" #\,))
             (value-end (or (position end-char json-string :start real-start)
                            (position #\} json-string :start real-start))))
        (subseq json-string real-start value-end)))))

;;; ── Main entry point ─────────────────────────────────────────────────────

(defun start ()
  (start-event-loop)
  (restas:start 'restas-app))
