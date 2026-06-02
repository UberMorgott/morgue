// cfxstrings — generic string-decryptor pass for ConfuserEx custom string
// decryptors (resource-keyed per-char XOR family) that de4dot's `-p crx` does
// not handle. IL rewriting via AsmResolver; rewrites each
// `ldstr <enc>; ldc.i4 <magic>; call <decryptor>` triplet to a single
// `ldstr <plaintext>` and emits a magic->plaintext TSV.
//
// Scheme (IL-confirmed on the real extracted ServerProtocol.dll oracle — the
// instance decryptor body XORs every UTF-16 unit with the same mask):
//     char[] a = s.ToCharArray();
//     for (i ...) a[i] ^= (char)(key[magic & 0xF] | magic);
//     return new string(a);
// Because every unit uses the SAME mask, forward and reverse XOR are identical,
// so the cipher is symmetric and the key can be recovered from ciphertext alone.
//
// Two decrypt strategies, generically applied:
//   PRIMARY (static, always): confirm the decryptor body matches the per-char
//     XOR shape, then determine the 16-byte key:
//       (a) read the manifest resource the decryptor's declaring type loads via
//           GetManifestResourceStream(<name>) when present; else
//       (b) RECOVER it by printable-output maximization per key index (the real
//           extracted-ServerProtocol case: the key resource is stripped during
//           extraction, so a dynamic invoke of the decryptor would throw).
//   FALLBACK (dynamic, only with --allow-dynamic): when the static shape does
//     NOT match (a different/unknown formula) AND the key resource is present so
//     the cctor can initialise, Assembly.LoadFrom + reflection-invoke the
//     target's own decryptor per call site. EXECUTES TARGET CODE — gated.
//
// Output lines: DECRYPTOR:<name|none>, SHAPE:<xor|unknown>, KEY:<source>...,
// MODE:<static|dynamic|none>, REWROTE:<n>, RESIDUAL:<n> (sites left encrypted).
// Exit 0 even when nothing matched (n may be 0) — that is not an error.

using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Reflection;
using System.Text;
using AsmResolver.DotNet;
using AsmResolver.DotNet.Code.Cil;
using AsmResolver.PE.DotNet.Cil;

