using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Reflection;
using System.Reflection.Metadata;
using System.Reflection.PortableExecutable;
using System.Runtime.CompilerServices;
using System.Runtime.Loader;
using HarmonyLib;

class Program {
    static string OutDir;
    static readonly HashSet<string> Seen = new();
    static int Count;

    static void Capture(byte[] data, string via) {
        if (data == null || data.Length < 2 || data[0] != 0x4D || data[1] != 0x5A) return;
        string name;
        try {
            using var ms = new MemoryStream(data, false);
            using var pe = new PEReader(ms);
            if (!pe.HasMetadata) return;
            var md = pe.GetMetadataReader();
            name = md.GetString(md.GetAssemblyDefinition().Name);
        } catch { return; }
        if (string.IsNullOrEmpty(name) || !Seen.Add(name)) return;
        File.WriteAllBytes(Path.Combine(OutDir, name + ".dll"), data);
        Count++;
        Console.WriteLine($"EXTRACTED:{name}.dll ({data.Length} bytes) via {via}");
    }

    static void CaptureStream(Stream s, string via) {
        if (s == null) return;
        try {
            long pos = s.CanSeek ? s.Position : -1;
            using var ms = new MemoryStream();
            s.CopyTo(ms);
            if (pos >= 0) s.Position = pos;
            Capture(ms.ToArray(), via);
        } catch { }
    }

    public static void PreLoadBytes(byte[] rawAssembly) => Capture(rawAssembly, "Load(byte[])");
    public static void PreLoadBytes2(byte[] rawAssembly) => Capture(rawAssembly, "Load(byte[],byte[])");
    public static void PreLoadStream(Stream assembly) => CaptureStream(assembly, "LoadFromStream");

    static void Main(string[] args) {
        var target = args[0];
        OutDir = args[1];
        Directory.CreateDirectory(OutDir);

        var h = new Harmony("morgue.embedded.extractor");
        void Patch(MethodInfo m, string pre) {
            if (m != null) h.Patch(m, prefix: new HarmonyMethod(typeof(Program).GetMethod(pre)));
        }
        Patch(typeof(Assembly).GetMethod("Load", new[] { typeof(byte[]) }), nameof(PreLoadBytes));
        Patch(typeof(Assembly).GetMethod("Load", new[] { typeof(byte[]), typeof(byte[]) }), nameof(PreLoadBytes2));
        Patch(typeof(AssemblyLoadContext).GetMethod("LoadFromStream", new[] { typeof(Stream) }), nameof(PreLoadStream));

        Assembly asm = Assembly.LoadFrom(target);
        try { RuntimeHelpers.RunModuleConstructor(asm.ManifestModule.ModuleHandle); }
        catch (Exception e) { Console.WriteLine("cctor-warn: " + e.GetType().Name + " " + e.Message); }

        var parts = GetApplicationParts(target).ToList();
        Console.WriteLine($"APPLICATION_PARTS:{parts.Count} -> {string.Join(",", parts)}");
        foreach (var name in parts) {
            try { AssemblyLoadContext.Default.LoadFromAssemblyName(new AssemblyName(name)); }
            catch { try { Assembly.Load(new AssemblyName(name)); } catch { } }
        }
        Console.WriteLine($"EXTRACT_COUNT:{Count}");
    }

    static IEnumerable<string> GetApplicationParts(string path) {
        var res = new List<string>();
        try {
            using var fs = File.OpenRead(path);
            using var pe = new PEReader(fs);
            var md = pe.GetMetadataReader();
            foreach (var ah in md.GetAssemblyDefinition().GetCustomAttributes()) {
                var ca = md.GetCustomAttribute(ah);
                if (!AttrTypeName(md, ca).Contains("ApplicationPart")) continue;
                var v = FirstStringArg(md, ca);
                if (!string.IsNullOrEmpty(v)) res.Add(v);
            }
        } catch (Exception e) { Console.WriteLine("parts-warn: " + e.Message); }
        return res.Distinct();
    }

    static string AttrTypeName(MetadataReader md, CustomAttribute ca) {
        try {
            if (ca.Constructor.Kind == HandleKind.MemberReference) {
                var mr = md.GetMemberReference((MemberReferenceHandle)ca.Constructor);
                if (mr.Parent.Kind == HandleKind.TypeReference)
                    return md.GetString(md.GetTypeReference((TypeReferenceHandle)mr.Parent).Name);
            } else if (ca.Constructor.Kind == HandleKind.MethodDefinition) {
                var mdf = md.GetMethodDefinition((MethodDefinitionHandle)ca.Constructor);
                return md.GetString(md.GetTypeDefinition(mdf.GetDeclaringType()).Name);
            }
        } catch { }
        return "";
    }

    static string FirstStringArg(MetadataReader md, CustomAttribute ca) {
        try {
            var blob = md.GetBlobReader(ca.Value);
            if (blob.ReadUInt16() != 1) return null;
            return blob.ReadSerializedString();
        } catch { return null; }
    }
}
