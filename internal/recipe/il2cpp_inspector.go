package recipe

// inspectorSupports reports whether Il2CppInspectorRedux handles a metadata
// version. Ranges per the InspectorRedux CLI: 16–24, 27–29, 31, 35, 38, 39,
// 104–106. (Minor sub-versions like 24.5/29.1 share the integer major.)
func inspectorSupports(v int32) bool {
	switch {
	case v >= 16 && v <= 24:
		return true
	case v >= 27 && v <= 29:
		return true
	case v == 31 || v == 35 || v == 38 || v == 39:
		return true
	case v >= 104 && v <= 106:
		return true
	}
	return false
}

// dumperOrder returns the dumper tool names to try, in order, for a metadata
// version. Default policy: InspectorRedux first when available AND it supports
// the version; legacy Il2CppDumper as fallback. When InspectorRedux can't
// handle the version, try the legacy dumper first (it covers some older sets),
// then InspectorRedux as a last resort if installed.
func dumperOrder(version int32, inspectorAvailable bool) []string {
	if !inspectorAvailable {
		return []string{"il2cppdumper"}
	}
	if inspectorSupports(version) {
		return []string{"il2cppinspector", "il2cppdumper"}
	}
	return []string{"il2cppdumper", "il2cppinspector"}
}
