package main

import (
	"go/ast"
	"go/parser"
	"go/token"
)

func parseFile(fset *token.FileSet, name string, src []byte) (*ast.File, error) {
	return parser.ParseFile(fset, name, src, parser.ParseComments)
}