internal static class Program
{
    private static int Main(string[] args)
    {
        // Self-test: no external DLL needed. Drives the REAL decode + validation gate
        // (Decrypt, ConfidenceCharWeighted, CrossMagicAgrees, accept decision) over
        // committed vectors so a Go test can assert both corruption-safety directions
        // without the oracle. See SelfTest().
        if (args.Length >= 1 && args[0] == "--selftest")
            return SelfTest();

        if (args.Length < 2)
        {
            Console.Error.WriteLine("usage: cfxstrings <input.dll> <output.dll> [tsv]");
            Console.Error.WriteLine("       cfxstrings --selftest");
            return 2;
        }
        string input = args[0];
        string output = args[1];
        string tsv = null;
        bool allowDynamic = false;
        for (int i = 2; i < args.Length; i++)
        {
            if (args[i] == "--allow-dynamic") allowDynamic = true;
            else tsv = args[i];
        }
        tsv ??= Path.ChangeExtension(output, ".tsv");

        ModuleDefinition module;
        try { module = ModuleDefinition.FromFile(input); }
        catch (Exception e) { Console.Error.WriteLine("load: " + e.Message); return 1; }

        var methods = module.GetAllTypes()
            .SelectMany(t => t.Methods)
            .Where(m => m.CilMethodBody != null)
            .ToList();

        var decryptor = LocateDecryptor(methods);

        // Collect (magic, encrypted) call-site pairs. Shape:
        //   ldstr <enc>; ldc.i4 <magic>; call <decryptor>    (string,int) facade
        // Only meaningful when a decryptor was located AND it is actually invoked
        // inline with a literal string + magic.
        var sites = new List<Site>();
        if (decryptor != null)
        {
            foreach (var m in methods)
            {
                var ins = m.CilMethodBody.Instructions;
                for (int i = 0; i < ins.Count; i++)
                {
                    if (ins[i].OpCode != CilOpCodes.Call) continue;
                    if (ins[i].Operand is not IMethodDescriptor md) continue;
                    if (md.FullName != decryptor.FullName) continue;
                    if (i < 2) continue;
                    if (!ins[i - 1].IsLdcI4()) continue;
                    if (ins[i - 2].OpCode != CilOpCodes.Ldstr || ins[i - 2].Operand is not string enc) continue;
                    sites.Add(new Site(m, i, ins[i - 1].GetLdcI4Constant(), enc));
                }
            }
        }
        // Print DECRYPTOR only when there is an actual inline-string decryptor to
        // act on — an (int)-only or zero-site candidate is not an obfuscator we
        // rewrite, and announcing it would be misleading.
        if (decryptor == null || sites.Count == 0)
        {
            Console.WriteLine("DECRYPTOR:none");
            Console.WriteLine("SHAPE:n/a");
            Console.WriteLine("MODE:none");
            Console.WriteLine("REWROTE:0");
            Console.WriteLine("RESIDUAL:0");
            module.Write(output); // emit a copy so the caller path is uniform
            return 0;
        }
        Console.WriteLine("DECRYPTOR:" + decryptor.FullName);

        // Per-site decrypted plaintext (null = not yet decrypted). Resolved by the
        // static XOR path first, then optionally the dynamic fallback.
        var plain = new string[sites.Count];
        string mode = "none";

        // PRIMARY: static per-char XOR. Applied only when (a) the decryptor body
        // matches the per-char XOR shape in a tight window AND (b) we can locate
        // the key-resource cctor that loads the Byte[] key — both gates protect
        // CLEAN children with an incidental `string Foo(string,int)` helper from
        // being mis-detected and destructively rewritten.
        // Corroboration that the key is resource-backed: the decryptor's type
        // cluster calls GetManifestResourceStream. The resource NAME may itself be
        // an obfuscated/computed string (no literal ldstr), so we capture the
        // literal name when present but only REQUIRE that the resource-loading call
        // exists. This gate + the XOR shape protect clean children.
        bool hasKeyRes = FindKeyResource(module, decryptor, out string keyResName);
        bool xorShape = MatchesXorShape(module, decryptor);
        Console.WriteLine("SHAPE:" + (xorShape ? "xor" : "unknown"));
        Console.WriteLine("KEYRES:" + (hasKeyRes ? (keyResName ?? "computed") : "none"));
        if (xorShape && hasKeyRes)
        {
            byte[] key = ResolveKey(module, keyResName, sites, out int keyLen, out string keySource);
            if ((keyLen & (keyLen - 1)) != 0)
            {
                // index = magic & (keyLen-1) requires a power-of-two key length;
                // bail rather than mis-mask.
                Console.Error.WriteLine($"cfxstrings: key length {keyLen} is not a power of two — refusing to rewrite");
            }
            else
            {
                int keyMask = keyLen - 1; // 16 indices -> magic & 0xF (IL-confirmed)
                var cand = new string[sites.Count];
                for (int i = 0; i < sites.Count; i++)
                    cand[i] = Decrypt(sites[i].Enc, sites[i].Magic, key, keyMask);

                // VALIDATE before trusting — for BOTH resource and recovered keys.
                // A captured "resource" name can be the wrong resource (first 16
                // bytes of unrelated data) and a recovered key can be wrong-but-
                // printable, so neither is exempt from the sanity gate:
                //   - character-weighted meaningfulness >= 0.85 (long strings carry
                //     proportional weight; short strings can't prop up a bad key), AND
                //   - cross-magic agreement: every key index exercised by >= 2
                //     distinct magics must decode meaningfully for all of them.
                bool fromResource = keySource.StartsWith("resource:");
                var magics = sites.Select(s => s.Magic).ToList();
                double conf = ConfidenceCharWeighted(cand);
                bool crossOk = CrossMagicAgrees(magics, cand, keyMask);
                Console.WriteLine($"KEY:{keySource} len={keyLen} bytes=" + BitConverter.ToString(key).Replace("-", "") + $" confidence={conf:0.000} crossmagic={(crossOk ? "ok" : "fail")}");

                bool accept = conf >= 0.85 && crossOk;
                if (!accept && fromResource)
                {
                    // The named resource did not decode cleanly — it likely isn't the
                    // real key resource. Fall back to static recovery and re-validate.
                    Console.WriteLine("KEYNOTE:resource-key rejected by sanity gate — retrying with recovery");
                    key = ResolveKey(module, null, sites, out keyLen, out keySource);
                    for (int i = 0; i < sites.Count; i++)
                        cand[i] = Decrypt(sites[i].Enc, sites[i].Magic, key, keyMask);
                    conf = ConfidenceCharWeighted(cand);
                    crossOk = CrossMagicAgrees(magics, cand, keyMask);
                    fromResource = false;
                    Console.WriteLine($"KEY:{keySource} len={keyLen} bytes=" + BitConverter.ToString(key).Replace("-", "") + $" confidence={conf:0.000} crossmagic={(crossOk ? "ok" : "fail")}");
                    accept = conf >= 0.85 && crossOk;
                }

                if (accept)
                {
                    Array.Copy(cand, plain, sites.Count);
                    mode = fromResource ? "static" : "static-recovered";
                }
                else
                {
                    // Sanity gate failed — do NOT bake plausible garbage in. Leave
                    // encrypted; the dynamic fallback may still resolve them,
                    // otherwise they surface as RESIDUAL.
                    Console.WriteLine($"KEYNOTE:{(keySource.StartsWith("resource:") ? "resource" : "recovered")}-unverified (confidence {conf:0.000} < 0.85 or crossmagic fail; not rewriting statically)");
                }
            }
        }

        // FALLBACK: dynamic reflection-invoke for sites the static path could not
        // resolve (unknown formula, or low-confidence recovered key). Gated behind
        // --allow-dynamic AND only viable when the decryptor can initialise (its
        // cctor reads the key resource); if the resource was stripped this throws
        // and we leave those sites encrypted rather than emitting garbage.
        if (allowDynamic && plain.Any(p => p == null))
        {
            int got = DynamicInvoke(input, decryptor, sites, plain);
            if (got > 0) mode = mode.StartsWith("static") ? mode + "+dynamic" : "dynamic";
        }

        Console.WriteLine("MODE:" + mode);

        var table = new SortedDictionary<int, string>();
        int rewrote = 0, residual = 0;
        for (int i = 0; i < sites.Count; i++)
        {
            var s = sites[i];
            if (plain[i] == null) { residual++; continue; }
            table[s.Magic] = plain[i];
            var ins = s.Method.CilMethodBody.Instructions;
            // Replace the triplet (ldstr enc; ldc.i4 magic; call) with a single
            // ldstr <plain>. Reuse instruction objects (nop the first two, retarget
            // the call) so branch targets / exception handler offsets stay valid.
            ins[s.CallIndex - 2].OpCode = CilOpCodes.Nop;
            ins[s.CallIndex - 2].Operand = null;
            ins[s.CallIndex - 1].OpCode = CilOpCodes.Nop;
            ins[s.CallIndex - 1].Operand = null;
            ins[s.CallIndex].OpCode = CilOpCodes.Ldstr;
            ins[s.CallIndex].Operand = plain[i];
            rewrote++;
        }

        try { module.Write(output); }
        catch (Exception e) { Console.Error.WriteLine("write: " + e.Message); return 1; }

        try
        {
            using var w = new StreamWriter(tsv, false, new UTF8Encoding(false));
            foreach (var kv in table)
                w.WriteLine(kv.Key + "\t" + kv.Value.Replace("\t", "\\t").Replace("\r", "\\r").Replace("\n", "\\n"));
        }
        catch (Exception e) { Console.Error.WriteLine("tsv: " + e.Message); }

        Console.WriteLine($"REWROTE:{rewrote}");
        Console.WriteLine($"RESIDUAL:{residual}");
        return 0;
    }

