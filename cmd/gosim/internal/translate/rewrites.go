package translate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"
)

var rewriteSelectorRegexp = regexp.MustCompile(`[/.]`)

// RewriteSelector rewrites internal/bytealg.Equal to InternalBytealg_Equal
func RewriteSelector(pkg, selector string) string {
	pkgParts := rewriteSelectorRegexp.Split(pkg, -1)
	var rewritten strings.Builder
	for _, part := range pkgParts {
		rewritten.WriteString(strings.ToUpper(part[0:1]))
		rewritten.WriteString(part[1:])
	}
	rewritten.WriteString("_")
	rewritten.WriteString(selector)
	return rewritten.String()
}

func (t *packageTranslator) rewriteImport(c *dstutil.Cursor) {
	node := c.Node()

	imp, ok := node.(*dst.ImportSpec)
	if !ok {
		return
	}

	val, err := strconv.Unquote(imp.Path.Value)
	if err != nil {
		return
	}

	if imp.Name != nil && imp.Name.Name == "." {
		imp.Name = nil
	}

	if replaced, ok := t.replacedPkgs[val]; ok {
		imp.Path.Value = strconv.Quote(replaced)
	}
}

func (t *packageTranslator) rewriteIdentImpl(ident *dst.Ident) {
	lookupPath := ident.Path
	if lookupPath == "" {
		lookupPath = t.pkgPath
	}

	// special case new plan
	/*
		if info, ok := t.globalInfo.ByPackage[lookupPath]; ok {
			if newName, ok := info.Rewritten[ident.Name]; ok {
				if _, ok := t.astMap.Nodes[ident]; !ok {
					// XXX: hack job to skip newly created idents
					return
				}

				ident.Name = newName
				if ident.Path != "" {
					ident.Path = t.replacedPkgs[ident.Path] // XXX: how do we know this?
				}
				return
			}
		}
	*/

	// a test function used as a value, as in t.Run(name, TestFoo), has the
	// new name of its declaration (rewriteTestFunc)
	if ident.Path == "" && t.testFuncs[ident.Name] {
		if astIdent, ok := t.astMap.Nodes[ident].(*ast.Ident); ok {
			if fn, ok := t.typesInfo.Uses[astIdent].(*types.Func); ok && fn.Parent() == fn.Pkg().Scope() {
				ident.Name = t.globalInfo.ByPackage[t.pkgPath].Rewritten[ident.Name]
				return
			}
		}
	}

	// rewrite specific selectors
	selector := packageSelector{Pkg: ident.Path, Selector: ident.Name}
	replacement, ok := replacements[selector]
	if ok && replacement.Pkg == translatedHooksPackage && t.pkgPath == hooksGo127Package {
		// The hooks call the originals they replace elsewhere.
		ok = false
	}
	if !ok {
		// rewrite entire packages
		if replaced, ok := t.replacedPkgs[ident.Path]; ok {
			if ident.Path == "testing" {
				if _, ok := t.astMap.Nodes[ident]; !ok {
					// XXX: hack job to skip newly created idents
					return
				}
			}

			ident.Path = replaced
			ident.Obj = nil
			return
		}
		return
	}

	pkg := replacement.Pkg
	if pkg == "" {
		panic(replacement.Pkg + " " + replacement.Selector)
	}
	// XXX
	// pkg = t.replacedPkgs[pkg]
	// if pkg == "" {
	// panic(replacement.Pkg + " " + replacement.Selector)
	// }

	ident.Name = replacement.Selector
	ident.Path = pkg
	ident.Obj = nil
}

func (t *packageTranslator) rewriteIdent(c *dstutil.Cursor) {
	if ident, ok := c.Node().(*dst.Ident); ok {
		t.rewriteIdentImpl(ident)
	}
}

