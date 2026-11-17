// godownfix rewrites Go 1.26+ constructs into Go 1.25 equivalents, type-aware.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

const godownPrefix = "github.com/sagernet/tailscale/internal/godown/std/"

type edit struct {
	start, end int
	text       string
}

func main() {
	cfg := &packages.Config{
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Tests:      false,
		BuildFlags: strings.Fields(os.Getenv("GODOWN_FLAGS")),
	}
	pkgs, err := packages.Load(cfg, os.Args[1:]...)
	if err != nil {
		panic(err)
	}
	done := map[string]bool{}
	total := 0
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			fmt.Fprintln(os.Stderr, "pkg error:", e)
		}
		for i, file := range pkg.Syntax {
			_ = i
			name := pkg.Fset.File(file.Pos()).Name()
			if done[name] || !strings.HasSuffix(name, ".go") {
				continue
			}
			done[name] = true
			n := fixFile(pkg, file, name)
			total += n
		}
	}
	fmt.Fprintln(os.Stderr, "rewrites:", total)
}

func fixFile(pkg *packages.Package, file *ast.File, name string) int {
	src, err := os.ReadFile(name)
	if err != nil {
		return 0
	}
	fset := pkg.Fset
	base := fset.File(file.Pos()).Base()
	off := func(p token.Pos) int { return int(p) - base }
	info := pkg.TypesInfo
	var edits []edit
	imports := map[string]string{} // path -> alias
	alias := func(path string) string {
		if a, ok := imports[path]; ok {
			return a
		}
		last := path[strings.LastIndex(path, "/")+1:]
		a := "godown" + strings.NewReplacer(".", "", "-", "").Replace(last)
		if strings.HasPrefix(path, godownPrefix) {
			a = "godown" + last
		}
		imports[path] = a
		return a
	}
	qual := func(p *types.Package) string {
		if p == pkg.Types {
			return ""
		}
		return alias(p.Path())
	}
	text := func(n ast.Node) string { return string(src[off(n.Pos()):off(n.End())]) }
	isType := func(e ast.Expr) bool {
		tv, ok := info.Types[e]
		return ok && tv.IsType()
	}
	namedIs := func(t types.Type, pkgPath, typeName string) bool {
		t = types.Unalias(t)
		if p, ok := t.(*types.Pointer); ok {
			_ = p
			return false
		}
		n, ok := t.(*types.Named)
		return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == pkgPath && n.Obj().Name() == typeName
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// new(expr)
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "new" && len(call.Args) == 1 {
			if b, ok := info.Uses[id].(*types.Builtin); ok && b.Name() == "new" && !isType(call.Args[0]) {
				t := info.TypeOf(call.Args[0])
				if t != nil {
					if basic, ok := t.(*types.Basic); ok && basic.Info()&types.IsUntyped != 0 {
						t = types.Default(t)
					}
					ts := types.TypeString(t, qual)
					edits = append(edits, edit{off(call.Pos()), off(call.End()), fmt.Sprintf("func() *%s { godownValue := %s; return &godownValue }()", ts, text(call.Args[0]))})
					return false
				}
			}
		}
		fun := call.Fun
		switch f := fun.(type) {
		case *ast.IndexExpr:
			fun = f.X
		case *ast.IndexListExpr:
			fun = f.X
		}
		sel, ok := fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// errors.AsType
		if x, ok := sel.X.(*ast.Ident); ok {
			if pn, ok := info.Uses[x].(*types.PkgName); ok && pn.Imported().Path() == "errors" && sel.Sel.Name == "AsType" {
				edits = append(edits, edit{off(sel.Pos()), off(sel.End()), alias(godownPrefix+"errors") + ".AsType"})
				return true
			}
		}
		// method rewrites
		s := info.Selections[sel]
		if s == nil || s.Kind() != types.MethodVal {
			return true
		}
		recv := s.Recv()
		var fn string
		switch {
		case namedIs(recv, "reflect", "Type") && (sel.Sel.Name == "Fields" || sel.Sel.Name == "Methods" || sel.Sel.Name == "Ins" || sel.Sel.Name == "Outs"):
			fn = alias(godownPrefix+"reflect") + ".Type" + sel.Sel.Name
		case namedIs(recv, "reflect", "Value") && (sel.Sel.Name == "Fields" || sel.Sel.Name == "Methods"):
			fn = alias(godownPrefix+"reflect") + ".Value" + sel.Sel.Name
		case namedIs(recv, "net/netip", "Prefix") && sel.Sel.Name == "Compare":
			fn = alias(godownPrefix+"net/netip") + ".PrefixCompare"
		}
		if fn == "" {
			return true
		}
		args := []string{text(sel.X)}
		for _, a := range call.Args {
			args = append(args, text(a))
		}
		edits = append(edits, edit{off(call.Pos()), off(call.End()), fn + "(" + strings.Join(args, ", ") + ")"})
		return false
	})
	if len(edits) == 0 {
		return 0
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := src
	for _, e := range edits {
		out = append(append(append([]byte{}, out[:e.start]...), e.text...), out[e.end:]...)
	}
	// add imports
	nfset := token.NewFileSet()
	nf, err := parseFile(nfset, name, out)
	if err != nil {
		fmt.Fprintln(os.Stderr, name, err)
		os.WriteFile(name, out, 0o644)
		return len(edits)
	}
	for path, a := range imports {
		astutil.AddNamedImport(nfset, nf, a, path)
	}
	var buf bytes.Buffer
	if err := format.Node(&buf, nfset, nf); err != nil {
		panic(err)
	}
	os.WriteFile(name, buf.Bytes(), 0o644)
	fmt.Fprintln(os.Stderr, name, len(edits), strconv.Quote(""))
	return len(edits)
}