    // MatchesXorShape confirms the decryptor (following a 1-level facade ->
    // instance method chain if present) contains the per-char XOR shape in a
    // TIGHT window: `ldelem.u1` (key byte) ... `or` ... `xor` ... `stelem.i2`,
    // all within a short instruction span (the inner-loop body), in order. The
    // window bound keeps an unrelated `ldelem.u1` early in the method and a
    // `stelem.i2` far later from being matched as the same pattern. Specific
    // enough to avoid mis-applying XOR to a position-indexed or other scheme.
    private const int XorWindow = 24; // instructions: the IL-confirmed body spans ~14
    private static bool MatchesXorShape(ModuleDefinition module, MethodDefinition decryptor)
    {
        foreach (var body in new[] { decryptor }.Concat(ResolveCallees(decryptor)))
        {
            if (body?.CilMethodBody == null) continue;
            var ins = body.CilMethodBody.Instructions;
            for (int i = 0; i < ins.Count; i++)
            {
                if (ins[i].OpCode != CilOpCodes.Ldelem_U1) continue;
                bool orOp = false, xorOp = false;
                int end = Math.Min(ins.Count, i + 1 + XorWindow);
                for (int j = i + 1; j < end; j++)
                {
                    var op = ins[j].OpCode;
                    if (op == CilOpCodes.Or) orOp = true;
                    else if (op == CilOpCodes.Xor && orOp) xorOp = true;
                    else if (op == CilOpCodes.Stelem_I2 && xorOp) return true;
                }
            }
        }
        return false;
    }

