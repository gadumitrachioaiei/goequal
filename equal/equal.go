// Package equal provides code generator for Equal like functions.
package equal

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/ioutil"
	"log"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

func init() {
	log.SetFlags(log.Llongfile)
}

var goroot string

// getGOROOT returns the value of GOROOT on the installation where go runs.
func getGOROOT() string {
	if goroot != "" {
		return goroot
	}
	if out, err := exec.Command("go", "list", "-f", "{{.Dir}}", "runtime").Output(); err != nil {
		log.Fatalf("can not dermine GOROOT: %s", err)
	} else {
		goroot = strings.TrimSuffix(strings.TrimSpace(string(out)), filepath.Join("src", "runtime"))
	}
	return goroot
}

// header is how we start the comment for generated files.
var header = []byte("// Code generated by goequal for type:")

// goFormat returns the gofmt-ed contents of the Generator's buffer.
func goFormat(content []byte) []byte {
	src, err := format.Source(content)
	if err != nil {
		// Should never happen, but can arise when developing this code.
		// The user can compile the output to see the error.
		log.Printf("warning: internal error: invalid Go generated: %s\n", err)
		log.Printf("warning: compile the package to analyze the error")
		return content
	}
	return src
}

// Type completely identifies a type.
type Type struct {
	name, pkgPath string
}

// code describes generated code: type name, package, function code and needed imports.
type code struct {
	typeName   string
	pkg        *pkg
	code       string
	imports    map[string]struct{} // set with import paths used by this type, used to import other refered types
	stdImports []string            // additional imports from other libraries ( e.g.: we can use from bytes Equal function )
}

func newCode(typeName string, pkg *pkg) *code {
	return &code{
		typeName: typeName,
		pkg:      pkg,
		imports:  make(map[string]struct{}),
	}
}

// serialize generates the final content.
// returns the path and the content to be written on disk.
func (c *code) serialize() (string, []byte) {
	path := filepath.Join(c.pkg.dir, fmt.Sprintf("goequal_%s.go", c.typeName))
	var content bytes.Buffer
	content.WriteString(fmt.Sprintf("// Code generated by goequal for type: %s; DO NOT EDIT\n", c.typeName))
	content.WriteString(fmt.Sprintf("package %s\n", c.pkg.name))
	for importPath := range c.imports {
		localImportName := c.pkg.imports[importPath]
		content.WriteString(fmt.Sprintf("import %s \"%s\"\n", localImportName, importPath))
	}
	content.WriteString(c.code)
	return path, goFormat(content.Bytes())
}

// pkg describes a parsed go package and will return a go node by name.
type pkg struct {
	name, path, dir string                      // path is package import path
	input           interface{}                 // string containing the code for this package, if given use this instead of reading the content of package from disk; testing purposes only
	defs            map[*ast.Ident]types.Object // map go identifiers to nodes
	imports         map[string]string           // maps  import path with local import name ( TODO: local import name can be . ??? )
}

func newPkg(path string, input interface{}) *pkg {
	return &pkg{
		path:    path,
		input:   input,
		imports: make(map[string]string),
	}
}

// testDiscover discovers a package that is delivered only through source code that doesn't exist on disk.
func (p *pkg) testDiscover() []string {
	p.name = p.path[strings.Index(p.path, "/")+1:]
	return []string{"test.go"}
}

// discover discovers files in the package.
// name and dir are needed.
func (p *pkg) discover() []string {
	pkgObj, err := build.Default.Import(p.path, "", 0)
	if err != nil {
		log.Fatalf("cannot process package %s: %s\n", p.path, err)
	}
	p.name = pkgObj.Name
	p.dir = pkgObj.Dir
	files := make([]string, 0, 1)
	for _, fileName := range pkgObj.GoFiles {
		// if this is one of our files, we want it to be skipped from checks, as code that is based upon might have been changed
		if strings.HasPrefix(fileName, "goequal_") {
			content, err := ioutil.ReadFile(filepath.Join(pkgObj.Dir, fileName))
			if err != nil {
				log.Fatalf(err.Error())
			}
			if bytes.HasPrefix(content, header) {
				continue
			}
		}
		files = append(files, filepath.Join(pkgObj.Dir, fileName))
	}
	for _, fileName := range pkgObj.CgoFiles {
		files = append(files, filepath.Join(pkgObj.Dir, fileName))
	}
	return files
}

