# M4 family contracts

- Quality v1 and provenance v1 use lowerCamelCase JSON. Unknown counts are null, never zero. Regex source declarations are estimated; they cannot yield bytecode coverage ratios.
- Register collectors with quality.Register(family, collector). Family keys: managed, java, native, delphi, ue5, il2cpp, unitydata, container. ForFamily returns nil for unregistered families; integration must surface missing evidence.
- Provenance sinks stream one record per line. A nil recipe.Context.Provenance is supported. The engine binds targetId/inputSha256 when needed; recipes retain artifact-relative paths, module identity and available token/RVA/address.
- Producer step IDs: managed.decompile (ilspycmd), managed.deobfuscate (de4dot-cex), managed.strings (cfxstrings), managed.extract (cfxextract), java.decompile (vineflower), native.decompile (ghidra), native.rename (morgue), il2cpp.stubs (il2cppdumper/il2cppinspector), unity.extract (assetripper). Embedded helpers use their content identity as version; never pretend they are external-tool versions.
- Fidelity constants: source, decompiled-source, pseudocode, stub. Metadata/assets retain their existing artifact kinds. Source means original source; ILSpy/Vineflower emit decompiled-source, Ghidra emits pseudocode, DummyDLL C# emits stub.
- Metric.Validate and Coverage.Validate reject absent counts, invalid unknowns and fabricated ratios. Comparable is an explicit assertion of a shared stable identity universe.
