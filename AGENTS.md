# Notes for agents working on agent-desk

## The dashboard's keyboard

The dashboard is driven from the keyboard in the style of Doom Emacs, following
the [evilify skill](https://github.com/NightMachinery/evilify-skill) (a private
repository; the rule below stands on its own).

Every new page, panel or action gets a named command in
`web/app/src/lib/keymap.js` in the same change that adds it, and a key where it
earns one. A command without a key is still reachable through `SPC SPC` and
`M-x`; a page with no command is reachable only by mouse. How to define
commands and keys: [docs/keyboard.md](docs/keyboard.md), "For developers".
