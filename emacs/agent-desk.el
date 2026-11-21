;;; agent-desk.el --- Act on agent-desk items from its org files  -*- lexical-binding: t; -*-

;; Author: NightMachinery
;; URL: https://github.com/NightMachinery/agent-desk
;; Version: 0.1
;; Package-Requires: ((emacs "28.1"))
;; Keywords: tools, convenience

;;; Commentary:

;; agent-desk renders its views as org files under
;; ${AGENT_DESK_HOME:-~/.agents/desk}/render/: inbox.org, board/<project>.org
;; and ledger/<yyyy-mm>.org.  The dashboard server renders them a moment
;; after the desk changes (Settings, "Org files"); `agent-desk render' does
;; it by hand, and this package runs it after each action and before
;; opening the inbox, so the files are current without a server.  Each item is a heading with a :DESK_ID:
;; property, and each node a heading with a :DESK_PATH:.  This package acts
;; on the item at point: answer, take the recommendation, message its agent,
;; mark it read or unread, pin it, open it on the dashboard, copy its id.
;;
;; The files are generated, so `agent-desk-org-mode' (on in any file under
;; the render directory) makes them read-only, reverts them when the desk
;; changes, and binds single keys in evil's normal state that match the
;; dashboard: a answer, t take, r reply, d read, U unread, p pin, o open,
;; gr refresh.  The same commands sit under C-c C-d without evil.
;;
;; Answers and messages are written in a compose buffer: C-c C-c (or ZZ)
;; sends, C-c C-k (or ZQ) cancels, and a prefix argument to either opening
;; command or to the send asks how to deliver it (steer, queue, interrupt).
;;
;; Load it with (load "/path/to/agent-desk/emacs/agent-desk.el").

;;; Code:

