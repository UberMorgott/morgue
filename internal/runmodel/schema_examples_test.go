package runmodel

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The checked-in examples must roundtrip through the DTOs without unknown
// fields and pass the DTO validators; the schemas must require every
// non-omitempty DTO field. Structural assertions stand in for a JSON Schema
// validator (no production dependency is added).
func TestQualityAndProvenanceExamplesMatchDTOs(t *testing.T) {
	var q QualityReport
	decodeStrict(t, "../../docs/schemas/quality-v1.example.json", &q)
	if err := q.Validate(); err != nil {
		t.Fatalf("quality example invalid: %v", err)
	}
	var p ProvenanceRecord
	decodeStrict(t, "../../docs/schemas/provenance-v1.example.json", &p)
	if p.SchemaVersion != ProvenanceVersion {
		t.Fatalf("provenance example schemaVersion %d", p.SchemaVersion)
	}
	for _, file := range []string{"quality-v1", "events-v2", "morgue-v1"} {
		defs := schemaDefs(t, "../../docs/schemas/"+file+".schema.json")
		for _, typ := range []reflect.Type{reflect.TypeOf(QualityReport{}), reflect.TypeOf(NameQuality{}), reflect.TypeOf(Coverage{}), reflect.TypeOf(Metric{})} {
			requireFields(t, file, defs, typ)
		}
	}
	requireFields(t, "provenance-v1", schemaDefs(t, "../../docs/schemas/provenance-v1.schema.json"), reflect.TypeOf(ProvenanceRecord{}))
}

func decodeStrict(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

type schemaDef struct {
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
}

func schemaDefs(t *testing.T, path string) map[string]schemaDef {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		schemaDef
		Defs map[string]schemaDef `json:"$defs"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	if s.Defs == nil {
		s.Defs = map[string]schemaDef{}
	}
	if s.Properties != nil {
		s.Defs["$root"] = s.schemaDef
	}
	return s.Defs
}

func requireFields(t *testing.T, file string, defs map[string]schemaDef, typ reflect.Type) {
	t.Helper()
	def, ok := defs[typ.Name()]
	if !ok {
		def, ok = defs["$root"]
	}
	if !ok {
		t.Errorf("%s: no schema definition for %s", file, typ.Name())
		return
	}
	required := map[string]bool{}
	for _, r := range def.Required {
		required[r] = true
	}
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if _, ok := def.Properties[name]; !ok {
			t.Errorf("%s: schema %s missing property %s", file, typ.Name(), name)
		}
		if !strings.Contains(tag, ",omitempty") && !required[name] {
			t.Errorf("%s: schema %s does not require %s", file, typ.Name(), name)
		}
	}
}