    // StringMeaningful reports whether a single decoded string looks like real
    // text: no residual PUA, no control/replacement noise, and mostly printable
    // ASCII with a reasonable fraction of identifier/URL/text characters. Empty
    // strings decrypt fine and count as meaningful. Used both for the
    // character-weighted batch score and the per-magic cross-check.
    private static bool StringMeaningful(string s)
    {
        if (string.IsNullOrEmpty(s)) return true;
        int printable = 0, idlike = 0, pua = 0, ctrl = 0, total = 0;
        foreach (char c in s)
        {
            total++;
            if (c >= 0xE000 && c <= 0xF8FF) pua++;
            else if (c >= 0x20 && c < 0x7F)
            {
                printable++;
                if (char.IsLetterOrDigit(c) || c == '/' || c == '.' || c == '_' || c == '-' ||
                    c == ':' || c == ' ' || c == '{' || c == '}' || c == '<' || c == '>')
                    idlike++;
            }
            else if (c == '\t' || c == '\r' || c == '\n') printable++;
            else if (char.IsControl(c) || c == '�') ctrl++;
        }
        if (total == 0) return true;
        if (pua != 0 || ctrl != 0) return false;            // hard requirements
        return (double)printable / total >= 0.95 && (double)idlike / total >= 0.6;
    }

    // ConfidenceCharWeighted returns the fraction of CHARACTERS (not strings) that
    // live in meaningful strings. Weighting by characters stops many trivially-
    // passing short strings (1-2 chars are easily id-like) from out-voting a few
    // long correct-looking ones, and vice-versa — closing the near-miss-key gap.
    private static double ConfidenceCharWeighted(string[] decoded)
    {
        long meaningfulChars = 0, totalChars = 0;
        foreach (var s in decoded)
        {
            int len = Math.Max(1, s?.Length ?? 0); // empty still carries weight 1
            totalChars += len;
            if (StringMeaningful(s)) meaningfulChars += len;
        }
        return totalChars == 0 ? 0 : (double)meaningfulChars / totalChars;
    }

    // The minimum acceptable per-index meaningful fraction for a key index that is
    // exercised by >= 2 distinct magics. A WRONG key byte corrupts EVERY string at
    // its index together (the error mask is shared), collapsing that index's
    // meaningful fraction far below this floor; a correct key keeps every index well
    // above it even when some legitimate strings (format specifiers, GUIDs, short
    // tokens) individually fail StringMeaningful. Calibrated against the real
    // ServerProtocol oracle: correct key -> worst index 0.737; single-bit key
    // corruptions that change output -> 0.27..0.63 at the wrong index. (Note: bits
    // masked away by `| magic` are unrecoverable but harmless — they produce
    // byte-identical output, so they neither move this metric nor matter.)
    private const double MinIndexMeaningfulFrac = 0.65;

