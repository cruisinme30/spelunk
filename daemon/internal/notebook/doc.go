// Package notebook reads Jupyter notebooks (.ipynb) as the text of their
// cells, so a notebook is searched, previewed and diffed by the code and
// notes people wrote in it rather than by its JSON.
//
// A notebook's text is each cell's source in turn, each followed by a line
// end. A cell of n lines is n lines of the text, as it is n lines in VS
// Code's notebook editor, so a line of the text maps to a cell and a line in
// it by counting. When text outputs are searched too, they come after every
// cell's source, so the source lines number the same either way.
package notebook
