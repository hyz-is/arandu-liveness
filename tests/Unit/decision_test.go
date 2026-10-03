package unit_test

import (
	"go/ast"
	"reflect"
	"regexp"
	"testing"

	liveness "github.com/hyz-is/arandu-liveness"
)

// TestNothingHereDecides holds the rule this package exists under: it returns
// separate, auditable signals, and the decision belongs to the application's
// policy engine.
//
// Two shapes would break it, and both are visible in syntax. A result type
// carrying a verdict -- a field called IsLive, Genuine, Approved, Decision --
// is a decision with a model's name on it. A function that runs several
// stages behind one name -- CheckLiveness, VerifyPresence -- hides which
// signal said what. The names are what an assistant reaches for first, which
// is why they are refused by name. "Completed" and "WithinReference" are
// allowed: they say a measurement met a versioned reference, not what the
// subject is.
func TestNothingHereDecides(t *testing.T) {
	t.Parallel()

	verdict := regexp.MustCompile(`(?i)(match|verif|approv|reject|decision|decide|verdict|accept|liveness|islive|isreal|isfake|isspoof|spoofed|genuine|passed|fraud|authentic)`)
	for _, source := range productionGoFiles(t, packageRoot(t)) {
		ast.Inspect(source.file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.FuncDecl:
				if node.Name.IsExported() && verdict.MatchString(node.Name.Name) {
					t.Errorf("%s declares %s, a name that decides or hides stages", source.path, node.Name.Name)
				}
			case *ast.TypeSpec:
				if node.Name.IsExported() && verdict.MatchString(node.Name.Name) {
					t.Errorf("%s declares the type %s, a name that decides", source.path, node.Name.Name)
				}
				if st, ok := node.Type.(*ast.StructType); ok {
					for _, field := range st.Fields.List {
						for _, name := range field.Names {
							if name.IsExported() && verdict.MatchString(name.Name) {
								t.Errorf("%s: %s.%s is a verdict field", source.path, node.Name.Name, name.Name)
							}
						}
					}
				}
			}
			return true
		})
	}
}

// TestThePADAndDeepfakeResultsHoldNoBoolean holds the stricter rule for the
// model-backed results: a PADResult or a DeepfakeSignal carries numbers, a
// class index, provenance and a reference -- and no boolean anywhere in it,
// because a boolean on a model's output is a decision whatever it is called.
func TestThePADAndDeepfakeResultsHoldNoBoolean(t *testing.T) {
	t.Parallel()

	var walk func(path string, typ reflect.Type, seen map[reflect.Type]bool)
	walk = func(path string, typ reflect.Type, seen map[reflect.Type]bool) {
		switch typ.Kind() {
		case reflect.Bool:
			t.Errorf("%s is a boolean", path)
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(path+"[]", typ.Elem(), seen)
		case reflect.Map:
			walk(path+"[key]", typ.Key(), seen)
			walk(path+"[]", typ.Elem(), seen)
		case reflect.Struct:
			if seen[typ] {
				return
			}
			seen[typ] = true
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				walk(path+"."+f.Name, f.Type, seen)
			}
		}
	}
	walk("PADResult", reflect.TypeOf(liveness.PADResult{}), map[reflect.Type]bool{})
	walk("DeepfakeSignal", reflect.TypeOf(liveness.DeepfakeSignal{}), map[reflect.Type]bool{})
}