    // CrossMagicAgrees is an independent, per-index corroboration that catches a key
    // byte wrong in an output-affecting bit: for every key index exercised by >= 2
    // DISTINCT magics, the fraction of that index's strings decoding meaningfully
    // must stay >= MinIndexMeaningfulFrac. A wrong byte fails its own index (all its
    // strings corrupt together); other indices are unaffected, so one bad byte is
    // enough to reject. Indices touched by a single magic can't be cross-checked and
    // are left to the aggregate char-weighted score.
    //
    // This catches GROSS key errors. It cannot catch a near-miss key that maps real
    // text to OTHER real-looking text (statistically indistinguishable) — recovered
    // keys are therefore reported as 'recovered'/lower-confidence, never as proven.
    private static bool CrossMagicAgrees(IReadOnlyList<int> magics, string[] decoded, int keyMask)
    {
        var byIndex = new Dictionary<int, (HashSet<int> magics, List<int> sites)>();
        for (int i = 0; i < magics.Count; i++)
        {
            int idx = magics[i] & keyMask;
            if (!byIndex.TryGetValue(idx, out var g))
            {
                g = (new HashSet<int>(), new List<int>());
                byIndex[idx] = g;
            }
            g.magics.Add(magics[i]);
            g.sites.Add(i);
        }
        foreach (var kv in byIndex)
        {
            if (kv.Value.magics.Count < 2) continue; // not independently checkable
            int good = kv.Value.sites.Count(si => StringMeaningful(decoded[si]));
            double frac = (double)good / kv.Value.sites.Count;
            if (frac < MinIndexMeaningfulFrac) return false;
        }
        return true;
    }

    // ResolveCallees returns the resolvable string-returning methods the given
    // method calls/callvirts (1 level) — the facade -> instance decryptor hop.
    private static IEnumerable<MethodDefinition> ResolveCallees(MethodDefinition m)
    {
        if (m?.CilMethodBody == null) yield break;
        foreach (var ins in m.CilMethodBody.Instructions)
            if ((ins.OpCode == CilOpCodes.Call || ins.OpCode == CilOpCodes.Callvirt)
                && ins.Operand is IMethodDefOrRef mdr)
            {
                MethodDefinition r = null;
                try { r = mdr.Resolve(); } catch { }
                if (r != null && r.Signature?.ReturnType?.FullName == "System.String") yield return r;
            }
    }

    // DynamicInvoke loads the target assembly and reflection-invokes the located
    // decryptor for each still-unresolved site. EXECUTES TARGET CODE. Fills plain[]
    // in place and returns the number newly resolved. On any load/init failure
    // (e.g. stripped key resource) it returns 0 and leaves sites encrypted.
    private static int DynamicInvoke(string input, MethodDefinition decryptor, List<Site> sites, string[] plain)
    {
        Console.Error.WriteLine("cfxstrings: SECURITY: --allow-dynamic fallback executes target code (Assembly.LoadFrom + invoke decryptor)");
        Assembly asm;
        try { asm = Assembly.LoadFrom(Path.GetFullPath(input)); }
        catch (Exception e) { Console.Error.WriteLine("dynamic load: " + e.Message); return 0; }

        MethodInfo mi = null;
        try
        {
            string typeName = decryptor.DeclaringType?.FullName?.Replace('/', '+');
            var t = typeName == null ? null : asm.GetType(typeName, false);
            if (t != null)
                mi = t.GetMethods(BindingFlags.Static | BindingFlags.Public | BindingFlags.NonPublic)
                    .FirstOrDefault(x => x.Name == decryptor.Name && x.ReturnType == typeof(string)
                        && x.GetParameters().Length == decryptor.Signature.ParameterTypes.Count);
        }
        catch (Exception e) { Console.Error.WriteLine("dynamic resolve: " + e.Message); }
        if (mi == null) { Console.Error.WriteLine("dynamic: decryptor not found via reflection"); return 0; }

        var pars = mi.GetParameters();
        int got = 0;
        for (int i = 0; i < sites.Count; i++)
        {
            if (plain[i] != null) continue;
            try
            {
                object[] argv = pars.Length == 2
                    ? new object[] { sites[i].Enc, sites[i].Magic }
                    : new object[] { sites[i].Magic };
                if (mi.Invoke(null, argv) is string r) { plain[i] = r; got++; }
            }
            catch { /* leave encrypted */ }
        }
        Console.Error.WriteLine($"dynamic: resolved {got} sites");
        return got;
    }