// check sets all definitions resulted from parsing the package.
func (p *pkg) check() {
	var files []string
	if p.input != nil {
		files = p.testDiscover()
	} else {
		files = p.discover()
	}
	var astFiles []*ast.File
	fs := token.NewFileSet()
	for _, file := range files {
		parsedFile, err := parser.ParseFile(fs, file, p.input, 0)
		if err != nil {
			log.Fatalf("parsing file: %s: %s", file, err)
		}
		// calculate imports by this package
		// do not allow to import the same package under two different names
		for _, importSpec := range parsedFile.Imports {
			importName := ""
			if importSpec.Name != nil {
				importName = importSpec.Name.Name
			}
			path := importSpec.Path.Value[1 : len(importSpec.Path.Value)-1] // remove enclosing quotes
			if oldImportName, ok := p.imports[path]; ok {
				// we simply don't like if the next condition is satisfied
				if oldImportName != importName {
					log.Fatalf("package:%s imports package:%s under two names:%s %s", p.path, path, oldImportName, importName)
				}
			} else {
				p.imports[path] = importName
			}
		}
		astFiles = append(astFiles, parsedFile)
	}
	config := types.Config{Importer: importer.Default(), FakeImportC: true}
	defs := make(map[*ast.Ident]types.Object)
	info := &types.Info{
		Defs: defs,
	}
	_, err := config.Check(p.path, fs, astFiles, info)
	if err != nil {
		log.Fatalf("checking package: %s", err)
	}
	p.defs = defs
}

// findObj returns the type by name.
// stops program if the type is not found.
func (p *pkg) findObj(name string) types.Object {
	if p.defs == nil {
		p.check()
	}
	for key := range p.defs {
		if key.Name == name {
			obj := p.defs[key]
			// we are looking for named types only
			if _, ok := obj.Type().(*types.Named); ok {
				return obj
			}
		}
	}
	log.Fatalf("Type:%s was not found in package:%s", name, p.path)
	return nil
}

// Generator generates the code according to a configuration.
type Generator struct {
	pkgPath, typeName string
	input             map[string]interface{} // map between package path and the code it contains, if given, we use this instead of reading the packages content from disk; test purposes only
	defs              map[string]*pkg        // map pkg import path to pkg objects
	equals            map[Type]*code         // stores generated functions, maps type names with the generated functions
	equalsOrder       []Type                 // contains the ordered type names for which we generated functions, so that we can write them in the same order they were generated
	usedTypes         []Type                 // list with all types put in a stack so we know the current parsed type
	stdOut            bool                   // write to stdout instead of disk
}

// NewGenerator creates a Equal generator for specified type.
func NewGenerator(pkgPath, typeName string, stdOut bool, input map[string]interface{}) *Generator {
	g := Generator{
		pkgPath:  pkgPath,
		typeName: typeName,
		input:    input,
		defs:     make(map[string]*pkg),
		equals:   make(map[Type]*code),
		stdOut:   stdOut,
	}
	return &g
}

// Generate generates the Equal like function.
// It writes the code to disk or on stdout.
func (g *Generator) Generate() {
	g.parse()
	// we should save each type in its package in its own file
	for _, myType := range g.equalsOrder {
		path, content := g.equals[myType].serialize()
		if g.stdOut {
			fmt.Println(string(content))
		} else {
			if err := ioutil.WriteFile(path, content, 0644); err != nil {
				log.Fatalf("Can not save file:%s", path)
			}
		}
	}
}

// buildDefault returns package dir and name.
// It looks in input to check if this is called from tests.
func (g *Generator) buildDefault(pkgPath string) (string, string) {
	if _, isTest := g.input[pkgPath]; isTest {
		return pkgPath, pkgPath
	}
	pkgObj, err := build.Default.Import(pkgPath, "", 0)
	if err != nil {
		log.Fatalf("cannot process package %s: %s\n", pkgPath, err)
	}
	return pkgObj.Dir, pkgObj.Name
}

