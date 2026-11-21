;;; agent-desk-test.el --- Tests for agent-desk.el  -*- lexical-binding: t; -*-

;; Run: emacs -Q --batch -L emacs -l emacs/agent-desk-test.el -f ert-run-tests-batch-and-exit
;; A stub program stands in for agent-desk: it records its arguments and
;; standard input, and answers `show' with a fixed view.

;;; Code:

(require 'ert)
(require 'agent-desk)

(defconst agent-desk-test--view
  "{\"needs_you\":[{\"items\":[
    {\"display_id\":\"q-aaaa\",\"event\":{\"kind\":\"ask\",\"meta\":{\"default\":\"stop at 30\"}}},
    {\"display_id\":\"q-bbbb\",\"question\":{\"meta\":{\"choices\":{\"questions\":[
      {\"text\":\"Split?\",\"options\":[{\"key\":\"A\",\"label\":\"Reuse\"},{\"key\":\"B\",\"label\":\"Regenerate\"}],\"recommended\":\"B\"},
      {\"text\":\"Tags?\",\"options\":[{\"key\":\"A\",\"label\":\"x\"},{\"key\":\"B\",\"label\":\"y\"},{\"key\":\"C\",\"label\":\"z\"}],\"multi\":true}]}}}}]}]}"
  "What the stub prints for show.")

(defmacro agent-desk-test--with-stub (&rest body)
  "Run BODY with `agent-desk-program' a stub; `log' names its record."
  (declare (indent 0))
  `(let* ((dir (make-temp-file "agent-desk-test" t))
          (log (expand-file-name "log" dir))
          (view (expand-file-name "view.json" dir))
          (stub (expand-file-name "agent-desk" dir))
          (agent-desk-render-directory (expand-file-name "render" dir))
          (agent-desk-program stub))
     (unwind-protect
         (progn
           (with-temp-file view (insert agent-desk-test--view))
           (with-temp-file stub
             (insert "#!/bin/sh\n"
                     "if [ \"$1\" = show ]; then cat " (shell-quote-argument view) "; exit 0; fi\n"
                     "printf 'ARGS %s\\n' \"$*\" >> " (shell-quote-argument log) "\n"
                     "printf 'STDIN ' >> " (shell-quote-argument log) "\n"
                     "cat >> " (shell-quote-argument log) "\n"
                     "printf '\\n' >> " (shell-quote-argument log) "\n"
                     "echo ok\n"))
           (set-file-modes stub #o700)
           (make-directory agent-desk-render-directory t)
           ,@body)
       (delete-directory dir t))))

(defun agent-desk-test--wait (proc)
  "Wait for PROC and its sentinel."
  (while (process-live-p proc) (accept-process-output proc 0.05))
  (accept-process-output nil 0.05))

(defun agent-desk-test--log (log)
  "The stub's record."
  (with-temp-buffer (insert-file-contents log) (buffer-string)))

(defun agent-desk-test--file (dir)
  "Visit a small desk file in DIR; point on item q-bbbb."
  (let ((f (expand-file-name "inbox.org" dir)))
    (with-temp-file f
      (insert "* Pinned (1)\n** s-pppp slot: next\n:PROPERTIES:\n:DESK_ID: s-pppp\n:END:\n  text\n"
              "* Needs you (1)\n** proj\n:PROPERTIES:\n:DESK_PATH: proj\n:END:\n"
              "*** ASK q-bbbb ask: Which split?\n:PROPERTIES:\n:DESK_ID: q-bbbb\n:DESK_PATH: proj/r/coord\n:END:\n  body\n"))
    (find-file f)
    (goto-char (point-min))
    (re-search-forward "^  body")
    (current-buffer)))

(ert-deftest agent-desk-test-choice-answer ()
  (let* ((choices (alist-get 'choices
                             (alist-get 'meta (alist-get 'question
                                                         (with-temp-buffer
                                                           (insert agent-desk-test--view)
                                                           (goto-char (point-min))
                                                           (aref (alist-get 'items (aref (alist-get 'needs_you (json-parse-buffer :object-type 'alist)) 0)) 1)))))))
    (should (equal (agent-desk--choice-lines choices) "1. B\n2. "))
    (should (equal (agent-desk--choice-answer "1. b keep the seed\n2. A, c" choices)
                   "1. B: Regenerate. Note: keep the seed\n2. A, C: x; z"))
    (should (equal (agent-desk--choice-answer "1. neither, ask me later\n\nThanks." choices)
                   "1. Other: neither, ask me later\n2. (no answer)\n\nThanks."))
    (should (equal (agent-desk--choice-answer "1. B-keep it" choices)
                   "1. B: Regenerate. Note: keep it\n2. (no answer)"))))

(ert-deftest agent-desk-test-choice-answer-hyphen-keys ()
  ;; A key with hyphens is read whole, not as its first word plus a note.
  (let ((choices '((questions . [((text . "Hunk 1") (options . [((key . "both") (label . "Both, upstream first"))
                                                              ((key . "both-local-first") (label . "Both, local first"))]))]))))
    (should (equal (agent-desk--choice-answer "1. both-local-first" choices)
                   "1. both-local-first: Both, local first"))
    (should (equal (agent-desk--choice-answer "1. both - keep order" choices)
                   "1. both: Both, upstream first. Note: keep order"))
    (should (equal (agent-desk--choice-answer "1. both-x" choices)
                   "1. both: Both, upstream first. Note: x"))))

(ert-deftest agent-desk-test-commands ()
  (agent-desk-test--with-stub
    (with-current-buffer (agent-desk-test--file agent-desk-render-directory)
      (should agent-desk-org-mode)
      (should buffer-read-only)
      (should (equal (agent-desk-id-at-point) "q-bbbb"))
      (agent-desk-test--wait (agent-desk-read))
      ;; Pin toggles by section: q-bbbb is not pinned, s-pppp is.
      (agent-desk-test--wait (agent-desk-pin))
      (goto-char (point-min)) (re-search-forward "^  text")
      (agent-desk-test--wait (agent-desk-pin))
      ;; On a node heading, reply targets its path.
      (goto-char (point-min)) (re-search-forward "^\\*\\* proj")
      (should (equal (agent-desk-target-at-point) "proj"))
      (agent-desk-reply)
      (insert "-n is not a flag here")
      (agent-desk-compose-send)
      (while (get-process "agent-desk") (accept-process-output nil 0.05))
      ;; An answer to a choice question.
      (set-buffer (find-buffer-visiting (expand-file-name "inbox.org" agent-desk-render-directory)))
      (re-search-forward "^  body")
      (agent-desk-answer)
      (should (equal (buffer-string) "1. B\n2. "))
      (goto-char (point-max)) (insert "C")
      (agent-desk-compose-send)
      (while (get-process "agent-desk") (accept-process-output nil 0.05))
      ;; Each action renders the desk once it succeeds.
      (let ((render "ARGS render\nSTDIN \n"))
        (should (equal (agent-desk-test--log log)
                       (concat "ARGS read q-bbbb\nSTDIN \n" render
                               "ARGS pin q-bbbb\nSTDIN \n" render
                               "ARGS unpin s-pppp\nSTDIN \n" render
                               "ARGS reply proj\nSTDIN -n is not a flag here\n\n" render
                               "ARGS answer q-bbbb\nSTDIN 1. B: Regenerate\n2. C: z\n\n" render)))))))

(ert-deftest agent-desk-test-take ()
  (agent-desk-test--with-stub
    (cl-letf (((symbol-function 'y-or-n-p) (lambda (&rest _) t)))
      (with-current-buffer (agent-desk-test--file agent-desk-render-directory)
        ;; Not every question has a recommendation: refused.
        (should-error (agent-desk-take) :type 'user-error)
        (goto-char (point-min))
        (re-search-forward "^\\*\\*\\* ")
        (let ((inhibit-read-only t))
          (replace-regexp-in-region "q-bbbb" "q-aaaa" (point-min) (point-max)))
        (re-search-forward "^  body")
        (agent-desk-test--wait (agent-desk-take))
        (should (equal (agent-desk-test--log log)
                       (concat "ARGS answer q-aaaa\nSTDIN Go ahead with your default: stop at 30\n\n")))
        (set-buffer-modified-p nil)))))

;;; agent-desk-test.el ends here