    private readonly struct Site
    {
        public readonly MethodDefinition Method;
        public readonly int CallIndex;
        public readonly int Magic;
        public readonly string Enc;
        public Site(MethodDefinition m, int idx, int magic, string enc)
        { Method = m; CallIndex = idx; Magic = magic; Enc = enc; }
    }

    // LocateDecryptor ranks candidate methods by call-site count. A candidate
    // returns System.String and takes either (string,int) or (int). The custom
    // ConfuserEx string decryptor is by far the most-called such method.
    private static MethodDefinition LocateDecryptor(List<MethodDefinition> methods)
    {
        var counts = new Dictionary<string, int>();
        foreach (var m in methods)
            foreach (var ins in m.CilMethodBody.Instructions)
                if (ins.OpCode == CilOpCodes.Call && ins.Operand is IMethodDescriptor md)
                {
                    counts.TryGetValue(md.FullName, out int c);
                    counts[md.FullName] = c + 1;
                }

        MethodDefinition best = null;
        int bestCount = 0;
        foreach (var m in methods)
        {
            var sig = m.Signature;
            if (sig == null) continue;
            if (sig.ReturnType?.FullName != "System.String") continue;
            int pc = sig.ParameterTypes.Count;
            if (pc != 1 && pc != 2) continue;
            // require an int parameter (the magic) — reject string->string identity helpers
            bool hasInt = sig.ParameterTypes.Any(p => p.FullName == "System.Int32");
            if (!hasInt) continue;
            // require at least one string-ish input for the (string,int) facade; the
            // (int)-only shape is allowed too (param is the magic).
            counts.TryGetValue(m.FullName, out int c);
            if (c > bestCount) { bestCount = c; best = m; }
        }
        return bestCount > 0 ? best : null;
    }

    // ResolveKey returns the XOR key bytes. Preference order:
    //   1. The manifest resource the decryptor's type cluster loads via
    //      GetManifestResourceStream(<resName>), when present (first 16 bytes) —
    //      AUTHORITATIVE, used as-is.
    //   2. Static recovery by printable-output maximization, when the resource is
    //      absent/stripped — last resort, the caller validates before trusting.
    private static byte[] ResolveKey(ModuleDefinition module, string resName,
        List<Site> sites, out int keyLen, out string source)
    {
        keyLen = 16;
        // 1. Try the resource named by the cctor (authoritative).
        if (resName != null)
        {
            var res = module.Resources.FirstOrDefault(r => r.Name == resName && r.IsEmbedded);
            if (res != null)
            {
                try
                {
                    byte[] data = res.GetData();
                    if (data != null && data.Length >= 16)
                    {
                        source = "resource:" + resName;
                        var k = new byte[16];
                        Array.Copy(data, k, 16);
                        return k;
                    }
                }
                catch { /* fall through to recovery */ }
            }
        }

        // 2. Recover: for each key index 0..15, the mask applied to every char of a
        // string with magic m is mask = key[m & 15] | m. Per index, pick the key
        // byte that maximises clean printable output across all its strings.
        source = "recovered";
        var k2 = new byte[16];
        for (int idx = 0; idx < 16; idx++)
        {
            var group = sites.Where(s => (s.Magic & 15) == idx).ToList();
            int bestKey = 0, bestScore = int.MinValue;
            for (int kb = 0; kb < 256; kb++)
            {
                int score = 0;
                foreach (var s in group)
                {
                    int mask = (kb | s.Magic) & 0xFFFF;
                    foreach (char c in s.Enc)
                    {
                        char d = (char)(c ^ mask);
                        if (d >= 0x20 && d < 0x7F) score += 2;          // printable ASCII
                        else if (d == '\t' || d == '\r' || d == '\n') score += 1;
                        else if (d >= 0xE000 && d <= 0xF8FF) score -= 4; // residual PUA — bad
                        else score -= 2;
                    }
                }
                if (score > bestScore) { bestScore = score; bestKey = kb; }
            }
            k2[idx] = (byte)bestKey;
        }
        return k2;
    }