// getEqualFunctionName returns whether the type is custom and the function name used for equality evaluation.
// If function name is empty string, the type should be ignored.
func (g *Generator) getEqualFunctionName(typ types.Type) (bool, string) {
	switch t := typ.(type) {
	case *types.Named:
		pkgPath := t.Obj().Pkg().Path()
		dir, _ := g.buildDefault(pkgPath)
		if strings.HasPrefix(dir, getGOROOT()) {
			return true, ""
		}
	case *types.Interface:
		importPath := "reflect"
		return true, g.getReferenceUpdateImports(importPath, "DeepEqual")
	case *types.Chan, *types.Signature:
		return true, ""
	}
	return false, ""
}

// parse parses the code and generates each function code and any other needed info for final code.
func (g *Generator) parse() {
	myType := Type{name: g.typeName, pkgPath: g.pkgPath}
	obj := g.findObj(myType)
	g.parseTypeDef(myType, obj)
}

// findObj finds a go ast node by name in specified package.
func (g *Generator) findObj(myType Type) types.Object {
	pkgObj := g.defs[myType.pkgPath]
	if pkgObj == nil {
		pkgObj = newPkg(myType.pkgPath, g.input[myType.pkgPath])
		g.defs[myType.pkgPath] = pkgObj
	}
	return pkgObj.findObj(myType.name)
}

// isPointer returns true if typ is of type pointer.
func (g *Generator) isPointer(typ types.Type) bool {
	switch typ.(type) {
	case *types.Basic, *types.Map, *types.Slice, *types.Pointer, *types.Interface, *types.Chan, *types.Signature:
		return true
	default:
		return false
	}
}

// parseType parses type definitions.
// name is the name of the type.
// obj is the type object the name refers to.
// Generates an Equal<name> method which it stores in equals map.
func (g *Generator) parseTypeDef(myType Type, obj types.Object) {
	var result bytes.Buffer
	typ := obj.Type().Underlying()
	if g.isPointer(typ) {
		result.WriteString(fmt.Sprintf("func Equal%s(t1, t2 %s) bool {\n", myType.name, myType.name))
	} else {
		result.WriteString(fmt.Sprintf("func Equal%s(t1, t2 *%s) bool {\n", myType.name, myType.name))
		// we artificially introduced pointers here, we need to check for non nil
		result.WriteString(fmt.Sprintf("if t1 == t2 {\nreturn true\n}\nif t1 == nil || t2 == nil {\nreturn false\n}\n"))
	}
	// we are storing now that we generate an Equal function so this can not be generated twice
	code := newCode(myType.name, g.defs[myType.pkgPath])
	g.equals[myType] = code
	// store what is the current parsed named type
	g.usedTypes = append(g.usedTypes, myType)
	result.WriteString(g.parseType(myType.name, typ, true, false))
	result.WriteString("return true\n}")
	code.code = result.String()
	g.equalsOrder = append(g.equalsOrder, myType)
	g.usedTypes = g.usedTypes[:len(g.usedTypes)-1]
}

// getArgs returns args used to call Equal functions and whether the args are dereferenced.
func (g *Generator) getArgs(name1, name2 string, isPointer, isPointerReference bool) (string, string, bool) {
	isDereferenced := false
	if isPointer {
		if isPointerReference {
			name1 = fmt.Sprintf("(*%s)", name1)
			name2 = fmt.Sprintf("(*%s)", name2)
			isDereferenced = true
		}
	} else {
		if !isPointerReference {
			name1 = fmt.Sprintf("(&%s)", name1)
			name2 = fmt.Sprintf("(&%s)", name2)
		}
	}
	return name1, name2, isDereferenced
}

