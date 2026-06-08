// Package desc implements the 'desc' command.
//
// Show package names and one-line descriptions
//
// Display a package's name and one-line description. With --search/--name/
// --description, search formula and cask names and/or descriptions for text
// (or a /regex/). Results are grouped under ==> Formulae and ==> Casks
// headers by default, or printed as plain "name: description" lines with
// --plain.
package desc
