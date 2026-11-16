#!/bin/zsh -f
# Renders the app icons in public/ from the favicon's drawing, with headless
# Chrome (scripts/shot.mjs). Run it after changing the icon; commit the PNGs.
#   icon-192.png, icon-512.png   the rounded square, transparent corners
#   maskable-512.png             full bleed, the glyph inside the 80% safe zone
#   apple-touch-icon.png         180 px, full bleed (iOS rounds it)
set -e
cd ${0:A:h}/..
glyph='<path d="M7 17.5 9.6 9.4A2 2 0 0 1 11.5 8h9a2 2 0 0 1 1.9 1.4l2.6 8.1V22a2 2 0 0 1-2 2H9a2 2 0 0 1-2-2z" fill="none" stroke="#fff" stroke-width="2" stroke-linejoin="round"/><path d="M7.5 17.5h5l1.2 2.5h4.6l1.2-2.5h5" fill="none" stroke="#fff" stroke-width="2" stroke-linejoin="round"/>'
rounded="<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'><rect width='32' height='32' rx='8' fill='#c2410c'/>${glyph}</svg>"
# Full bleed, glyph shrunk to 70% about the centre.
bleed="<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'><rect width='32' height='32' fill='#c2410c'/><g transform='translate(16 16) scale(0.7) translate(-16 -16)'>${glyph}</g></svg>"
render() { # svg size out
  local page="<!doctype html><meta name=viewport content='width=device-width'><style>html,body{margin:0}svg{display:block;width:100vw;height:100vh}</style>${1}"
  node scripts/shot.mjs "data:text/html;charset=utf-8,${page//\#/%23}" ${3} --width ${2} --height ${2} --scale 1 --wait 300 --transparent
}
render ${rounded} 192 public/icon-192.png
render ${rounded} 512 public/icon-512.png
render ${bleed} 512 public/maskable-512.png
render ${bleed} 180 public/apple-touch-icon.png