// getReferenceUpdateImports returns the fully qualified name relative to pkgPath.
// If the referenced package name is not empty it will update imports for the current code.
func (g *Generator) getReferenceUpdateImports(pkgPath, name string) string {
	// getReferencedPackageName returns the referenced package name in current package
	// returns empty string if the package is the current package
	var getReferencedPackageName = func(pkgPath string) string {
		currentType := g.usedTypes[len(g.usedTypes)-1]
		currentPackagePath := currentType.pkgPath
		if pkgPath == currentPackagePath {
			return ""
		}
		referencePackageName := g.defs[currentPackagePath].imports[pkgPath]
		if referencePackageName == "" {
			// this can happen when using custom configuration
			pkgObj := g.defs[pkgPath]
			if pkgObj == nil {
				// if the referenced package has not been processed yet
				_, referencePackageName = g.buildDefault(pkgPath)
			} else {
				referencePackageName = pkgObj.name
			}
		}
		return referencePackageName
	}
	referencedPackageName := getReferencedPackageName(pkgPath)
	if referencedPackageName == "" {
		return name
	}
	code := g.equals[g.usedTypes[len(g.usedTypes)-1]]
	code.imports[pkgPath] = struct{}{}
	if referencedPackageName == "." {
		return name
	}
	return referencedPackageName + "." + name
}

// parseType parses a types.Type
// name is the name of a variable of type typ or the name of a type with underlying type typ.
func (g *Generator) parseType(name string, typ types.Type, isType bool, isPointerReference bool) string {
	// check for custom type
	if ok, customCall := g.getEqualFunctionName(typ); ok {
		if customCall == "" {
			return ""
		}
		name1, name2 := getNames(name, isType)
		return fmt.Sprintf("if !%s(%s, %s) {\n return false\n}\n", customCall, name1, name2)
	}
	switch t := typ.(type) {
	case *types.Named:
		return g.parseNamed(name, t, isType, isPointerReference)
	case *types.Struct:
		return g.parseStruct(t)
	case *types.Basic:
		name1, name2 := getNames(name, isType)
		return fmt.Sprintf("if %s != %s {\nreturn false\n}\n", name1, name2)
	case *types.Slice:
		return g.parseSlice(name, t, isType)
	case *types.Array:
		return g.parseArray(name, t, isType, isPointerReference)
	case *types.Map:
		return g.parseMap(name, t, isType)
	case *types.Pointer:
		return g.parsePointer(name, t, isType)
	}
	return ""
}

// parseNamed parses a named type.
// It calls parseTypeDef for parsing the new type def and returns the call to the newly generated function.
func (g *Generator) parseNamed(name string, typ *types.Named, isType bool, isPointerReference bool) string {
	name1, name2 := getNames(name, isType)
	typeDecl := typ.Obj()
	myType := Type{name: typeDecl.Name(), pkgPath: typeDecl.Pkg().Path()}
	myCode := g.equals[myType]
	if myCode == nil {
		// findObj is guarenteed to succeed
		g.parseTypeDef(myType, g.findObj(myType))
	}
	funcName := g.getReferenceUpdateImports(myType.pkgPath, "Equal"+myType.name)
	// deal with pointer reference, findObj is guarenteed to succeed
	isPointer := g.isPointer(g.findObj(myType).Type().Underlying())
	callName1, callName2, isDereferenced := g.getArgs(name1, name2, isPointer, isPointerReference)
	// if isDereferenced we have to check for non nil before we pass the args to func
	funcCall := fmt.Sprintf("if !%s(%s, %s) {\nreturn false\n}\n", funcName, callName1, callName2)
	if isDereferenced {
		return fmt.Sprintf("if %s != %s{\nif %s == nil || %s == nil {\nreturn false\n}\n%s}\n", name1, name2, name1, name2, funcCall)
	}
	return funcCall
}

// parseStruct generates code for asserting struct equality.
func (g *Generator) parseStruct(structType *types.Struct) string {
	var result bytes.Buffer
	for i := 0; i < structType.NumFields(); i++ {
		field := structType.Field(i)
		result.WriteString(g.parseType(field.Name(), field.Type(), false, false))
	}
	return result.String()
}