    // FindKeyResource reports whether the decryptor's key is loaded from a manifest
    // resource, and captures the literal resource name when one is present. The
    // ctor/cctor that calls `GetManifestResourceStream` lives on the decryptor's
    // declaring type, its nested types, or its enclosing type (the ConfuserEx
    // facade/instance/helper cluster). The resource NAME is often itself an
    // obfuscated string computed at runtime (no literal ldstr) — in that case we
    // still return true with name=null. Returns false only when no resource-loading
    // call exists in the cluster (the key is not resource-backed) — which, combined
    // with the XOR shape, gates whether a static rewrite is attempted at all and so
    // protects clean children from mis-detection.
    private static bool FindKeyResource(ModuleDefinition module, MethodDefinition decryptor, out string name)
    {
        name = null;
        var cluster = new HashSet<TypeDefinition>();
        void AddType(TypeDefinition t)
        {
            if (t == null || !cluster.Add(t)) return;
            foreach (var n in t.NestedTypes) AddType(n);
        }
        var dt = decryptor.DeclaringType;
        AddType(dt);
        if (dt?.DeclaringType != null) AddType(dt.DeclaringType);

        bool found = false;
        foreach (var t in cluster)
        {
            foreach (var m in t.Methods)
            {
                if (m.CilMethodBody == null) continue;
                if (!m.IsConstructor) continue; // cctor or instance ctor
                var ins = m.CilMethodBody.Instructions;
                string lastStr = null;
                for (int i = 0; i < ins.Count; i++)
                {
                    if (ins[i].OpCode == CilOpCodes.Ldstr && ins[i].Operand is string s) lastStr = s;
                    else if ((ins[i].OpCode == CilOpCodes.Callvirt || ins[i].OpCode == CilOpCodes.Call)
                             && ins[i].Operand is IMethodDescriptor md
                             && md.Name == "GetManifestResourceStream")
                    {
                        found = true;
                        if (lastStr != null && name == null) name = lastStr; // literal name if any
                    }
                }
            }
        }
        return found;
    }

    // Decrypt applies the ConfuserEx custom reverse-XOR. Because every unit uses
    // the same mask, forward and reverse XOR are equivalent, so we XOR each unit.
    private static string Decrypt(string enc, int magic, byte[] key, int keyMask)
    {
        int mask = (key[magic & keyMask] | magic) & 0xFFFF;
        var a = enc.ToCharArray();
        for (int i = 0; i < a.Length; i++) a[i] = (char)(a[i] ^ mask);
        return new string(a);
    }

    // ---- Self-test ----------------------------------------------------------
    // Committed corruption-safety check. The vectors below are REAL (ciphertext,
    // magic, plaintext) triplets recorded from the ServerProtocol oracle's custom
    // string decryptor under the recovered key SelfTestKey; ciphertext is stored as
    // UTF-16 code units in hex so it is plain-ASCII and committable. Four key
    // indices (12,13,14,15) carry >= 2 distinct magics, so CrossMagicAgrees is
    // exercised. SelfTest runs the SAME decode + gate (conf >= 0.85 && crossmagic)
    // the rewrite path uses, in two directions:
    //   (a) correct key  -> all plaintexts match, gate ACCEPTs  (would rewrite)
    //   (b) near-miss key -> gate WITHHOLDs (corrupted siblings fail cross-magic)
    // Output is machine-checkable; the Go test asserts SELFTEST-CORRECT:PASS and
    // SELFTEST-NEARMISS:WITHHELD.
    private const string SelfTestKey = "8CE0E84420AA8860E1E6A18400821090";
    private static readonly (int magic, string encHex, string plain)[] SelfTestVectors =
    {
        (62686, "F4AEF4BFF4ADF4ADF4A9F4B1F4ACF4BA", "password"),
        (61582, "F0D7F0EDF0DFF0FDF0EAF0F7F0E8F0FB", "IsActive"),
        (63614, "F83FF80BF80AF816F811F80CF817F804F81FF80AF817F811F810", "Authorization"),
        (57486, "E0CBE0EDE0FBE0ECE0DDE0ECE0FBE0FAE0FBE0F0E0EAE0F7E0FFE0F2E0ED", "UserCredentials"),
        (63036, "F67DF658F658F64EF659F64FF64F", "Address"),
        (61276, "EF0FEF39EF2EEF2AEF39EF2EEF0CEF2EEF33EF28EF33EF3FEF33EF30", "ServerProtocol"),
        (59948, "EA5EEA49EA5FEA5CEA43EA42EA5FEA49EA78EA55EA5CEA49", "responseType"),
        (61695, "F0ACF08BF09EF08BF08AF08C", "Status"),
        (57967, "E2BDE296E291E29EE28DE286E2BBE29EE28BE29E", "BinaryData"),
        (62047, "F29BF2BAF2A9F2B6F2BCF2BAF28CF2BAF2ADF2B6F2BEF2B3F291F2AAF2B2F2BDF2BAF2AD", "DeviceSerialNumber"),
        (59965, "EACDEAD0EACAEACBEADA", "route"),
        (60941, "EEE9EEE6EEE3EEEAEEDBEEF6EEFFEEEA", "fileType"),
        (57621, "E1F1E1DAE1D8E1D0E1CBE1D6E1DEE1CBE1DA", "Negotiate"),
        (62541, "F4BAF4BCF4AAF4BDF4A1F4AEF4A2F4AAF498F4A6F4BBF4A7F48BF4A0F4A2F4AEF4A6F4A1", "usernameWithDomain"),
    };

