// Package render turns the model into output. Markdown goes through
// text/template, never html/template, whose escaping corrupts Markdown
// punctuation and URLs.
package render