// parseSlice generates code for asserting slice equality
// two slices are considered equal if they have the same length and the same element for every index.
func (g *Generator) parseSlice(name string, sliceType *types.Slice, isType bool) string {
	name1, name2 := getNames(name, isType)
	// check for custom type
	if ok, customCall := g.getEqualFunctionName(sliceType.Elem()); ok && customCall == "" {
		return fmt.Sprintf("if len(%s) != len(%s) {\nreturn false\n}\n", name1, name2)
	}
	t, ok := sliceType.Elem().(*types.Basic)
	if ok && t.Kind() == types.Byte {
		funcName := g.getReferenceUpdateImports("bytes", "Equal")
		return fmt.Sprintf("if !%s(%s, %s) {\n return false\n}\n", funcName, name1, name2)
	}
	var result bytes.Buffer
	result.WriteString(fmt.Sprintf("if len(%s) != len(%s) {\nreturn false\n}\n", name1, name2))
	// we want to find the index of looping through a slice
	// because we can have inner loops, we will name our indexes i1, i2, etc
	index := findNextUsableIndex(name, "i")
	indexName := fmt.Sprintf("i%d", index)
	referenceName := fmt.Sprintf("%s[%s]", name, indexName)
	result.WriteString(fmt.Sprintf("for %s := range %s {\n%s}\n",
		indexName, name1, g.parseType(referenceName, sliceType.Elem(), isType, false)))
	return result.String()
}

// parseArray does what parseSlice does, except first it tries to do basic comparison.
func (g *Generator) parseArray(name string, arrayType *types.Array, isType bool, isPointerReference bool) string {
	// check for custom type
	if ok, customCall := g.getEqualFunctionName(arrayType.Elem()); ok && customCall == "" {
		return ""
	}
	if isType && !isPointerReference {
		// if we are a type, we have to dereference if the type wasn't a pointer
		// this should be true for all types that are not pointers and are comparable
		name = "(*" + name + ")"
	}
	name1, name2 := getNames(name, isType)
	if _, ok := arrayType.Elem().(*types.Basic); ok {
		return fmt.Sprintf("if %s != %s {\nreturn false\n}\n", name1, name2)
	}
	var result bytes.Buffer
	// we want to find the index of looping through a slice
	// because we can have inner loops, we will name our indexes i1, i2, etc
	index := findNextUsableIndex(name, "i")
	indexName := fmt.Sprintf("i%d", index)
	referenceName := fmt.Sprintf("%s[%s]", name, indexName)
	result.WriteString(fmt.Sprintf("for %s := range %s {\n%s}\n",
		indexName, name1, g.parseType(referenceName, arrayType.Elem(), isType, false)))
	return result.String()
}

// parseMap generates code for asserting map equality.
// Two maps are considered equal if they have the same length and the same element for every key.
func (g *Generator) parseMap(name string, mapType *types.Map, isType bool) string {
	name1, name2 := getNames(name, isType)
	var result bytes.Buffer
	result.WriteString(fmt.Sprintf("if len(%s) != len(%s) {\nreturn false\n}\n", name1, name2))
	// we want to find the key of looping through a map
	// because we can have inner loops, we will name our indexes key1, key2, etc
	index := findNextUsableIndex(name, "key")
	keyName := fmt.Sprintf("key%d", index)
	newName := fmt.Sprintf("%s[%s]", name, keyName)
	value1, value2 := getNames(newName, isType)
	// check for custom type
	if ok, customCall := g.getEqualFunctionName(mapType.Elem()); ok && customCall == "" {
		result.WriteString(fmt.Sprintf("for %s := range %s {\nif _, ok := %s[%s]; !ok {\nreturn false\n}\n}\n", keyName, name1, name2, keyName))
		return result.String()
	}
	result.WriteString(fmt.Sprintf(
		"for %s, %s := range %s {\nif %s, ok := %s[%s]; !ok {\nreturn false\n} else {\n%s}\n}\n",
		keyName, value1, name1, value2, name2, keyName, g.parseType(newName, mapType.Elem(), isType, false)))
	return result.String()
}

// parsePointer generates code for asserting pointer equality
// Two pointers are equal if they are equal as pointers or if the values they point to are equal.
func (g *Generator) parsePointer(name string, pointerType *types.Pointer, isType bool) string {
	// we first check if the two pointers are equal
	// name can be a variable, in which case we can just compare the two
	// e.g.: (*f)[0]
	// can also be a type defined as a pointer to another type
	name1, name2 := getNames(name, isType)
	// check for custom type
	if ok, customCall := g.getEqualFunctionName(pointerType.Elem()); ok && customCall == "" {
		return fmt.Sprintf("if %s != %s {\nif %s == nil || %s == nil {\nreturn false\n}\n}\n", name1, name2, name1, name2)
	}
	// generally we dereference the name, but not for named types, because they are handled separately in parseType
	if _, ok := pointerType.Elem().(*types.Named); ok {
		resultParseType := g.parseType(name, pointerType.Elem(), isType, true)
		// we don't check here for non nil pointers because it is done in parseTypeDef or parseType
		return resultParseType
	}
	resultParseType := g.parseType("(*"+name+")", pointerType.Elem(), isType, true)
	return fmt.Sprintf("if %s != %s {\nif %s == nil || %s == nil {\nreturn false\n}\n%s}\n", name1, name2, name1, name2, resultParseType)
}