(require 'cl-lib)
(require 'org)
(require 'json)
(require 'subr-x)

(defgroup agent-desk nil
  "Act on agent-desk items from its rendered org files."
  :group 'tools
  :prefix "agent-desk-")

(defcustom agent-desk-program "agent-desk"
  "The agent-desk command."
  :type 'string)

(defcustom agent-desk-render-directory
  (expand-file-name "render" (or (getenv "AGENT_DESK_HOME") "~/.agents/desk"))
  "Where agent-desk writes its org files."
  :type 'directory)

(defcustom agent-desk-evil-keys t
  "Non-nil binds single keys in evil's normal state in desk files."
  :type 'boolean)

(defcustom agent-desk-behaviors '("steer" "queue" "interrupt")
  "Delivery behaviours offered when a command asks how to deliver."
  :type '(repeat string))

;;;; The item at point

(defun agent-desk-id-at-point ()
  "The display id (q-..., t-...) of the desk item at point, or nil."
  (when (derived-mode-p 'org-mode)
    (org-entry-get nil "DESK_ID")))

(defun agent-desk-target-at-point ()
  "The item id at point, else the node path at point, else nil."
  (when (derived-mode-p 'org-mode)
    (or (org-entry-get nil "DESK_ID")
        (org-entry-get nil "DESK_PATH" t))))

(defun agent-desk--need (thing what)
  "Return THING, or signal that point is on no WHAT."
  (or thing (user-error "No desk %s at point" what)))

(defun agent-desk--pinned-p ()
  "Whether the item at point sits under the inbox's Pinned section."
  (save-excursion
    (org-back-to-heading t)
    (while (org-up-heading-safe))
    (string-prefix-p "Pinned" (org-get-heading t t t t))))

;;;; Running the command

(defvar agent-desk--log-buffer " *agent-desk*"
  "Where the command's output goes.")

(defun agent-desk--run (args &optional input on-success)
  "Run `agent-desk-program' with ARGS, asynchronously.
INPUT, a string, is its standard input; stdin is closed either way, since
some commands read it when given no text.  On success call ON-SUCCESS with
the output, render the desk (writes no longer do; the dashboard server
does a moment later) and refresh the desk's buffers."
  (let* ((buf (get-buffer-create agent-desk--log-buffer))
         (start (with-current-buffer buf
                  (goto-char (point-max))
                  (insert (format "\n$ %s %s\n" agent-desk-program
                                  (mapconcat #'shell-quote-argument args " ")))
                  (point-max)))
         (proc (make-process
                :name "agent-desk"
                :buffer buf
                :command (cons agent-desk-program args)
                :connection-type 'pipe
                :noquery t
                :sentinel
                (lambda (p _event)
                  (when (memq (process-status p) '(exit signal))
                    (let ((out (with-current-buffer (process-buffer p)
                                 (string-trim (buffer-substring-no-properties
                                               start (point-max))))))
                      (cond
                       ((zerop (process-exit-status p))
                        (when on-success (funcall on-success out))
                        (if (equal (car args) "render")
                            (agent-desk-refresh-buffers)
                          (agent-desk--run (list "render"))))
                       (t (message "agent-desk %s failed: %s" (car args)
                                   (car (last (split-string out "\n" t))))))))))))
    (when input (process-send-string proc input))
    (process-send-eof proc)
    proc))

(defun agent-desk--say (fmt)
  "An ON-SUCCESS function that shows FMT with the command's last line."
  (lambda (out)
    (message fmt (or (car (last (split-string out "\n" t))) ""))))

(defun agent-desk--item-json (id)
  "The dashboard view's entry for item ID, as an alist, or nil."
  (with-temp-buffer
    (when (zerop (call-process agent-desk-program nil '(t nil) nil
                               "show" "--all" "--format" "json"))
      (goto-char (point-min))
      (let ((found nil))
        (cl-labels ((walk (x)
                      (cond
                       (found)
                       ((and (consp x) (consp (car x)) (symbolp (caar x)))
                        (if (equal (alist-get 'display_id x) id)
                            (setq found x)
                          (mapc (lambda (kv) (walk (cdr kv))) x)))
                       ((vectorp x) (mapc #'walk x))
                       ((consp x) (mapc #'walk x)))))
          (walk (json-parse-buffer :object-type 'alist :array-type 'array
                                   :null-object nil :false-object nil)))
        found))))

(defun agent-desk--question-meta (id)
  "The meta of question ID: an alist with `default' and `choices', if any."
  (let ((item (agent-desk--item-json id)))
    (or (alist-get 'meta (alist-get 'question item))
        (alist-get 'meta (alist-get 'event item)))))

(defun agent-desk--read-behavior ()
  "Ask how to deliver."
  (completing-read "Deliver how: " agent-desk-behaviors nil t nil nil
                   (car agent-desk-behaviors)))

;;;; Choices

(defun agent-desk--same-key (a b)
  "Whether option keys A and B are the same, ignoring case."
  (eq t (compare-strings a nil nil b nil nil t)))

(defun agent-desk--choice-lines (choices)
  "A compose template for CHOICES: one line per question, with its recommendation."
  (let ((i 0))
    (mapconcat (lambda (q)
                 (setq i (1+ i))
                 (format "%d. %s" i (or (alist-get 'recommended q) "")))
               (alist-get 'questions choices) "\n")))

(defun agent-desk--choice-answer (text choices)
  "Turn compose TEXT into the answer the desk expects for CHOICES.
A line \"N. B\" (or \"N. B, C\") picks options by key, words after the keys
are a note, other words are your own answer, and a question with no line
or an empty one has no answer.  Lines that are not numbered are sent as a
general message after the picks."
  (let* ((qs (append (alist-get 'questions choices) nil))
         (picks (make-vector (length qs) nil))
         (rest '()))
    (dolist (line (split-string text "\n"))
      (cond
       ((string-match "\\`\\s-*\\([0-9]+\\)\\.\\s-*\\(.*\\)\\'" line)
        (let ((n (1- (string-to-number (match-string 1 line)))))
          (cond
           ((and (>= n 0) (< n (length qs))) (aset picks n (string-trim (match-string 2 line))))
           (t (push line rest)))))
       (t (push line rest))))
    (let ((lines
           (cl-loop
            for q in qs for i from 0
            collect
            (let* ((s (or (aref picks i) ""))
                   (opts (append (alist-get 'options q) nil))
                   (key-of (lambda (k) (cl-find-if (lambda (o) (agent-desk--same-key
                                                                 (alist-get 'key o) k))
                                                    opts))))
              (cond
               ((string-empty-p s) (format "%d. (no answer)" (1+ i)))
               ;; Keys may contain hyphens (both-local-first); when the
               ;; hyphenated reading names no option, "B-note" still reads
               ;; as key B with a note.
               ((let ((case-fold-search nil))
                  (cl-some (lambda (k)
                             (and (string-match
                                   (format "\\`\\(%s\\(?:\\s-*,\\s-*%s\\)*\\)\\(?:[ \t:;.-]+\\(.*\\)\\)?\\'" k k) s)
                                  (save-match-data
                                    (not (memq nil (mapcar key-of (split-string (match-string 1 s) "\\s-*,\\s-*" t)))))))
                           '("[[:alnum:]_-]*[[:alnum:]]" "[[:alnum:]]+")))
                (let* ((keys (split-string (match-string 1 s) "\\s-*,\\s-*" t))
                       (note (string-trim (or (match-string 2 s) "")))
                       (os (mapcar key-of keys)))
                  (cond
                   ((and os (not (memq nil os)))
                    (concat (format "%d. %s: %s" (1+ i)
                                    (mapconcat (lambda (o) (alist-get 'key o)) os ", ")
                                    (mapconcat (lambda (o) (alist-get 'label o)) os "; "))
                            (if (string-empty-p note) "" (concat ". Note: " note))))
                   (t (format "%d. Other: %s" (1+ i) s)))))
               (t (format "%d. Other: %s" (1+ i) s)))))))
      (string-trim (concat (string-join lines "\n") "\n\n"
                           (string-trim (string-join (nreverse rest) "\n")))))))

;;;; Compose buffers

(defvar-local agent-desk--compose nil
  "In a compose buffer: a plist of :verb, :target, :choices, :behavior, :windows.")

(defvar agent-desk-compose-mode-map
  (let ((m (make-sparse-keymap)))
    (define-key m (kbd "C-c C-c") #'agent-desk-compose-send)
    (define-key m (kbd "C-c C-k") #'agent-desk-compose-cancel)
    m)
  "Keys in a compose buffer.")

(define-minor-mode agent-desk-compose-mode
  "Write an answer or a message for agent-desk.
\\<agent-desk-compose-mode-map>\\[agent-desk-compose-send] sends and \
\\[agent-desk-compose-cancel] cancels."
  :lighter " Desk"
  :keymap agent-desk-compose-mode-map)

(defun agent-desk--compose (verb target title &optional initial choices behavior)
  "Open a buffer to write VERB (answer or reply) for TARGET, called TITLE.
INITIAL is its starting text; CHOICES the question's multiple-choice set;
BEHAVIOR how to deliver, nil for the desk's setting."
  (let ((windows (current-window-configuration))
        (buf (generate-new-buffer (format "*agent-desk %s %s*" verb target))))
    (pop-to-buffer buf '((display-buffer-below-selected) (window-height . 12)))
    (cond ((fboundp 'markdown-mode) (markdown-mode))
          (t (text-mode)))
    (agent-desk-compose-mode 1)
    (setq agent-desk--compose (list :verb verb :target target :choices choices
                                    :behavior behavior :windows windows))
    (setq header-line-format
          (concat (format "%s %s: %s" (capitalize verb) target title)
                  (when choices "  ·  one line per question: N. KEY [note], or N. your words")
                  (substitute-command-keys
                   "  ·  \\<agent-desk-compose-mode-map>\\[agent-desk-compose-send] send, \\[agent-desk-compose-cancel] cancel")))
    (when initial (insert initial) (goto-char (point-min)) (end-of-line))
    (when (and (featurep 'evil) (fboundp 'evil-insert-state)) (evil-insert-state))
    buf))

(defun agent-desk--compose-close ()
  "Kill the compose buffer and restore the windows it replaced."
  (let ((windows (plist-get agent-desk--compose :windows)))
    (kill-buffer (current-buffer))
    (when windows (set-window-configuration windows))))

(defun agent-desk-compose-send (&optional ask)
  "Send the compose buffer.  With prefix ASK, ask how to deliver it."
  (interactive "P")
  (unless agent-desk--compose (user-error "Not an agent-desk compose buffer"))
  (let* ((c agent-desk--compose)
         (raw (string-trim (buffer-string)))
         (text (cond ((plist-get c :choices) (agent-desk--choice-answer raw (plist-get c :choices)))
                     (t raw)))
         (beh (cond (ask (agent-desk--read-behavior)) (t (plist-get c :behavior)))))
    (when (string-empty-p raw) (user-error "Nothing to send"))
    (agent-desk--compose-close)
    (agent-desk--run (append (list (plist-get c :verb) (plist-get c :target))
                             (when beh (list "--behavior" beh)))
                     (concat text "\n")
                     (agent-desk--say (format "Sent your %s to %s: %%s"
                                              (plist-get c :verb) (plist-get c :target))))))

(defun agent-desk-compose-cancel ()
  "Throw the compose buffer away."
  (interactive)
  (when (or (string-empty-p (string-trim (buffer-string)))
            (y-or-n-p "Discard this text? "))
    (agent-desk--compose-close)))

(with-eval-after-load 'evil
  (when (fboundp 'evil-define-key*)
    (evil-define-key* 'normal agent-desk-compose-mode-map
      "ZZ" #'agent-desk-compose-send
      "ZQ" #'agent-desk-compose-cancel)))

;;;; Commands on the item at point

(defun agent-desk-answer (&optional ask)
  "Answer the question at point.  With prefix ASK, ask how to deliver."
  (interactive "P")
  (let* ((id (agent-desk--need (agent-desk-id-at-point) "item"))
         (_ (unless (string-prefix-p "q-" id) (user-error "%s is not a question" id)))
         (meta (agent-desk--question-meta id))
         (choices (alist-get 'choices meta)))
    (agent-desk--compose "answer" id (org-get-heading t t t t)
                         (when choices (agent-desk--choice-lines choices))
                         choices
                         (when ask (agent-desk--read-behavior)))))

(defun agent-desk-take (&optional ask)
  "Answer the question at point with its default or recommendation.
With prefix ASK, ask how to deliver."
  (interactive "P")
  (let* ((id (agent-desk--need (agent-desk-id-at-point) "item"))
         (_ (unless (string-prefix-p "q-" id) (user-error "%s is not a question" id)))
         (meta (agent-desk--question-meta id))
         (choices (alist-get 'choices meta))
         (default (alist-get 'default meta))
         (text (cond
                ((and choices (cl-every (lambda (q) (alist-get 'recommended q))
                                        (alist-get 'questions choices)))
                 (agent-desk--choice-answer (agent-desk--choice-lines choices) choices))
                (choices (user-error "Not every question of %s has a recommendation" id))
                ((and default (not (string-empty-p default)))
                 (format "Go ahead with your default: %s" default))
                (t "Take your recommendation."))))
    (when (y-or-n-p (format "Answer %s with: %s ? " id text))
      (agent-desk--run (append (list "answer" id)
                               (when ask (list "--behavior" (agent-desk--read-behavior))))
                       (concat text "\n")
                       (agent-desk--say (format "Answered %s: %%s" id))))))

(defun agent-desk-reply (&optional ask)
  "Message the agent behind the item or node at point.
With prefix ASK, ask how to deliver."
  (interactive "P")
  (let ((target (agent-desk--need (agent-desk-target-at-point) "item or node")))
    (agent-desk--compose "reply" target (org-get-heading t t t t) nil nil
                         (when ask (agent-desk--read-behavior)))))

(defun agent-desk-read ()
  "Mark the item at point read; on a question, dismiss it."
  (interactive)
  (let ((id (agent-desk--need (agent-desk-id-at-point) "item")))
    (agent-desk--run (list "read" id) nil (agent-desk--say (format "Read %s %%s" id)))))

(defun agent-desk-unread ()
  "Mark the item at point unread."
  (interactive)
  (let ((id (agent-desk--need (agent-desk-id-at-point) "item")))
    (agent-desk--run (list "unread" id) nil (agent-desk--say (format "Unread %s %%s" id)))))

(defun agent-desk-pin (&optional unpin)
  "Pin the item at point, or unpin it when it is pinned or with prefix UNPIN."
  (interactive "P")
  (let* ((id (agent-desk--need (agent-desk-id-at-point) "item"))
         (verb (cond ((or unpin (agent-desk--pinned-p)) "unpin") (t "pin"))))
    (agent-desk--run (list verb id) nil (agent-desk--say (format "%s %s %%s" (capitalize verb) id)))))

(defun agent-desk-open ()
  "Open the item or node at point the way a notification click does."
  (interactive)
  (let ((target (agent-desk--need (agent-desk-target-at-point) "item or node")))
    (agent-desk--run (list "open" target))))

(defun agent-desk-copy-id ()
  "Copy the id of the item at point, else the node's path."
  (interactive)
  (let ((target (agent-desk--need (agent-desk-target-at-point) "item or node")))
    (kill-new target)
    (message "Copied %s" target)))

(defun agent-desk-inbox ()
  "Render the desk, then visit its inbox."
  (interactive)
  (call-process agent-desk-program nil nil nil "render")
  (find-file (expand-file-name "inbox.org" agent-desk-render-directory)))

(defun agent-desk-refresh ()
  "Render the desk again and revert its buffers."
  (interactive)
  (agent-desk--run (list "render") nil (lambda (_) (message "Desk rendered"))))

;;;; The desk's buffers

(defun agent-desk--desk-file-p (&optional file)
  "Whether FILE (default: this buffer's) is one agent-desk renders."
  (let ((f (or file buffer-file-name)))
    (and f (string-suffix-p ".org" f)
         (file-in-directory-p f agent-desk-render-directory))))

(defun agent-desk--revert ()
  "Revert this desk buffer, keeping point on the same item."
  (let ((id (agent-desk-id-at-point))
        (line (line-number-at-pos))
        (win-start (window-start)))
    (revert-buffer t t t)
    (setq buffer-read-only t)
    (goto-char (point-min))
    (cond
     ((and id (re-search-forward (format "^:DESK_ID: %s$" (regexp-quote id)) nil t))
      (org-back-to-heading t)
      (cond ((fboundp 'org-fold-show-context) (org-fold-show-context))
            (t (with-no-warnings (org-show-context)))))
     (t (forward-line (1- line))))
    (set-window-start (selected-window) win-start t)))

(defun agent-desk-refresh-buffers ()
  "Revert every unmodified buffer visiting a desk file."
  (dolist (b (buffer-list))
    (with-current-buffer b
      (when (and (agent-desk--desk-file-p) (not (buffer-modified-p)))
        (agent-desk--revert)))))

(defvar agent-desk-org-mode-map
  (let ((m (make-sparse-keymap))
        (p (make-sparse-keymap)))
    (define-key p "a" #'agent-desk-answer)
    (define-key p "t" #'agent-desk-take)
    (define-key p "r" #'agent-desk-reply)
    (define-key p "d" #'agent-desk-read)
    (define-key p "U" #'agent-desk-unread)
    (define-key p "p" #'agent-desk-pin)
    (define-key p "o" #'agent-desk-open)
    (define-key p "y" #'agent-desk-copy-id)
    (define-key p "g" #'agent-desk-refresh)
    (define-key p "i" #'agent-desk-inbox)
    (define-key m (kbd "C-c C-d") p)
    m)
  "Keys in desk files: the commands under C-c C-d.")

(define-minor-mode agent-desk-org-mode
  "Act on agent-desk items in its rendered org files.
\\{agent-desk-org-mode-map}"
  :lighter " Desk"
  :keymap agent-desk-org-mode-map
  (cond
   (agent-desk-org-mode
    (setq buffer-read-only t)
    (setq-local auto-revert-verbose nil)
    (auto-revert-mode 1))
   (t (auto-revert-mode -1))))

(defun agent-desk-org-mode-maybe ()
  "Turn on `agent-desk-org-mode' in a file agent-desk renders."
  (when (agent-desk--desk-file-p)
    (agent-desk-org-mode 1)))

(add-hook 'org-mode-hook #'agent-desk-org-mode-maybe)

(with-eval-after-load 'evil
  (when (and agent-desk-evil-keys (fboundp 'evil-define-key*))
    (evil-define-key* 'normal agent-desk-org-mode-map
      "a" #'agent-desk-answer
      "t" #'agent-desk-take
      "r" #'agent-desk-reply
      "d" #'agent-desk-read
      "U" #'agent-desk-unread
      "p" #'agent-desk-pin
      "o" #'agent-desk-open
      "gr" #'agent-desk-refresh)))

(provide 'agent-desk)
;;; agent-desk.el ends here