func (t *packageTranslator) rewriteStdlibEmptyAndLinkname(c *dstutil.Cursor) {
	node := c.Node()

	decl, ok := node.(*dst.FuncDecl)
	if !ok {
		return
	}

	// Hooks replace package-level functions. A method with the same name as a
	// hooked function, such as internal/synctest's Bubble.Run, keeps its body.
	var linkTo packageSelector
	hasHook := false
	if decl.Recv == nil {
		linkTo, hasHook = t.hooks[packageSelector{Pkg: t.pkgPath, Selector: decl.Name.Name}]
	}

	// XXX: this misses some linkname stuff
	// XXX: this wrongly assumes a linkname is going to be part of a FuncDecl's Decs; they can be anywhere in the code

	hasLinkname := false
	// unimplemented is set when the function has no body and its linkname
	// points at a body gosim does not translate.
	unimplemented := false
	for i, val := range decl.Decs.Start {
		if val == "//go:uintptrkeepalive" {
			decl.Decs.Start[i] = "//go:uintptrescapes" // uintptrkeepalive is only allowed in standard library...
		}

		if strings.HasPrefix(val, "//go:linkname") {
			hasLinkname = true

			if hasHook {
				decl.Decs.Start[i] = "//"
				hasLinkname = false
				continue
			}

			parts := strings.Split(val, " ")

			if len(parts) == 2 && decl.Body == nil && nativeLinknamePackages[t.pkgPath] {
				// Call the function the runtime pushes to the original
				// package.
				decl.Decs.Start[i] = fmt.Sprintf("//go:linkname %s %s.%s", decl.Name.Name, t.pkgPath, decl.Name.Name)
			} else if len(parts) == 2 && decl.Body == nil && !pushedLinknames[packageSelector{Pkg: t.pkgPath, Selector: decl.Name.Name}] {
				// The body is pushed from a package gosim does not
				// translate, such as runtime.
				decl.Decs.Start[i] = "// gosim unimplemented: " + val
				unimplemented = true
			}

			if len(parts) == 3 {
				dest := parts[2]
				idx1 := strings.LastIndex(dest, "/") + 1
				idx2 := strings.Index(dest[idx1:], ".")
				if idx2 != -1 {
					idx := idx1 + idx2
					pkg, name := dest[:idx], dest[idx+1:]
					if decl.Body == nil && pkg == t.pkgPath && name == decl.Name.Name {
						// A linkname to itself: the body lives in a package
						// gosim does not translate, such as runtime.
						decl.Decs.Start[i] = "// gosim unimplemented: " + val
						unimplemented = true
					} else if _, ok := runtimeLinknameHooks[packageSelector{Pkg: pkg, Selector: name}]; ok && decl.Body == nil {
						hooks := hooksGo127Package
						if replaced, ok := t.replacedPkgs[hooks]; ok {
							hooks = replaced
						}
						parts[2] = fmt.Sprintf("%s.%s", hooks, RewriteSelector(pkg, name))
						decl.Decs.Start[i] = strings.Join(parts, " ")
					} else if pkg == "reflect" && decl.Body == nil && !isStdPackage(t.pkgPath) {
						// A package outside the standard library that reaches
						// into reflect, as github.com/goccy/go-json does, links
						// to internals that gosim's reflect does not have.
						decl.Decs.Start[i] = "// gosim unimplemented: " + val
						unimplemented = true
					} else if newPkg, ok := t.replacedPkgs[pkg]; ok {
						slog.Debug("rewriting linkname", "pkg", t.pkgPath, "name", decl.Name.Name)

						parts[2] = fmt.Sprintf("%s.%s", newPkg, name)
						decl.Decs.Start[i] = strings.Join(parts, " ")
					} else if t.acceptedLinknames[packageSelector{Pkg: t.pkgPath, Selector: decl.Name.Name}] == (packageSelector{Pkg: pkg, Selector: name}) {
						slog.Debug("keeping linkname", "pkg", t.pkgPath, "name", decl.Name.Name, "targetPkg", pkg, "targetName", name)
					} else if strings.HasPrefix(pkg, "gosimnotranslate/") {
						parts[2] = fmt.Sprintf("%s.%s", strings.TrimPrefix(pkg, "gosimnotranslate/"), name)
						decl.Decs.Start[i] = strings.Join(parts, " ")
					} else if decl.Body == nil {
						decl.Decs.Start[i] = "// gosim unimplemented: " + val
						unimplemented = true
					} else {
						// The function pushes its body into a package gosim
						// does not translate. The translated copy must not
						// replace that package's function.
						slog.Debug("removing linkname", "pkg", t.pkgPath, "name", decl.Name.Name, "targetPkg", pkg, "targetName", name)
						decl.Decs.Start[i] = "// gosim removed: " + val
						hasLinkname = false
					}
				}
			}
		}
	}

	if unimplemented {
		t.stub(decl)
		return
	}

	if decl.Body != nil && hasHook {
		// take over!
		decl.Body = nil
	}

	// XXX: hack
	if decl.Body == nil && !hasLinkname {
		for i, val := range decl.Decs.Start {
			if val == "//go:noescape" {
				decl.Decs.Start[i] = "//"
			}
		}

		if hasHook {
			selector := RewriteSelector(t.pkgPath, decl.Name.Name)

			pkg := linkTo.Pkg
			if replaced, ok := t.replacedPkgs[pkg]; ok {
				// XXX: replace machinePackage properly
				pkg = replaced
			}

			decl.Decs.Start = append(decl.Decs.Start, fmt.Sprintf("//go:linkname %s %s.%s",
				decl.Name.Name, pkg, selector))
			t.needsUnsafe = true

		} else if decl.Name.Name == "newUnixFile" {
			// need a go:linkname that is not required in the standard library...
			decl.Decs.Start = append(decl.Decs.Start, fmt.Sprintf("//go:linkname %s",
				decl.Name.Name))
			t.needsUnsafe = true

		} else if t.keepAsmPkgs[t.pkgPath] {
			slog.Debug("keeping asm header", "pkg", t.pkgPath, "name", decl.Name.Name)
		} else {
			// An assembly function, or a function implemented in runtime.
			t.stub(decl)
		}
	}

	// HACK: insert //go:nocheckptr for Mmap calls in the syscall packages.  The
	// simulation package implements Mmap with a backing slice allocated on the
	// heap, and checkptr is not happy with the unsafe conversion from uintptr
	// to unsafe.Pointer.
	// I believe checkptr can be safely overriden because the simulation keeps
	// the backing slice alive until Munmap is called.
	if decl.Name.Name == "Mmap" && (t.pkgPath == "syscall" || t.pkgPath == "golang.org/x/sys/unix") {
		decl.Decs.Start = append(decl.Decs.Start, "//go:nocheckptr")
	}
}