// findNextUsableIndex finds last int that is preceded by key and is enclosed in square brackets.
// adds 1 to it and returns.
// e.g.: if key is i, we search for: [i1], [i2], etc.
// e.g.: if key is key, we search for: [key1], [key2], etc.
func findNextUsableIndex(name string, key string) int {
	rightSquareBracketIndex := -1
	searched := "[" + key
	searchedLength := len(searched)
	for i := len(name) - 1; i > 0; i-- {
		if name[i] == ']' && name[i-1] != '[' {
			rightSquareBracketIndex = i
			i = i - (searchedLength - 1)
			continue
		}
		if rightSquareBracketIndex > -1 && name[i:i+searchedLength] == searched {
			if index, err := strconv.Atoi(name[i+searchedLength : rightSquareBracketIndex]); err == nil {
				return index + 1
			}
		}
	}
	return 1
}

// findLastUnpairedParantheses finds last unpaired open parantheses.
func findLastUnpairedParantheses(s string) int {
	starts := []int{}
	for i, r := range s {
		switch r {
		case '(':
			starts = append(starts, i)
		case ')':
			starts = starts[:len(starts)-1]
		}
	}
	if len(starts) > 0 {
		return starts[len(starts)-1]
	}
	return -1
}

// getNames returns the names of variables used in testing equality inside an equal function.
// name is the name of original variable or type.
func getNames(name string, isType bool) (string, string) {
	// 1. if we find map indexes we optimize and transform the variable name into value of map
	// e.g: a[key1] -> value11, a[key1][i1] -> value11[i1], (*a[i1][key1])[i2] -> (*value11)[i2]
	// the first digit after value indicates the index of inner loop, the second is 1 or 2, for name1 and name2 respectively
	// if name ends with [key1], etc. we return the value of the map for the key
	// if name contains [key1] we replace the string up to [key1] with value11
	// we start looking from end to start
	if i := strings.LastIndex(name, "[key"); i > -1 {
		for j, r := range name[i+4:] {
			if !unicode.IsDigit(r) {
				if name[i+4+j] == ']' {
					keyNumber := name[i+4 : i+4+j]
					prefix := ""
					for _, r1 := range name[i+4+j+1:] {
						if r1 == ')' {
							if start := findLastUnpairedParantheses(name[:i]); start > -1 {
								prefix = name[:start+2]
							}
						}
					}
					name1 := fmt.Sprintf("%svalue%s1%s", prefix, keyNumber, name[i+4+j+1:])
					name2 := fmt.Sprintf("%svalue%s2%s", prefix, keyNumber, name[i+4+j+1:])
					return name1, name2
				}
				break
			}
		}
	}
	// we calculate what is the actual variable name from name
	// a[i1] -> a, (*a)[i1] -> a, (*(*a)[i1])[i2] -> a
	startVar, endVar := -1, len(name)
	for i, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			if startVar == -1 {
				startVar = i
			}
		} else {
			if startVar > -1 {
				endVar = i
				break
			}
		}
	}
	if startVar == -1 {
		log.Fatalf("Wrong variable name:%s\n", name)
	}
	var name1, name2 string
	if isType {
		// if varName is the name of a type, we replace it with t1 and t2 respectively
		// e.g.: X -> t1, X[key1] -> t1[key1], (*X[i1]) -> (*t1[i1])
		name1 = name[:startVar] + "t1" + name[endVar:]
		name2 = name[:startVar] + "t2" + name[endVar:]
	} else {
		// varName is field name of a struct, we prefix it with t1. and t2. respectively
		name1 = name[:startVar] + "t1." + name[startVar:]
		name2 = name[:startVar] + "t2." + name[startVar:]
	}
	return name1, name2
}