    private static string HexToUtf16(string hex)
    {
        var sb = new StringBuilder(hex.Length / 4);
        for (int i = 0; i + 4 <= hex.Length; i += 4)
            sb.Append((char)Convert.ToUInt16(hex.Substring(i, 4), 16));
        return sb.ToString();
    }

    private static int SelfTest()
    {
        const int keyMask = 15;
        var magics = SelfTestVectors.Select(v => v.magic).ToList();
        var enc = SelfTestVectors.Select(v => HexToUtf16(v.encHex)).ToArray();

        // Same accept decision as the rewrite path.
        bool Gate(byte[] key, out string[] decoded, out double conf, out bool crossOk)
        {
            decoded = new string[enc.Length];
            for (int i = 0; i < enc.Length; i++)
                decoded[i] = Decrypt(enc[i], magics[i], key, keyMask);
            conf = ConfidenceCharWeighted(decoded);
            crossOk = CrossMagicAgrees(magics, decoded, keyMask);
            return conf >= 0.85 && crossOk;
        }

        int rc = 0;

        // (a) correct key: every plaintext must match AND the gate must ACCEPT.
        byte[] correct = Convert.FromHexString(SelfTestKey);
        bool acc = Gate(correct, out var dec, out double c1, out bool x1);
        bool allMatch = true;
        for (int i = 0; i < dec.Length; i++)
            if (dec[i] != SelfTestVectors[i].plain) { allMatch = false; Console.WriteLine($"MISMATCH[{i}] magic={magics[i]} got=\"{dec[i]}\" want=\"{SelfTestVectors[i].plain}\""); }
        Console.WriteLine($"CORRECT conf={c1:0.000} crossmagic={(x1 ? "ok" : "fail")} accept={acc} plaintextMatch={allMatch}");
        if (acc && allMatch) Console.WriteLine("SELFTEST-CORRECT:PASS");
        else { Console.WriteLine("SELFTEST-CORRECT:FAIL"); rc = 1; }

        // (b) near-miss key: flip an output-affecting bit at index 14 (0x90 ^ 0x40).
        // It garbles the idx-14 siblings (IsActive, UserCredentials) while a couple
        // survive via `| magic` masking — the cross-magic check catches the corrupted
        // ones and the gate WITHHOLDs (no rewrite).
        byte[] near = Convert.FromHexString(SelfTestKey);
        near[14] = (byte)(near[14] ^ 0x40);
        bool acc2 = Gate(near, out _, out double c2, out bool x2);
        Console.WriteLine($"NEARMISS conf={c2:0.000} crossmagic={(x2 ? "ok" : "fail")} accept={acc2}");
        if (!acc2) Console.WriteLine("SELFTEST-NEARMISS:WITHHELD");
        else { Console.WriteLine("SELFTEST-NEARMISS:ACCEPTED(BUG)"); rc = 1; }

        return rc;
    }
}