// stub gives decl a body that fails through gosimruntime.Unimplemented.
func (t *packageTranslator) stub(decl *dst.FuncDecl) {
	for i, val := range decl.Decs.Start {
		if val == "//go:noescape" {
			// Only allowed on functions without a body.
			decl.Decs.Start[i] = "//"
		}
	}

	name := strconv.Quote(t.pkgPath + "." + decl.Name.Name)
	body := []dst.Stmt{&dst.ExprStmt{X: &dst.CallExpr{
		Fun:  t.newRuntimeSelector("Unimplemented"),
		Args: []dst.Expr{&dst.BasicLit{Kind: token.STRING, Value: name}},
	}}}
	if decl.Type.Results != nil && len(decl.Type.Results.List) > 0 {
		// Unimplemented never returns.
		body = append(body, &dst.ExprStmt{X: &dst.CallExpr{
			Fun:  dst.NewIdent("panic"),
			Args: []dst.Expr{&dst.BasicLit{Kind: token.STRING, Value: strconv.Quote("unreachable")}},
		}})
	}
	decl.Body = &dst.BlockStmt{List: body}
}

func removeLinknames(commentLines []string) {
	for i, line := range commentLines {
		if strings.HasPrefix(line, "//go:linkname") {
			commentLines[i] = "// gosim removed: " + line
		}
	}
}

