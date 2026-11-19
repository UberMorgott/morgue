package recon

import "testing"

// DiE 4.0.0 `diec -j` shape: detections nested under detects[].values.
func TestApplyDiEJSONNested(t *testing.T) {
	out := `{"detects":[{"filetype":"PE32","parentfilepart":"Header","values":[
		{"name":"Microsoft Visual C/C++","type":"compiler","version":"19.50"},
		{"name":"UPX","type":"packer","version":"4.2"},
		{"name":"ConfuserEx","type":"protector","version":""}]}]}`
	var r Result
	applyDiEJSON(&r, out)
	if r.Compiler != "Microsoft Visual C/C++ 19.50" || !r.Packed || r.Obfuscator != "UPX" {
		t.Fatalf("got compiler=%q packed=%v obf=%q", r.Compiler, r.Packed, r.Obfuscator)
	}
}