func (t *packageTranslator) rewriteDanglingLinknames(c *dstutil.Cursor) {
	node := c.Node()

	if funcDecl, ok := node.(*dst.FuncDecl); ok {
		endIdx := 0
		for i, line := range slices.Backward(funcDecl.Decs.Start) {
			if line == "\n" {
				endIdx = i
				break
			}
		}
		removeLinknames(funcDecl.Decs.Start[:endIdx])
		removeLinknames(funcDecl.Decs.End)
	}

	if genDecl, ok := node.(*dst.GenDecl); ok {
		for _, spec := range genDecl.Specs {
			removeLinknames(spec.Decorations().Start)
			removeLinknames(spec.Decorations().End)
		}
	}
}

func (t *packageTranslator) rewriteNotifyListHack(c *dstutil.Cursor) {
	if decl, ok := c.Node().(*dst.GenDecl); ok {
		if decl.Tok == token.TYPE {
			for _, spec := range decl.Specs {
				if spec, ok := spec.(*dst.TypeSpec); ok {
					if t.pkgPath == "sync" && spec.Name.Name == "notifyList" {
						spec.Assign = true
						spec.Type = t.newRuntimeSelector("NotifyList")
					}
				}
			}
		}
	}
}

// rewriteBigIntRand rewrites z.Rand(rnd, n) on a *big.Int into a call of
// the hook BigIntRand. math/big is not translated, and its Rand takes the
// native *rand.Rand, where translated code has the translated one.
func (t *packageTranslator) rewriteBigIntRand(c *dstutil.Cursor) {
	call, ok := c.Node().(*dst.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok {
		return
	}
	astSel, ok := t.astMap.Nodes[sel].(*ast.SelectorExpr)
	if !ok {
		return
	}
	selection, ok := t.typesInfo.Selections[astSel]
	if !ok || selection.Kind() != types.MethodVal {
		return
	}
	fn := selection.Obj()
	if fn.Pkg() == nil || fn.Pkg().Path() != "math/big" || fn.Name() != "Rand" {
		return
	}
	if recv := fn.Type().(*types.Signature).Recv(); recv == nil || recv.Type().String() != "*math/big.Int" {
		return
	}
	z := t.apply(sel.X).(dst.Expr)
	if typ, ok := t.getType(sel.X); ok {
		if _, isPtr := typ.Underlying().(*types.Pointer); !isPtr {
			z = &dst.UnaryExpr{Op: token.AND, X: z}
		}
	}
	args := []dst.Expr{z}
	for _, arg := range call.Args {
		args = append(args, t.apply(arg).(dst.Expr))
	}
	c.Replace(&dst.CallExpr{
		Fun:  &dst.Ident{Name: "BigIntRand", Path: translatedHooksPackage},
		Args: args,
	})
}

// isStdPackage reports whether the package is in the standard library,
// whose paths have no dot in their first element.
func isStdPackage(pkgPath string) bool {
	first, _, _ := strings.Cut(pkgPath, "/")
	return !strings.Contains(first, ".")
}

// rewriteConstantLen replaces len(x) and cap(x) that are constants, as for
// an array x, with a constant that translation keeps constant, where
// translation would make x non-constant: when x names a package-level
// variable, which gosim moves into the package's globals, or holds a map
// or channel value, which becomes a call (lenOperand). Go does not evaluate x in such
// a call, so nothing is lost; left alone, the translated x would be
// evaluated, and could panic, and an array length or a const declaration
// would not compile.
//
// The constant is len([N]struct{}{}), with the call's own builtin, len or
// cap, which resolves to the builtin here and names nothing a local could
// hide. The local variables that x uses stay used, in a term that is zero
// and evaluates nothing: + 0*len([K]interface{}{v1, ..., vK}).
func (t *packageTranslator) rewriteConstantLen(c *dstutil.Cursor) {
	call, ok := c.Node().(*dst.CallExpr)
	if !ok || !(t.isNamedBuiltIn(call.Fun, "len") || t.isNamedBuiltIn(call.Fun, "cap")) {
		return
	}
	astCall, ok := t.astMap.Nodes[call].(*ast.CallExpr)
	if !ok {
		return
	}
	tv, ok := t.typesInfo.Types[astCall]
	if !ok || tv.Value == nil {
		return
	}
	becomesCall, locals := t.lenOperand(astCall.Args[0])
	if !becomesCall {
		return
	}
	builtin := call.Fun.(*dst.Ident).Name
	array := func(n string, elt dst.Expr, elts []dst.Expr) dst.Expr {
		return &dst.CallExpr{
			Fun: dst.NewIdent(builtin),
			Args: []dst.Expr{&dst.CompositeLit{
				Type: &dst.ArrayType{Len: &dst.BasicLit{Kind: token.INT, Value: n}, Elt: elt},
				Elts: elts,
			}},
		}
	}
	var folded dst.Expr = array(tv.Value.ExactString(), &dst.StructType{Fields: &dst.FieldList{Opening: true, Closing: true}}, nil)
	if len(locals) > 0 {
		var uses []dst.Expr
		for _, name := range locals {
			uses = append(uses, dst.NewIdent(name))
		}
		folded = &dst.BinaryExpr{
			X:  folded,
			Op: token.ADD,
			Y: &dst.BinaryExpr{
				X:  &dst.BasicLit{Kind: token.INT, Value: "0"},
				Op: token.MUL,
				Y:  array(fmt.Sprint(len(locals)), &dst.InterfaceType{Methods: &dst.FieldList{Opening: true, Closing: true}}, uses),
			},
		}
		folded = &dst.ParenExpr{X: folded}
	}
	folded.Decorations().Start = call.Decs.NodeDecs.Start
	folded.Decorations().End = call.Decs.NodeDecs.End
	c.Replace(folded)
}

// lenOperand reports whether translation makes expr non-constant, as
// rewriteConstantLen describes, and the local variables that expr uses,
// each once, in order. gosim translates every value of a map or channel
// type, a nil one too, into a call; an element of such a type in a
// composite literal may be an untyped nil, so the literal's element type
// counts as well.
func (t *packageTranslator) lenOperand(expr ast.Expr) (becomesCall bool, locals []string) {
	isMapOrChan := func(typ types.Type) bool {
		if typ == nil {
			return false
		}
		switch typ.Underlying().(type) {
		case *types.Map, *types.Chan:
			return true
		}
		return false
	}
	ast.Inspect(expr, func(n ast.Node) bool {
		e, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if tv, ok := t.typesInfo.Types[e]; ok && tv.IsValue() && isMapOrChan(tv.Type) {
			becomesCall = true
		}
		switch e := e.(type) {
		case *ast.Ident:
			if v, ok := t.typesInfo.Uses[e].(*types.Var); ok && !v.IsField() && v.Pkg() != nil {
				if v.Parent() == v.Pkg().Scope() {
					becomesCall = true
				} else if !slices.Contains(locals, e.Name) {
					locals = append(locals, e.Name)
				}
			}
		case *ast.CompositeLit:
			if typ := t.typesInfo.TypeOf(e); typ != nil && len(e.Elts) > 0 {
				if elems, ok := typ.Underlying().(interface{ Elem() types.Type }); ok && isMapOrChan(elems.Elem()) {
					becomesCall = true
				}
			}
		}
		return true
	})
	return becomesCall, locals
}

// rewriteEmbedFS wraps an embed.FS that is converted to an interface of
// the translated io/fs, such as fs.FS, in the hook EmbedFS. gosim does not
// translate embed, so embed.FS implements only the native interfaces.
func (t *packageTranslator) rewriteEmbedFS(c *dstutil.Cursor) {
	expr, ok := c.Node().(dst.Expr)
	if !ok {
		return
	}
	astExpr, ok := t.astMap.Nodes[expr].(ast.Expr)
	if !ok {
		return
	}
	to, ok := t.implicitConversions[astExpr]
	if !ok || !types.IsInterface(to) || to.Underlying().(*types.Interface).Empty() {
		return
	}
	from, ok := t.getType(expr)
	if !ok {
		return
	}
	named, ok := from.(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "embed" || named.Obj().Name() != "FS" {
		return
	}
	c.Replace(&dst.CallExpr{
		Fun:  &dst.Ident{Name: "EmbedFS", Path: translatedHooksPackage},
		Args: []dst.Expr{t.convertedGlobal(expr)},
	})
}
