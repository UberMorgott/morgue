// cfxcflow — control-flow deobfuscation pass for ConfuserEx-family switch
// flattening driven by pure stateless constant-provider methods that de4dot does
// not remove. IL rewriting via AsmResolver. SAFE BY CONSTRUCTION: a method is
// rewritten only when the transform is PROVEN semantics-preserving; any unproven
// or ambiguous method is left byte-identical, so the worst case is a no-op. After
// rewriting, the output module must reparse/validate or the INPUT is emitted
// unchanged (VERIFY:fail). Static-only: this tool NEVER executes target code.
//
// CLI:  cfxcflow <input.dll> <output.dll> [report.tsv]
//       cfxcflow --selftest
// Stdout markers (exit 0 even when nothing matched):
//   FAMILY:<name>  PROVIDERS:<n>  METHODS:<n>  DEFLATTENED:<n>  BLOCKSREMOVED:<n>
//   FOLDED:<n>  WITHHELD:<n>  VERIFY:ok|fail  MODE:none
//
// v1 family: const-switch-flatten. A "dispatcher" method has a flattening `switch`
// whose selector local `s` is a compile-time constant at every definition: either
// `ldc.i4 k` or `provider(literal)` (a pure stateless int->int method), optionally
// combined with inline literal xor/add. Prove constant-propagates `s` from method
// entry through the dispatcher, computing the single concrete successor for each
// visit; if every visit is constant and the state machine is finite, Apply folds
// the provider calls to ldc.i4 and rewrites the dispatcher switch to a direct `br`
// to the proven successor. Anything non-constant / runtime-dependent is WITHHELD.

using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Text;
using AsmResolver.DotNet;
using AsmResolver.DotNet.Code.Cil;
using AsmResolver.DotNet.Collections;
using AsmResolver.DotNet.Signatures;
using AsmResolver.PE.DotNet.Cil;
using AsmResolver.PE.DotNet.Metadata.Tables.Rows;

internal static class Program
{
    private const string FamilyName = "const-switch-flatten";

    private static int Main(string[] args)
    {
        if (args.Length >= 1 && args[0] == "--selftest")
            return SelfTest();

        if (args.Length < 2)
        {
            Console.Error.WriteLine("usage: cfxcflow <input.dll> <output.dll> [report.tsv]");
            Console.Error.WriteLine("       cfxcflow --selftest");
            return 2;
        }

        string input = args[0];
        string output = args[1];
        string tsv = args.Length >= 3 ? args[2] : null;

        ModuleDefinition module;
        try { module = ModuleDefinition.FromFile(input); }
        catch (Exception e) { Console.Error.WriteLine("load: " + e.Message); return 1; }

        var report = new List<string>();
        var stats = Run(module, report);

        // MODE:none only when the pass did NO work at all — neither a true de-flatten
        // nor any safe constant fold. Folding is real (readability) work, so a fold-
        // only run is NOT "none".
        if (stats.Deflattened == 0 && stats.Folded == 0)
            Console.WriteLine("MODE:none");

        // Emit. If post-verify fails, discard rewrites and re-emit the INPUT
        // unchanged so the worst case is a byte-identical no-op.
        bool verifyOk;
        try
        {
            module.Write(output);
            verifyOk = Verify(output);
        }
        catch (Exception e)
        {
            Console.Error.WriteLine("write: " + e.Message);
            verifyOk = false;
        }

        if (!verifyOk)
        {
            // Re-emit the INPUT bytes unchanged. Reload to drop any partial edits.
            try
            {
                File.Copy(input, output, overwrite: true);
            }
            catch (Exception e) { Console.Error.WriteLine("fallback-copy: " + e.Message); return 1; }
        }

        PrintMarkers(stats, verifyOk);

        if (tsv != null)
        {
            try
            {
                using var w = new StreamWriter(tsv, false, new UTF8Encoding(false));
                w.WriteLine("method_full_name\tstatus\tblocks_before\tblocks_after\treason");
                foreach (var line in report) w.WriteLine(line);
            }
            catch (Exception e) { Console.Error.WriteLine("tsv: " + e.Message); }
        }

        return 0;
    }

    private static void PrintMarkers(Stats s, bool verifyOk)
    {
        if (s.Deflattened > 0 || s.Folded > 0) Console.WriteLine("FAMILY:" + FamilyName);
        Console.WriteLine("PROVIDERS:" + s.Providers);
        Console.WriteLine("METHODS:" + s.Methods);
        Console.WriteLine("DEFLATTENED:" + s.Deflattened);
        Console.WriteLine("BLOCKSREMOVED:" + s.BlocksRemoved);
        Console.WriteLine("FOLDED:" + s.Folded);
        Console.WriteLine("WITHHELD:" + s.Withheld);
        Console.WriteLine("VERIFY:" + (verifyOk ? "ok" : "fail"));
    }

    private struct Stats
    {
        public int Providers, Methods, Deflattened, BlocksRemoved, Folded, Withheld;
    }

    // Run drives the family over every method body: identify pure providers
    // module-wide (cached), then per method try Detect -> Prove -> Apply.
    private static Stats Run(ModuleDefinition module, List<string> report)
    {
        var stats = new Stats();

        var allMethods = module.GetAllTypes()
            .SelectMany(t => t.Methods)
            .Where(m => m.CilMethodBody != null)
            .ToList();

        var providers = IdentifyProviders(allMethods);
        stats.Providers = providers.Count;

        // Methods deflattened in the prove loop (skip them in the fold loop), and
        // methods that matched the flattening switch but were WITHHELD from
        // linearization (they may still fold; if not, they get a `withheld` row).
        var deflattened = new HashSet<MethodDefinition>();
        var withheldMatched = new HashSet<MethodDefinition>();

        foreach (var m in allMethods)
        {
            stats.Methods++;

            Cfg cfg;
            try { cfg = Cfg.Build(m); }
            catch { continue; }

            if (!TryFindFlatteningSwitch(m, cfg, providers, out var sw, out var stateLocal))
                continue;

            Plan plan;
            try { plan = Prove(m, cfg, providers, sw, stateLocal); }
            catch { plan = null; }

            int blocksBefore = cfg.Blocks.Count;
            if (plan == null)
            {
                // Matched-but-not-proven: WITHHELD from linearization. The method may
                // still be safely constant-folded by the module-wide fold pass below;
                // if it folds it gets a `folded` row there, otherwise a `withheld` one.
                stats.Withheld++;
                withheldMatched.Add(m);
                continue;
            }

            try
            {
                Apply(m, plan);
                stats.Deflattened++;
                stats.BlocksRemoved += plan.RemovedBlocks;
                stats.Folded += plan.FoldSites.Count;
                deflattened.Add(m);
                int blocksAfter = Math.Max(1, blocksBefore - plan.RemovedBlocks);
                report.Add($"{m.FullName}\tdeflattened\t{blocksBefore}\t{blocksAfter}\tproven folded={plan.FoldSites.Count}");
            }
            catch (Exception e)
            {
                // Per-method isolation: an Apply that throws drops this method to
                // untouched. The instructions may be partially mutated, so we cannot
                // trust this body — but the module-level post-verify gate will catch
                // any resulting corruption and discard ALL rewrites.
                stats.Withheld++;
                report.Add($"{m.FullName}\twithheld\t{blocksBefore}\t{blocksBefore}\tapply-exception:{e.GetType().Name}");
            }
        }

        // Module-wide safe constant-fold: every `ldc.i4 <lit>; call <pure provider>`
        // triplet folds to `ldc.i4 <const>` UNCONDITIONALLY — the provider is proven
        // pure and side-effect-free and the arg is a literal, so the call's result is
        // a compile-time constant whatever the surrounding control flow. This is the
        // SAME proven nop+retarget technique cfxstrings uses for string sites, and it
        // is what does real work on the actual targets: it collapses the dispatcher
        // selector arithmetic (and any other provider use) into constants the
        // decompiler can simplify further, materially improving readability even when
        // a method's data-dependent control flow makes full linearization unsafe (the
        // common case — see WITHHELD). Folding does NOT count as DEFLATTENED; it only
        // increments FOLDED. The post-verify gate still guards the whole module.
        var providerNames = new HashSet<string>(providers.Select(p => p.FullName));
        foreach (var m in allMethods)
        {
            if (deflattened.Contains(m)) continue; // already folded + reported in prove loop
            int n;
            try { n = FoldProviderCalls(m, providers, providerNames); }
            catch { continue; /* per-method isolation; post-verify guards corruption */ }
            if (n > 0)
            {
                stats.Folded += n;
                // Fold-only method: distinct `folded` status so the artifact is honest
                // (the method was NOT linearized, only its provider constants folded).
                report.Add($"{m.FullName}\tfolded\t-\t-\tfolded={n}");
                withheldMatched.Remove(m); // folded — reported here, not as withheld
            }
        }

        // Methods that matched the flattening switch but were neither linearized nor
        // folded get an honest `withheld` row.
        foreach (var m in withheldMatched)
            report.Add($"{m.FullName}\twithheld\t-\t-\tnon-constant-state-or-irreducible");

        return stats;
    }

    // ======================================================================
    //  A2 — pure constant-provider identification + bounded IL interpreter
    // ======================================================================

    // IdentifyProviders returns the set of methods provably pure stateless int->int
    // constant providers. Purity can depend on other providers (a provider may call
    // another), so we iterate to a fixed point: a method is admitted once all the
    // providers it calls are already admitted.
    private static HashSet<MethodDefinition> IdentifyProviders(List<MethodDefinition> methods)
    {
        var candidates = methods.Where(IsProviderShape).ToList();
        var proven = new HashSet<MethodDefinition>();
        bool changed = true;
        while (changed)
        {
            changed = false;
            foreach (var m in candidates)
            {
                if (proven.Contains(m)) continue;
                if (CallsOnlyProviders(m, proven))
                {
                    proven.Add(m);
                    changed = true;
                }
            }
        }
        return proven;
    }

    // IsProviderShape: static, returns System.Int32, exactly one System.Int32
    // parameter, body has no field access / object creation / virtual dispatch /
    // throw. (Inter-provider `call` purity is enforced separately at fixed point.)
    private static bool IsProviderShape(MethodDefinition m)
    {
        var sig = m.Signature;
        if (sig == null || !m.IsStatic) return false;
        if (sig.ReturnType?.FullName != "System.Int32") return false;
        if (sig.ParameterTypes.Count != 1) return false;
        if (sig.ParameterTypes[0].FullName != "System.Int32") return false;
        if (m.CilMethodBody == null) return false;

        foreach (var ins in m.CilMethodBody.Instructions)
        {
            var op = ins.OpCode;
            if (op == CilOpCodes.Ldfld || op == CilOpCodes.Stfld ||
                op == CilOpCodes.Ldsfld || op == CilOpCodes.Stsfld ||
                op == CilOpCodes.Ldflda || op == CilOpCodes.Ldsflda ||
                op == CilOpCodes.Newobj || op == CilOpCodes.Newarr ||
                op == CilOpCodes.Callvirt || op == CilOpCodes.Throw ||
                op == CilOpCodes.Rethrow || op == CilOpCodes.Ldftn ||
                op == CilOpCodes.Ldvirtftn || op == CilOpCodes.Calli ||
                op == CilOpCodes.Box || op == CilOpCodes.Unbox)
                return false;
        }
        return true;
    }

    // CallsOnlyProviders: every `call` target in the body resolves to a method
    // already in `proven`. A call to anything else (or unresolvable) disqualifies.
    private static bool CallsOnlyProviders(MethodDefinition m, HashSet<MethodDefinition> proven)
    {
        foreach (var ins in m.CilMethodBody.Instructions)
        {
            if (ins.OpCode != CilOpCodes.Call) continue;
            if (ins.Operand is not IMethodDescriptor md) return false;
            MethodDefinition r;
            try { r = md.Resolve(); } catch { return false; }
            if (r == null || !proven.Contains(r)) return false;
        }
        return true;
    }

    // TryEval is a bounded stack-based IL interpreter over a provider's single int
    // argument. Returns false (conservative WITHHOLD signal) on any unsupported
    // opcode, stack underflow, or step-limit overrun. Supports the integer subset a
    // ConfuserEx constant provider uses: ldc/ldarg/dup/pop/arith/bitwise/shift/
    // switch/branches/ret. Nested provider calls fold via recursion (bounded depth).
    private const int EvalStepLimit = 10000;

    // Memoise (provider, arg) -> result. A provider is pure, so the evaluation is a
    // pure function; the same (provider, literal) triplet recurs thousands of times
    // across a real assembly's call sites, so caching turns a quadratic blow-up into
    // a near-linear pass. The boolean is the success flag; the int the result.
    private static readonly Dictionary<(MethodDefinition, int), (bool ok, int val)> EvalCache = new();

    private static bool TryEval(MethodDefinition p, int arg, out int result)
        => TryEval(p, arg, 0, out result);

    private static bool TryEval(MethodDefinition p, int arg, int depth, out int result)
    {
        result = 0;
        if (depth > 32 || p?.CilMethodBody == null) return false;

        var key = (p, arg);
        if (EvalCache.TryGetValue(key, out var cached)) { result = cached.val; return cached.ok; }

        bool ok = TryEvalUncached(p, arg, depth, out result);
        EvalCache[key] = (ok, result);
        return ok;
    }

    private static bool TryEvalUncached(MethodDefinition p, int arg, int depth, out int result)
    {
        result = 0;
        var ins = p.CilMethodBody.Instructions;
        ins.CalculateOffsets();

        var stack = new Stack<int>();
        // Local-variable file: real ConfuserEx providers spill the arg to a local
        // (`stloc V_0; ldloc V_0`) before the lookup switch. A local read before any
        // write is uninitialised -> bail (conservative).
        var locals = new Dictionary<int, int>();
        int ip = 0;            // index into ins
        int steps = 0;

        int IndexOfTarget(object operand)
        {
            // Branch/switch operands are ICilLabel after read-from-file.
            if (operand is ICilLabel lbl)
                return IndexByOffset(ins, lbl.Offset);
            if (operand is CilInstruction ci)
                return ins.IndexOf(ci);
            return -1;
        }

        int LocalSlot(CilInstruction i)
        {
            if (i.Operand is CilLocalVariable lv) return lv.Index;
            return -1;
        }

        while (ip >= 0 && ip < ins.Count)
        {
            if (++steps > EvalStepLimit) return false;
            var i = ins[ip];
            var op = i.OpCode;

            if (i.IsLdcI4()) { stack.Push(i.GetLdcI4Constant()); ip++; continue; }

            if (op == CilOpCodes.Ldarg_0 || op == CilOpCodes.Ldarg ||
                op == CilOpCodes.Ldarg_S)
            {
                // Single int parameter -> the only valid arg index is 0. ldarg.0 is
                // the parameter for a static method.
                int ai = op == CilOpCodes.Ldarg_0 ? 0 : ArgIndex(i);
                if (ai != 0) return false;
                stack.Push(arg); ip++; continue;
            }
            if (op == CilOpCodes.Ldarg_1 || op == CilOpCodes.Ldarg_2 || op == CilOpCodes.Ldarg_3)
                return false; // more than one logical arg -> not our shape

            if (op == CilOpCodes.Dup) { if (stack.Count < 1) return false; stack.Push(stack.Peek()); ip++; continue; }
            if (op == CilOpCodes.Pop) { if (stack.Count < 1) return false; stack.Pop(); ip++; continue; }
            if (op == CilOpCodes.Nop) { ip++; continue; }

            if (op == CilOpCodes.Neg) { if (stack.Count < 1) return false; stack.Push(-stack.Pop()); ip++; continue; }
            if (op == CilOpCodes.Not) { if (stack.Count < 1) return false; stack.Push(~stack.Pop()); ip++; continue; }

            // Local store/load (long and short forms). The short forms encode the slot
            // in the opcode; map them explicitly.
            if (op == CilOpCodes.Stloc_0 || op == CilOpCodes.Stloc_1 ||
                op == CilOpCodes.Stloc_2 || op == CilOpCodes.Stloc_3)
            {
                if (stack.Count < 1) return false;
                int slot = op == CilOpCodes.Stloc_0 ? 0 : op == CilOpCodes.Stloc_1 ? 1 : op == CilOpCodes.Stloc_2 ? 2 : 3;
                locals[slot] = stack.Pop(); ip++; continue;
            }
            if (op == CilOpCodes.Stloc || op == CilOpCodes.Stloc_S)
            {
                if (stack.Count < 1) return false;
                int slot = LocalSlot(i); if (slot < 0) return false;
                locals[slot] = stack.Pop(); ip++; continue;
            }
            if (op == CilOpCodes.Ldloc_0 || op == CilOpCodes.Ldloc_1 ||
                op == CilOpCodes.Ldloc_2 || op == CilOpCodes.Ldloc_3)
            {
                int slot = op == CilOpCodes.Ldloc_0 ? 0 : op == CilOpCodes.Ldloc_1 ? 1 : op == CilOpCodes.Ldloc_2 ? 2 : 3;
                if (!locals.TryGetValue(slot, out int lv)) return false; // uninitialised
                stack.Push(lv); ip++; continue;
            }
            if (op == CilOpCodes.Ldloc || op == CilOpCodes.Ldloc_S)
            {
                int slot = LocalSlot(i); if (slot < 0) return false;
                if (!locals.TryGetValue(slot, out int lv)) return false;
                stack.Push(lv); ip++; continue;
            }

            if (IsBinaryArith(op))
            {
                if (stack.Count < 2) return false;
                int b = stack.Pop(), a = stack.Pop();
                if (!ApplyBinary(op, a, b, out int r)) return false;
                stack.Push(r); ip++; continue;
            }

            if (op == CilOpCodes.Ret)
            {
                if (stack.Count < 1) return false;
                result = stack.Pop();
                return true;
            }

            if (op == CilOpCodes.Br || op == CilOpCodes.Br_S)
            {
                int t = IndexOfTarget(i.Operand);
                if (t < 0) return false;
                ip = t; continue;
            }
            if (op == CilOpCodes.Brtrue || op == CilOpCodes.Brtrue_S)
            {
                if (stack.Count < 1) return false;
                int v = stack.Pop();
                ip = v != 0 ? IndexOfTarget(i.Operand) : ip + 1;
                if (ip < 0) return false; continue;
            }
            if (op == CilOpCodes.Brfalse || op == CilOpCodes.Brfalse_S)
            {
                if (stack.Count < 1) return false;
                int v = stack.Pop();
                ip = v == 0 ? IndexOfTarget(i.Operand) : ip + 1;
                if (ip < 0) return false; continue;
            }
            if (TryEvalConditionalBranch(op, stack, out bool taken))
            {
                ip = taken ? IndexOfTarget(i.Operand) : ip + 1;
                if (ip < 0) return false; continue;
            }

            if (op == CilOpCodes.Switch)
            {
                if (stack.Count < 1) return false;
                int v = stack.Pop();
                if (i.Operand is not IList<ICilLabel> labels) return false;
                if (v < 0 || v >= labels.Count) { ip++; continue; } // out of range falls through
                int t = IndexByOffset(ins, labels[v].Offset);
                if (t < 0) return false;
                ip = t; continue;
            }

            if (op == CilOpCodes.Call)
            {
                // Nested provider call: pop one int arg, fold via recursion.
                if (i.Operand is not IMethodDescriptor md) return false;
                MethodDefinition callee;
                try { callee = md.Resolve(); } catch { return false; }
                if (callee == null || stack.Count < 1) return false;
                int a = stack.Pop();
                if (!TryEval(callee, a, depth + 1, out int cr)) return false;
                stack.Push(cr); ip++; continue;
            }

            if (op == CilOpCodes.Conv_I4 || op == CilOpCodes.Conv_I ||
                op == CilOpCodes.Conv_U4 || op == CilOpCodes.Conv_U)
            {
                ip++; continue; // int-domain conversions are identity here
            }

            return false; // any unsupported opcode -> bail conservatively
        }
        return false;
    }

    private static int ArgIndex(CilInstruction i)
    {
        if (i.Operand is Parameter p) return p.Index;
        if (i.Operand is int n) return n;
        if (i.Operand is short s) return s;
        return -1;
    }

    private static bool IsBinaryArith(CilOpCode op) =>
        op == CilOpCodes.Add || op == CilOpCodes.Add_Ovf || op == CilOpCodes.Add_Ovf_Un ||
        op == CilOpCodes.Sub || op == CilOpCodes.Sub_Ovf || op == CilOpCodes.Sub_Ovf_Un ||
        op == CilOpCodes.Mul || op == CilOpCodes.Mul_Ovf || op == CilOpCodes.Mul_Ovf_Un ||
        op == CilOpCodes.And || op == CilOpCodes.Or || op == CilOpCodes.Xor ||
        op == CilOpCodes.Shl || op == CilOpCodes.Shr || op == CilOpCodes.Shr_Un ||
        op == CilOpCodes.Div || op == CilOpCodes.Div_Un ||
        op == CilOpCodes.Rem || op == CilOpCodes.Rem_Un;

    private static bool ApplyBinary(CilOpCode op, int a, int b, out int r)
    {
        r = 0;
        unchecked
        {
            if (op == CilOpCodes.Add || op == CilOpCodes.Add_Ovf || op == CilOpCodes.Add_Ovf_Un) { r = a + b; return true; }
            if (op == CilOpCodes.Sub || op == CilOpCodes.Sub_Ovf || op == CilOpCodes.Sub_Ovf_Un) { r = a - b; return true; }
            if (op == CilOpCodes.Mul || op == CilOpCodes.Mul_Ovf || op == CilOpCodes.Mul_Ovf_Un) { r = a * b; return true; }
            if (op == CilOpCodes.And) { r = a & b; return true; }
            if (op == CilOpCodes.Or) { r = a | b; return true; }
            if (op == CilOpCodes.Xor) { r = a ^ b; return true; }
            if (op == CilOpCodes.Shl) { r = a << (b & 31); return true; }
            if (op == CilOpCodes.Shr) { r = a >> (b & 31); return true; }
            if (op == CilOpCodes.Shr_Un) { r = (int)((uint)a >> (b & 31)); return true; }
            if (op == CilOpCodes.Div) { if (b == 0) return false; r = a / b; return true; }
            if (op == CilOpCodes.Div_Un) { if (b == 0) return false; r = (int)((uint)a / (uint)b); return true; }
            if (op == CilOpCodes.Rem) { if (b == 0) return false; r = a % b; return true; }
            if (op == CilOpCodes.Rem_Un) { if (b == 0) return false; r = (int)((uint)a % (uint)b); return true; }
        }
        return false;
    }

    private static bool TryEvalConditionalBranch(CilOpCode op, Stack<int> stack, out bool taken)
    {
        taken = false;
        bool two = op == CilOpCodes.Beq || op == CilOpCodes.Beq_S ||
                   op == CilOpCodes.Bne_Un || op == CilOpCodes.Bne_Un_S ||
                   op == CilOpCodes.Blt || op == CilOpCodes.Blt_S ||
                   op == CilOpCodes.Blt_Un || op == CilOpCodes.Blt_Un_S ||
                   op == CilOpCodes.Bgt || op == CilOpCodes.Bgt_S ||
                   op == CilOpCodes.Bgt_Un || op == CilOpCodes.Bgt_Un_S ||
                   op == CilOpCodes.Ble || op == CilOpCodes.Ble_S ||
                   op == CilOpCodes.Ble_Un || op == CilOpCodes.Ble_Un_S ||
                   op == CilOpCodes.Bge || op == CilOpCodes.Bge_S ||
                   op == CilOpCodes.Bge_Un || op == CilOpCodes.Bge_Un_S;
        if (!two) return false;
        if (stack.Count < 2) { taken = false; return true; } // caller bails via index check
        int b = stack.Pop(), a = stack.Pop();
        if (op == CilOpCodes.Beq || op == CilOpCodes.Beq_S) taken = a == b;
        else if (op == CilOpCodes.Bne_Un || op == CilOpCodes.Bne_Un_S) taken = a != b;
        else if (op == CilOpCodes.Blt || op == CilOpCodes.Blt_S) taken = a < b;
        else if (op == CilOpCodes.Blt_Un || op == CilOpCodes.Blt_Un_S) taken = (uint)a < (uint)b;
        else if (op == CilOpCodes.Bgt || op == CilOpCodes.Bgt_S) taken = a > b;
        else if (op == CilOpCodes.Bgt_Un || op == CilOpCodes.Bgt_Un_S) taken = (uint)a > (uint)b;
        else if (op == CilOpCodes.Ble || op == CilOpCodes.Ble_S) taken = a <= b;
        else if (op == CilOpCodes.Ble_Un || op == CilOpCodes.Ble_Un_S) taken = (uint)a <= (uint)b;
        else if (op == CilOpCodes.Bge || op == CilOpCodes.Bge_S) taken = a >= b;
        else if (op == CilOpCodes.Bge_Un || op == CilOpCodes.Bge_Un_S) taken = (uint)a >= (uint)b;
        return true;
    }

    // Offset->index lookup. A naive linear scan is O(n) and the hot paths (CFG build,
    // Prove's chain walk, the interpreter's branch resolution) call it inside loops,
    // making the whole pass quadratic-or-worse on large methods. We memoise an
    // offset->index map per instruction-list identity; it is invalidated implicitly
    // because each method body is a distinct IList instance and CalculateOffsets is
    // called before lookups. The cache is keyed by reference + the list's Count so a
    // body whose instruction count changed (after a rewrite) rebuilds the map.
    private static readonly Dictionary<IList<CilInstruction>, (int count, Dictionary<int, int> map)> OffsetMaps = new();

    private static int IndexByOffset(IList<CilInstruction> ins, int offset)
    {
        if (!OffsetMaps.TryGetValue(ins, out var cached) || cached.count != ins.Count)
        {
            var map = new Dictionary<int, int>(ins.Count);
            for (int k = 0; k < ins.Count; k++) map[ins[k].Offset] = k;
            cached = (ins.Count, map);
            OffsetMaps[ins] = cached;
        }
        return cached.map.TryGetValue(offset, out int idx) ? idx : -1;
    }

    // Drop the cached offset map for a body after mutating its instructions, so the
    // next IndexByOffset rebuilds against the new offsets (count may be unchanged
    // while offsets shifted, which the count-keyed cache would otherwise miss).
    private static void InvalidateOffsets(IList<CilInstruction> ins) => OffsetMaps.Remove(ins);

    // ======================================================================
    //  A3 — CFG construction + dispatcher/state detection
    // ======================================================================

    private sealed class Block
    {
        public int Start;          // instruction index (inclusive)
        public int End;            // instruction index (exclusive)
        public readonly List<int> Succ = new();
    }

    private sealed class Cfg
    {
        public MethodDefinition Method;
        public IList<CilInstruction> Ins;
        public List<Block> Blocks = new();
        public Dictionary<int, int> BlockAtStart = new(); // start index -> block idx

        public static Cfg Build(MethodDefinition m)
        {
            var body = m.CilMethodBody;
            var ins = body.Instructions;
            ins.CalculateOffsets();

            var leaders = new SortedSet<int> { 0 };

            // Region boundaries from exception handlers — do not merge across them.
            foreach (var eh in body.ExceptionHandlers)
            {
                AddLeaderByLabel(ins, leaders, eh.TryStart);
                AddLeaderByLabel(ins, leaders, eh.TryEnd);
                AddLeaderByLabel(ins, leaders, eh.HandlerStart);
                AddLeaderByLabel(ins, leaders, eh.HandlerEnd);
                AddLeaderByLabel(ins, leaders, eh.FilterStart);
            }

            for (int i = 0; i < ins.Count; i++)
            {
                var op = ins[i].OpCode;
                var fc = op.FlowControl;
                if (fc == CilFlowControl.Branch || fc == CilFlowControl.ConditionalBranch)
                {
                    // target(s) are leaders; the fall-through (i+1) is a leader
                    foreach (int t in BranchTargets(ins, ins[i]))
                        if (t >= 0) leaders.Add(t);
                    if (i + 1 < ins.Count) leaders.Add(i + 1);
                }
                else if (fc == CilFlowControl.Return || fc == CilFlowControl.Throw)
                {
                    if (i + 1 < ins.Count) leaders.Add(i + 1);
                }
            }

            var cfg = new Cfg { Method = m, Ins = ins };
            var sorted = leaders.Where(l => l >= 0 && l < ins.Count).OrderBy(x => x).ToList();
            for (int li = 0; li < sorted.Count; li++)
            {
                int start = sorted[li];
                int end = li + 1 < sorted.Count ? sorted[li + 1] : ins.Count;
                var b = new Block { Start = start, End = end };
                cfg.BlockAtStart[start] = cfg.Blocks.Count;
                cfg.Blocks.Add(b);
            }

            // Wire successors.
            foreach (var b in cfg.Blocks)
            {
                if (b.End <= b.Start) continue;
                var last = ins[b.End - 1];
                var fc = last.OpCode.FlowControl;
                if (fc == CilFlowControl.Branch)
                {
                    foreach (int t in BranchTargets(ins, last))
                        if (cfg.BlockAtStart.TryGetValue(t, out int bi)) b.Succ.Add(bi);
                }
                else if (fc == CilFlowControl.ConditionalBranch)
                {
                    foreach (int t in BranchTargets(ins, last))
                        if (cfg.BlockAtStart.TryGetValue(t, out int bi)) b.Succ.Add(bi);
                    if (cfg.BlockAtStart.TryGetValue(b.End, out int fi)) b.Succ.Add(fi);
                }
                else if (fc == CilFlowControl.Return || fc == CilFlowControl.Throw)
                {
                    // no successors
                }
                else
                {
                    if (cfg.BlockAtStart.TryGetValue(b.End, out int fi)) b.Succ.Add(fi);
                }
            }
            return cfg;
        }

        private static void AddLeaderByLabel(IList<CilInstruction> ins, SortedSet<int> leaders, ICilLabel lbl)
        {
            if (lbl == null) return;
            int idx = IndexByOffset(ins, lbl.Offset);
            if (idx >= 0) leaders.Add(idx);
        }
    }

    private static IEnumerable<int> BranchTargets(IList<CilInstruction> ins, CilInstruction i)
    {
        if (i.Operand is ICilLabel lbl)
        {
            yield return IndexByOffset(ins, lbl.Offset);
        }
        else if (i.Operand is IList<ICilLabel> labels)
        {
            foreach (var l in labels) yield return IndexByOffset(ins, l.Offset);
        }
    }

    // TryFindFlatteningSwitch: locate a `switch` opcode whose selector is a local
    // assigned ONLY from constants or provider(literal) calls, optionally combined
    // with inline literal xor/add immediately before the switch. Returns the switch
    // instruction and the state local variable.
    private static bool TryFindFlatteningSwitch(MethodDefinition m, Cfg cfg,
        HashSet<MethodDefinition> providers, out CilInstruction switchInstr, out CilLocalVariable stateLocal)
    {
        switchInstr = null; stateLocal = null;
        var ins = cfg.Ins;

        // candidate switches
        for (int i = 0; i < ins.Count; i++)
        {
            if (ins[i].OpCode != CilOpCodes.Switch) continue;
            if (ins[i].Operand is not IList<ICilLabel> labels || labels.Count == 0) continue;

            // The selector local is the local loaded (possibly via xor/add with a
            // literal) just before the switch. Walk back over inline literal
            // xor/add/ldc to find a ldloc.
            var local = FindSelectorLocal(ins, i);
            if (local == null) continue;

            // Verify every definition of `local` in the method is a constant or
            // provider(literal). (If any def is runtime data, this is not a const
            // flattening switch — Prove would withhold anyway, but cheap to gate.)
            if (!AllDefsConstant(m, ins, local, providers)) continue;

            switchInstr = ins[i];
            stateLocal = local;
            return true;
        }
        return false;
    }

    // FindSelectorLocal walks backward from the switch over the small expression
    // that computes the selector (ldloc [xor/add literal]) and returns the local.
    private static CilLocalVariable FindSelectorLocal(IList<CilInstruction> ins, int switchIdx)
    {
        // Pattern variants:
        //   ldloc s; switch
        //   ldloc s; ldc.i4 key; xor; switch
        //   ldloc s; ldc.i4 key; add; switch
        int j = switchIdx - 1;
        // optional trailing arith with literal
        while (j >= 1 &&
               (ins[j].OpCode == CilOpCodes.Xor || ins[j].OpCode == CilOpCodes.Add ||
                ins[j].OpCode == CilOpCodes.Sub || ins[j].OpCode == CilOpCodes.And ||
                ins[j].OpCode == CilOpCodes.Or) &&
               ins[j - 1].IsLdcI4())
        {
            j -= 2; // skip the literal and the arith op
        }
        if (j < 0) return null;
        return GetLdlocLocal(ins[j]);
    }

    private static CilLocalVariable GetLdlocLocal(CilInstruction i)
    {
        var op = i.OpCode;
        if (op == CilOpCodes.Ldloc || op == CilOpCodes.Ldloc_S)
            return i.Operand as CilLocalVariable;
        // ldloc.0..3 encode the index in the opcode.
        return null;
    }

    private static CilLocalVariable GetStlocLocal(CilInstruction i)
    {
        var op = i.OpCode;
        if (op == CilOpCodes.Stloc || op == CilOpCodes.Stloc_S)
            return i.Operand as CilLocalVariable;
        return null;
    }

    // AllDefsConstant: every `stloc state` site stores either a ldc.i4 constant or
    // the result of provider(literal) [optionally xored/added with a literal]. Used
    // as a cheap gate before the full symbolic Prove.
    private static bool AllDefsConstant(MethodDefinition m, IList<CilInstruction> ins,
        CilLocalVariable state, HashSet<MethodDefinition> providers)
    {
        bool any = false;
        for (int i = 0; i < ins.Count; i++)
        {
            var def = GetStlocLocal(ins[i]);
            if (def == null || def != state) continue;
            any = true;
            if (!IsConstantStoreExpr(ins, i, providers)) return false;
        }
        return any;
    }

    // IsConstantStoreExpr checks the expression immediately feeding a `stloc state`
    // is compile-time constant: a ldc.i4, or provider(literal), each optionally
    // combined with inline literal xor/add/sub/and/or.
    private static bool IsConstantStoreExpr(IList<CilInstruction> ins, int stlocIdx,
        HashSet<MethodDefinition> providers)
    {
        int j = stlocIdx - 1;
        if (j < 0) return false;

        // optional: <expr> ldc.i4 key (xor|add|...)
        while (j >= 1 &&
               (ins[j].OpCode == CilOpCodes.Xor || ins[j].OpCode == CilOpCodes.Add ||
                ins[j].OpCode == CilOpCodes.Sub || ins[j].OpCode == CilOpCodes.And ||
                ins[j].OpCode == CilOpCodes.Or) &&
               ins[j - 1].IsLdcI4())
        {
            j -= 2;
        }
        if (j < 0) return false;

        // base expr: ldc.i4   OR   ldc.i4(literal) ; call provider
        if (ins[j].OpCode == CilOpCodes.Call &&
            ins[j].Operand is IMethodDescriptor md && j >= 1 && ins[j - 1].IsLdcI4())
        {
            MethodDefinition callee;
            try { callee = md.Resolve(); } catch { return false; }
            return callee != null && providers.Contains(callee);
        }
        return ins[j].IsLdcI4();
    }

    // ======================================================================
    //  A4 — Prove (symbolic resolution -> rewrite Plan)
    // ======================================================================

    private sealed class Plan
    {
        // Fold sites: a provider call to replace with ldc.i4 <const>. We record the
        // call instruction, its literal-arg-load instruction, and the folded value.
        public readonly List<(CilInstruction argLoad, CilInstruction call, int value)> FoldSites = new();
        // Edge redirects: dispatcher exit -> direct branch. Records (the instruction
        // to turn into `br`, the target instruction).
        public readonly List<(CilInstruction at, CilInstruction target)> Redirects = new();
        public int RemovedBlocks;
    }

    // Prove constant-propagates the state local from method entry through the
    // dispatcher. The flattened shape is a loop:
    //   entry: ... state = const0 ; br dispatch
    //   dispatch: ldloc state [^ key] ; switch -> caseBlocks ; br default
    //   caseBlock_k: <real work> ; state = const_k ; br dispatch   (or ret/leave)
    //
    // We start from the entry's constant state and repeatedly resolve switch->case,
    // recording for each visit the case-body entry and the case's terminating
    // back-branch (`br dispatch`). The chain is the proven straight-line order.
    // De-flattening then: redirect the entry's `br dispatch` to the first case body,
    // and each case's back-branch to the NEXT case body (or to the dispatcher's
    // `default` target when the chain leaves the loop). The dispatcher block itself
    // (ldloc/switch/br) becomes unreachable. A visited-set on the resolved state
    // value guarantees termination; every state value MUST be a compile-time
    // constant and every switch index in range, else null (withhold).
    private static Plan Prove(MethodDefinition m, Cfg cfg, HashSet<MethodDefinition> providers,
        CilInstruction switchInstr, CilLocalVariable state)
    {
        var ins = cfg.Ins;
        int switchIdx = ins.IndexOf(switchInstr);
        if (switchIdx < 0) return null;
        if (switchInstr.Operand is not IList<ICilLabel> labels) return null;

        // The dispatcher's "default" successor (taken when the switch index is out of
        // the case-table range) is the fall-through right after the switch — usually
        // a `br exit`. We need its concrete target instruction to terminate the chain.
        CilInstruction defaultTarget = ResolveDispatchDefault(ins, switchIdx);
        if (defaultTarget == null) return null;

        // Inline selector transform applied before the switch: state [op key].
        if (!ReadSelectorTransform(ins, switchIdx, out var selOp, out int selKey))
            return null;

        var plan = new Plan();

        // Resolve the entry state and the entry's branch INTO the dispatcher (the
        // instruction we will redirect to the first case body).
        if (!TryResolveInitialState(ins, switchIdx, state, providers, plan,
                out int curState, out CilInstruction entryBranch))
            return null;
        if (entryBranch == null) return null;

        int dispatchStart = StartOfDispatch(cfg, switchIdx);

        // Walk the chain. chain[i] = (caseStartInstr, backBranchInstr, nextState/exit).
        var chain = new List<(CilInstruction caseStart, CilInstruction backBranch, bool toExit)>();
        var visited = new HashSet<int>();

        // The chain visits each distinct state value at most once (visited guard), so
        // it is bounded by the number of switch cases; cap explicitly as a backstop.
        int guard = 0, guardMax = labels.Count + ins.Count + 16;
        while (true)
        {
            if (++guard > guardMax) return null;
            if (!visited.Add(curState))
                return null; // revisiting a state without leaving the loop = non-terminating

            int idx = ApplyTransform(selOp, curState, selKey);
            CilInstruction caseStartInstr;
            if (idx < 0 || idx >= labels.Count)
            {
                // index selects the default arm -> the chain leaves the loop here via
                // the dispatcher default. Treat as exit on the previous back-branch.
                if (chain.Count == 0) return null;
                var lastTmp = chain[chain.Count - 1];
                chain[chain.Count - 1] = (lastTmp.caseStart, lastTmp.backBranch, true);
                break;
            }
            int caseStartIdx = IndexByOffset(ins, labels[idx].Offset);
            if (caseStartIdx < 0) return null;
            caseStartInstr = ins[caseStartIdx];

            if (!WalkCase(ins, caseStartIdx, dispatchStart, state, providers, plan,
                    out int nextState, out bool hasNext, out CilInstruction backBranch))
                return null;

            chain.Add((caseStartInstr, backBranch, !hasNext));

            if (!hasNext) break; // real exit inside the case (ret/leave/throw)
            curState = nextState;
        }

        if (chain.Count == 0) return null;

        // Build redirects. Entry branch -> first case body.
        plan.Redirects.Add((entryBranch, chain[0].caseStart));
        // Each case back-branch -> next case body, or to the dispatcher default for
        // the terminal step. Cases that exit internally (ret/leave/throw) have no
        // back-branch to retarget.
        for (int i = 0; i < chain.Count; i++)
        {
            var (_, back, toExit) = chain[i];
            if (back == null) continue; // case exits internally; nothing to retarget
            CilInstruction target = toExit ? defaultTarget : chain[i + 1].caseStart;
            plan.Redirects.Add((back, target));
        }

        if (plan.Redirects.Count == 0) return null;
        plan.RemovedBlocks = chain.Count; // dispatcher round-trips collapsed
        return plan;
    }

    // ResolveDispatchDefault returns the concrete instruction the dispatcher falls
    // through to after the switch (the default/out-of-range arm). Typically a
    // `br exit` immediately after the switch; we follow one level of unconditional
    // branch so the redirect lands on the real exit block.
    private static CilInstruction ResolveDispatchDefault(IList<CilInstruction> ins, int switchIdx)
    {
        int after = switchIdx + 1;
        if (after >= ins.Count) return null;
        var nxt = ins[after];
        if (nxt.OpCode == CilOpCodes.Br || nxt.OpCode == CilOpCodes.Br_S)
        {
            foreach (int t in BranchTargets(ins, nxt))
                if (t >= 0) return ins[t];
            return null;
        }
        return nxt;
    }

    // ReadSelectorTransform reads the inline `state [op key]` transform applied to
    // the state local before the switch. Returns op=Nop-equivalent (none) when the
    // selector is the bare local.
    private static bool ReadSelectorTransform(IList<CilInstruction> ins, int switchIdx,
        out CilOpCode op, out int key)
    {
        op = CilOpCodes.Nop; key = 0;
        int j = switchIdx - 1;
        if (j < 1) { return ins[switchIdx - 1].OpCode == CilOpCodes.Ldloc ||
                            ins[switchIdx - 1].OpCode == CilOpCodes.Ldloc_S; }
        if ((ins[j].OpCode == CilOpCodes.Xor || ins[j].OpCode == CilOpCodes.Add ||
             ins[j].OpCode == CilOpCodes.Sub) && ins[j - 1].IsLdcI4())
        {
            op = ins[j].OpCode;
            key = ins[j - 1].GetLdcI4Constant();
        }
        return true;
    }

    private static int ApplyTransform(CilOpCode op, int state, int key)
    {
        unchecked
        {
            if (op == CilOpCodes.Xor) return state ^ key;
            if (op == CilOpCodes.Add) return state + key;
            if (op == CilOpCodes.Sub) return state - key;
            return state; // no transform
        }
    }

    private static int StartOfDispatch(Cfg cfg, int switchIdx)
    {
        // The dispatch block is the block containing the switch.
        foreach (var b in cfg.Blocks)
            if (switchIdx >= b.Start && switchIdx < b.End) return b.Start;
        return -1;
    }

    // TryResolveInitialState executes the method's entry straight-line region,
    // folding provider(literal) calls, until the first `stloc state`, capturing the
    // constant AND the branch that carries control into the dispatcher (the
    // instruction we redirect to the first proven case body). The entry branch is the
    // first unconditional `br <dispatch>` at or after the initial store; if control
    // falls straight through into the dispatcher with no branch, we withhold (we have
    // no single instruction to retarget without inserting one — conservative).
    private static bool TryResolveInitialState(IList<CilInstruction> ins, int switchIdx,
        CilLocalVariable state, HashSet<MethodDefinition> providers, Plan plan,
        out int value, out CilInstruction entryBranch)
    {
        value = 0; entryBranch = null;
        int storeIdx = -1;
        for (int i = 0; i < ins.Count; i++)
        {
            if (GetStlocLocal(ins[i]) == state) { storeIdx = i; break; }
        }
        if (storeIdx < 0) return false;
        if (!TryFoldStoreExpr(ins, storeIdx, providers, plan, out value)) return false;

        // From the store, find the unconditional branch that enters the dispatcher.
        for (int i = storeIdx + 1; i < ins.Count; i++)
        {
            var op = ins[i].OpCode;
            if (op == CilOpCodes.Br || op == CilOpCodes.Br_S)
            {
                entryBranch = ins[i];
                return true;
            }
            // A store/load/const on the way to the dispatcher is fine; anything that
            // is itself the switch means we fell through with no branch -> withhold.
            if (op == CilOpCodes.Switch) { entryBranch = null; return false; }
        }
        return false;
    }

    // WalkCase walks a case body from its start, recording provider fold sites and
    // the terminal `stloc state` (next constant). On reaching the state store it then
    // locates the case's back-branch (the unconditional `br <dispatch>` that returns
    // control to the dispatcher) and returns it for retargeting. A case that instead
    // reaches a real exit (ret/leave/throw) before any state store returns hasNext=
    // false and backBranch=null. Any conditional branch BEFORE the terminal store
    // means the next state isn't a single compile-time constant -> withhold.
    private static bool WalkCase(IList<CilInstruction> ins, int caseStart, int dispatchStart,
        CilLocalVariable state, HashSet<MethodDefinition> providers, Plan plan,
        out int nextState, out bool hasNext, out CilInstruction backBranch)
    {
        nextState = 0; hasNext = false; backBranch = null;

        int i = caseStart;
        var seenWalk = new HashSet<int>();
        while (i >= 0 && i < ins.Count)
        {
            // Visiting the same instruction twice means a branch cycle with no state
            // store on the path -> not a constant straight-line case -> withhold.
            if (!seenWalk.Add(i)) return false;
            var op = ins[i].OpCode;

            var def = GetStlocLocal(ins[i]);
            if (def == state)
            {
                if (!TryFoldStoreExpr(ins, i, providers, plan, out nextState)) return false;
                hasNext = true;
                // Locate the back-branch: the unconditional br after the store that
                // targets the dispatcher block. It is the edge we retarget. The walk
                // follows `br`-to-`br` chains; a `seen` set + step bound guarantees it
                // terminates even on a branch cycle that never reaches the dispatcher.
                var seenBack = new HashSet<int>();
                for (int k = i + 1; k < ins.Count;)
                {
                    if (!seenBack.Add(k)) return false; // branch cycle -> withhold
                    var kop = ins[k].OpCode;
                    if (kop == CilOpCodes.Br || kop == CilOpCodes.Br_S)
                    {
                        int t = -1;
                        foreach (int tt in BranchTargets(ins, ins[k])) { t = tt; break; }
                        if (t == dispatchStart) { backBranch = ins[k]; return true; }
                        if (t < 0) return false;
                        k = t; // follow the branch
                        continue;
                    }
                    if (kop == CilOpCodes.Switch && k == dispatchStart) { backBranch = null; return false; }
                    // a non-branch instruction between store and back-edge: step over.
                    k++;
                }
                return false; // no back-branch found -> withhold
            }

            var fc = op.FlowControl;
            if (fc == CilFlowControl.Return || fc == CilFlowControl.Throw) { hasNext = false; return true; }
            if (op == CilOpCodes.Leave || op == CilOpCodes.Leave_S) { hasNext = false; return true; }
            if (fc == CilFlowControl.Branch)
            {
                int t = -1;
                foreach (int tt in BranchTargets(ins, ins[i])) { t = tt; break; }
                if (t < 0) return false;
                if (t == dispatchStart) return false; // looped to dispatch w/o a state store -> withhold
                i = t;
                continue;
            }
            if (fc == CilFlowControl.ConditionalBranch) return false; // non-constant successor
            i++;
        }
        return false;
    }

    // TryFoldStoreExpr resolves the constant value stored by `stloc state` at index
    // stlocIdx, recording any provider(literal) fold site, and applying inline
    // literal xor/add/sub. Returns false if the expression is not constant.
    private static bool TryFoldStoreExpr(IList<CilInstruction> ins, int stlocIdx,
        HashSet<MethodDefinition> providers, Plan plan, out int value)
    {
        value = 0;
        int j = stlocIdx - 1;
        if (j < 0) return false;

        // optional inline trailing literal arith
        var post = new List<(CilOpCode op, int key)>();
        while (j >= 1 &&
               (ins[j].OpCode == CilOpCodes.Xor || ins[j].OpCode == CilOpCodes.Add ||
                ins[j].OpCode == CilOpCodes.Sub || ins[j].OpCode == CilOpCodes.And ||
                ins[j].OpCode == CilOpCodes.Or) &&
               ins[j - 1].IsLdcI4())
        {
            post.Insert(0, (ins[j].OpCode, ins[j - 1].GetLdcI4Constant()));
            j -= 2;
        }

        int baseVal;
        if (ins[j].OpCode == CilOpCodes.Call && ins[j].Operand is IMethodDescriptor md &&
            j >= 1 && ins[j - 1].IsLdcI4())
        {
            MethodDefinition callee;
            try { callee = md.Resolve(); } catch { return false; }
            if (callee == null || !providers.Contains(callee)) return false;
            int arg = ins[j - 1].GetLdcI4Constant();
            if (!TryEval(callee, arg, out baseVal)) return false;
            // record fold site: replace the call with ldc.i4 baseVal, nop the arg.
            plan.FoldSites.Add((ins[j - 1], ins[j], baseVal));
        }
        else if (ins[j].IsLdcI4())
        {
            baseVal = ins[j].GetLdcI4Constant();
        }
        else return false;

        unchecked
        {
            foreach (var (op, key) in post)
            {
                if (op == CilOpCodes.Xor) baseVal ^= key;
                else if (op == CilOpCodes.Add) baseVal += key;
                else if (op == CilOpCodes.Sub) baseVal -= key;
                else if (op == CilOpCodes.And) baseVal &= key;
                else if (op == CilOpCodes.Or) baseVal |= key;
            }
        }
        value = baseVal;
        return true;
    }

    // ======================================================================
    //  A5 — Apply (fold + rewrite) + post-verify
    // ======================================================================

    // Apply mutates IL using the cfxstrings nop+retarget technique (reuse the same
    // CilInstruction objects so branch targets / exception regions stay valid):
    //   - fold each provider triplet: nop the literal-arg load, retarget the `call`
    //     instruction to `ldc.i4 <const>`.
    //   - redirect proven edges: retarget the recorded instruction to `br <target>`.
    // We never physically remove instructions or reindex; dead plumbing is left as
    // nops (harmless) and counted in BLOCKSREMOVED logically.
    private static void Apply(MethodDefinition m, Plan plan)
    {
        foreach (var (argLoad, call, value) in plan.FoldSites)
        {
            // nop the literal arg load; turn the call into ldc.i4 <const>.
            argLoad.OpCode = CilOpCodes.Nop;
            argLoad.Operand = null;
            call.OpCode = CilOpCodes.Ldc_I4;
            call.Operand = value;
        }

        foreach (var (at, target) in plan.Redirects)
        {
            at.OpCode = CilOpCodes.Br;
            at.Operand = target.CreateLabel();
        }

        // Nop out instructions that are now unreachable from the method entry (the
        // dispatcher's ldloc/switch/default-br and any orphaned back-edges). This is
        // what actually de-flattens the DECOMPILED output: the surviving control flow
        // is the proven straight-line chain. We replace with Nop rather than delete so
        // instruction indices, branch targets, and exception-region labels all stay
        // valid (never reindex). Unreachable instructions that are themselves branch
        // targets of OTHER unreachable instructions are fine. We never nop anything
        // still reachable, and never touch exception-handler boundary instructions.
        NopUnreachable(m.CilMethodBody);

        // Recompute offsets so the body serialises cleanly. We deliberately do NOT
        // OptimizeMacros: it is unnecessary for correctness and would rewrite operand
        // encodings, complicating the static post-verify and any downstream tooling.
        m.CilMethodBody.Instructions.CalculateOffsets();
    }

    // FoldProviderCalls folds every `ldc.i4 <lit>; call <pure provider>` triplet in
    // the method to a single `ldc.i4 <const>`, returning the count folded. Safe
    // unconditionally: the provider is proven pure/side-effect-free and the argument
    // is a literal, so the call's value is a compile-time constant independent of any
    // surrounding control flow. Reuses the cfxstrings nop+retarget technique (nop the
    // arg load, retarget the call instruction) so branch targets / exception regions
    // stay valid. A `call` whose arg is not a literal, or whose provider can't be
    // evaluated, is left untouched.
    private static int FoldProviderCalls(MethodDefinition m, HashSet<MethodDefinition> providers,
        HashSet<string> providerNames)
    {
        var ins = m.CilMethodBody.Instructions;
        int folded = 0;
        for (int i = 1; i < ins.Count; i++)
        {
            if (ins[i].OpCode != CilOpCodes.Call) continue;
            if (ins[i].Operand is not IMethodDescriptor md) continue;
            if (!ins[i - 1].IsLdcI4()) continue;
            // Cheap name gate before the expensive Resolve(): only a handful of
            // provider methods exist, so reject the vast majority of calls by a string
            // compare and never resolve them.
            if (!providerNames.Contains(md.FullName)) continue;
            MethodDefinition callee;
            try { callee = md.Resolve(); } catch { continue; }
            if (callee == null || !providers.Contains(callee)) continue;
            int arg = ins[i - 1].GetLdcI4Constant();
            if (!TryEval(callee, arg, out int val)) continue;

            // nop the literal arg load; retarget the call to ldc.i4 <const>.
            ins[i - 1].OpCode = CilOpCodes.Nop;
            ins[i - 1].Operand = null;
            ins[i].OpCode = CilOpCodes.Ldc_I4;
            ins[i].Operand = val;
            folded++;
        }
        if (folded > 0) ins.CalculateOffsets();
        return folded;
    }

    // NopUnreachable replaces every instruction not reachable from the method entry
    // (or an exception-handler/filter region start, which the runtime enters
    // directly) with Nop. Reachability is a forward walk over fall-through +
    // branch/switch edges. Instructions that are exception-region BOUNDARY anchors
    // (try/handler/filter starts and ends) are preserved verbatim so AsmResolver can
    // still resolve those label offsets when serialising — nopping them is harmless
    // for an in-bounds offset but we keep them intact to be safe. We never remove or
    // reindex; dead instructions simply become nops.
    private static void NopUnreachable(CilMethodBody body)
    {
        var ins = body.Instructions;
        InvalidateOffsets(ins);
        ins.CalculateOffsets();
        int n = ins.Count;
        if (n == 0) return;

        var reachable = new bool[n];
        var stack = new Stack<int>();
        void Push(int idx) { if (idx >= 0 && idx < n && !reachable[idx]) { reachable[idx] = true; stack.Push(idx); } }

        Push(0);
        // Exception regions are entered by the runtime, not by explicit branches.
        var boundary = new HashSet<int>();
        foreach (var eh in body.ExceptionHandlers)
        {
            void Mark(ICilLabel l) { if (l != null) { int x = IndexByOffset(ins, l.Offset); if (x >= 0) { boundary.Add(x); } } }
            Mark(eh.TryStart); Mark(eh.TryEnd);
            Mark(eh.HandlerStart); Mark(eh.HandlerEnd);
            Mark(eh.FilterStart);
            int hs = eh.HandlerStart != null ? IndexByOffset(ins, eh.HandlerStart.Offset) : -1;
            int fs = eh.FilterStart != null ? IndexByOffset(ins, eh.FilterStart.Offset) : -1;
            Push(hs); Push(fs);
        }

        while (stack.Count > 0)
        {
            int i = stack.Pop();
            var op = ins[i].OpCode;
            var fc = op.FlowControl;

            if (fc == CilFlowControl.Branch)
            {
                foreach (int t in BranchTargets(ins, ins[i])) Push(t);
                continue; // unconditional: no fall-through
            }
            if (fc == CilFlowControl.ConditionalBranch)
            {
                foreach (int t in BranchTargets(ins, ins[i])) Push(t);
                Push(i + 1);
                continue;
            }
            if (fc == CilFlowControl.Return || fc == CilFlowControl.Throw)
                continue; // terminal
            // leave (used by exception regions) branches to its target.
            if (op == CilOpCodes.Leave || op == CilOpCodes.Leave_S)
            {
                foreach (int t in BranchTargets(ins, ins[i])) Push(t);
                continue;
            }
            // ordinary instruction: fall through
            Push(i + 1);
        }

        for (int i = 0; i < n; i++)
        {
            if (reachable[i] || boundary.Contains(i)) continue;
            ins[i].OpCode = CilOpCodes.Nop;
            ins[i].Operand = null;
        }
    }

    // Verify re-reads the OUTPUT statically and walks every method body's
    // instructions without exception. Static reparse + body walk — NEVER executes
    // target code.
    private static bool Verify(string outputPath)
    {
        try
        {
            var module = ModuleDefinition.FromFile(outputPath);
            foreach (var t in module.GetAllTypes())
            {
                foreach (var meth in t.Methods)
                {
                    var body = meth.CilMethodBody;
                    if (body == null) continue;
                    foreach (var _ in body.Instructions) { /* force materialisation */ }
                    body.Instructions.CalculateOffsets();
                }
            }
            return true;
        }
        catch (Exception e)
        {
            Console.Error.WriteLine("verify: " + e.Message);
            return false;
        }
    }

    // ======================================================================
    //  A6 — full --selftest (in-memory fixtures, machine-checkable)
    // ======================================================================

    // EvalNoArgMethod is a SELFTEST-ONLY faithful interpreter for the committed
    // no-arg fixture methods (two int locals: state, acc). It executes ldloc/stloc/
    // ldc/dup/pop/add/sub/and/or/xor, provider `call`, `switch`, br/brtrue/brfalse,
    // ret — enough to run BOTH the flattened and the de-flattened forms of the
    // fixture and compare outputs, proving Apply preserved semantics. It is NOT used
    // on target assemblies (the tool is static-only); it exists purely so the self-
    // test's "evaluated output equals reference" check is machine-checkable in-proc.
    // Returns int.MinValue on any unsupported construct (treated as a test failure).
    private static int EvalNoArgMethod(MethodDefinition m)
    {
        var body = m.CilMethodBody;
        if (body == null) return int.MinValue;
        var ins = body.Instructions;
        InvalidateOffsets(ins);
        ins.CalculateOffsets();
        var locals = new int[Math.Max(4, body.LocalVariables.Count)];
        var stack = new Stack<int>();
        int ip = 0, steps = 0;

        int Idx(object operand)
        {
            if (operand is ICilLabel lbl) return IndexByOffset(ins, lbl.Offset);
            return -1;
        }
        int LocalIndex(CilInstruction i)
        {
            if (i.Operand is CilLocalVariable lv) return lv.Index;
            return -1;
        }

        while (ip >= 0 && ip < ins.Count)
        {
            if (++steps > 100000) return int.MinValue;
            var i = ins[ip];
            var op = i.OpCode;

            if (op == CilOpCodes.Nop) { ip++; continue; }
            if (i.IsLdcI4()) { stack.Push(i.GetLdcI4Constant()); ip++; continue; }
            if (op == CilOpCodes.Stloc || op == CilOpCodes.Stloc_S)
            { int li = LocalIndex(i); if (li < 0 || stack.Count < 1) return int.MinValue; locals[li] = stack.Pop(); ip++; continue; }
            if (op == CilOpCodes.Ldloc || op == CilOpCodes.Ldloc_S)
            { int li = LocalIndex(i); if (li < 0) return int.MinValue; stack.Push(locals[li]); ip++; continue; }
            if (op == CilOpCodes.Dup) { if (stack.Count < 1) return int.MinValue; stack.Push(stack.Peek()); ip++; continue; }
            if (op == CilOpCodes.Pop) { if (stack.Count < 1) return int.MinValue; stack.Pop(); ip++; continue; }
            if (IsBinaryArith(op))
            { if (stack.Count < 2) return int.MinValue; int b = stack.Pop(), a = stack.Pop(); if (!ApplyBinary(op, a, b, out int r)) return int.MinValue; stack.Push(r); ip++; continue; }
            if (op == CilOpCodes.Call && i.Operand is IMethodDescriptor md)
            {
                MethodDefinition callee; try { callee = md.Resolve(); } catch { return int.MinValue; }
                if (callee == null || stack.Count < 1) return int.MinValue;
                int a = stack.Pop();
                if (!TryEval(callee, a, out int cr)) return int.MinValue;
                stack.Push(cr); ip++; continue;
            }
            if (op == CilOpCodes.Br || op == CilOpCodes.Br_S) { ip = Idx(i.Operand); if (ip < 0) return int.MinValue; continue; }
            if (op == CilOpCodes.Brtrue || op == CilOpCodes.Brtrue_S) { if (stack.Count < 1) return int.MinValue; ip = stack.Pop() != 0 ? Idx(i.Operand) : ip + 1; if (ip < 0) return int.MinValue; continue; }
            if (op == CilOpCodes.Brfalse || op == CilOpCodes.Brfalse_S) { if (stack.Count < 1) return int.MinValue; ip = stack.Pop() == 0 ? Idx(i.Operand) : ip + 1; if (ip < 0) return int.MinValue; continue; }
            if (op == CilOpCodes.Switch)
            {
                if (stack.Count < 1) return int.MinValue;
                int v = stack.Pop();
                if (i.Operand is not IList<ICilLabel> labels) return int.MinValue;
                if (v < 0 || v >= labels.Count) { ip++; continue; }
                ip = IndexByOffset(ins, labels[v].Offset); if (ip < 0) return int.MinValue; continue;
            }
            if (op == CilOpCodes.Ret) { return stack.Count >= 1 ? stack.Pop() : int.MinValue; }
            return int.MinValue;
        }
        return int.MinValue;
    }

    private static int SelfTest()
    {
        int rc = 0;
        try
        {
            rc |= SelfTestDeflatten() ? 0 : 1;
            rc |= SelfTestWithheld() ? 0 : 1;
            rc |= SelfTestVerify() ? 0 : 1;
        }
        catch (Exception e)
        {
            Console.Error.WriteLine("selftest exception: " + e);
            Console.WriteLine("SELFTEST-DEFLATTEN:FAIL");
            Console.WriteLine("SELFTEST-WITHHELD:FAIL");
            Console.WriteLine("SELFTEST-VERIFY:FAIL");
            return 1;
        }
        return rc;
    }

    // Build a fresh in-memory module with a pure provider + a flattened method whose
    // state derives only from constants/provider(literal). De-flatten it and assert
    // the fold count drops and the proven order holds.
    private static bool SelfTestDeflatten()
    {
        var module = NewModule("cfxcflow_selftest_deflatten");
        var providers = new HashSet<MethodDefinition>();
        var provider = BuildProvider(module);   // int P(int x) => switch lookup
        var flat = BuildFlattened(module, provider);

        var all = module.GetAllTypes().SelectMany(t => t.Methods).Where(x => x.CilMethodBody != null).ToList();
        var provs = IdentifyProviders(all);
        bool providerFound = provs.Contains(provider);

        // sanity: TryEval over the provider matches the lookup table.
        bool evalOk = TryEval(provider, 16, out int e16) && e16 == 2
                   && TryEval(provider, 17, out int e17) && e17 == 3
                   && TryEval(provider, 99, out int e99) && e99 == 0;

        // Reference output of the flattened method over sample inputs, computed by a
        // faithful interpreter BEFORE rewriting. The method takes no args; it returns
        // a fixed accumulator. We snapshot it, rewrite, then re-evaluate the rewritten
        // body and require equality — proving the de-flatten preserved semantics.
        int refOut = EvalNoArgMethod(flat);

        var cfg = Cfg.Build(flat);
        int gotoBefore = CountBranches(flat);
        bool detected = TryFindFlatteningSwitch(flat, cfg, provs, out var sw, out var st);
        Plan plan = detected ? Prove(flat, cfg, provs, sw, st) : null;
        bool proven = plan != null;
        int folded = plan?.FoldSites.Count ?? 0;
        int redirects = plan?.Redirects.Count ?? 0;

        if (proven) Apply(flat, plan);
        int gotoAfter = CountBranches(flat);
        int newOut = EvalNoArgMethod(flat);

        bool branchesDropped = gotoAfter < gotoBefore;
        bool semanticsKept = refOut == newOut && refOut != int.MinValue;
        bool pass = providerFound && evalOk && detected && proven && folded > 0
                    && branchesDropped && semanticsKept;
        Console.WriteLine($"DEFLATTEN providerFound={providerFound} eval={evalOk} detect={detected} prove={proven} folded={folded} redirects={redirects} branchesBefore={gotoBefore} branchesAfter={gotoAfter} refOut={refOut} newOut={newOut}");
        Console.WriteLine(pass ? "SELFTEST-DEFLATTEN:PASS" : "SELFTEST-DEFLATTEN:FAIL");
        return pass;
    }

    // The state of this fixture derives from a method ARGUMENT (runtime data) — Prove
    // must return null and the method must be left untouched.
    private static bool SelfTestWithheld()
    {
        var module = NewModule("cfxcflow_selftest_withheld");
        var provider = BuildProvider(module);
        var flat = BuildArgDependent(module, provider);

        var all = module.GetAllTypes().SelectMany(t => t.Methods).Where(x => x.CilMethodBody != null).ToList();
        var provs = IdentifyProviders(all);

        var cfg = Cfg.Build(flat);
        bool detected = TryFindFlatteningSwitch(flat, cfg, provs, out var sw, out var st);
        Plan plan = detected ? Prove(flat, cfg, provs, sw, st) : null;

        // Withheld == Prove returns null (we do NOT rewrite). Detection may or may
        // not fire; what matters is no Plan is produced.
        bool withheld = plan == null;
        Console.WriteLine($"WITHHELD detect={detected} prove={(plan != null)}");
        Console.WriteLine(withheld ? "SELFTEST-WITHHELD:PASS" : "SELFTEST-WITHHELD:FAIL");
        return withheld;
    }

    // After de-flattening fixture 1 and writing it, the output module must reparse.
    private static bool SelfTestVerify()
    {
        var module = NewModule("cfxcflow_selftest_verify");
        var provider = BuildProvider(module);
        var flat = BuildFlattened(module, provider);

        var all = module.GetAllTypes().SelectMany(t => t.Methods).Where(x => x.CilMethodBody != null).ToList();
        var provs = IdentifyProviders(all);
        var cfg = Cfg.Build(flat);
        if (TryFindFlatteningSwitch(flat, cfg, provs, out var sw, out var st))
        {
            var plan = Prove(flat, cfg, provs, sw, st);
            if (plan != null) Apply(flat, plan);
        }

        string tmp = Path.Combine(Path.GetTempPath(), "cfxcflow_selftest_" + Guid.NewGuid().ToString("N") + ".dll");
        bool ok;
        try
        {
            module.Write(tmp);
            ok = Verify(tmp);
        }
        catch (Exception e)
        {
            Console.Error.WriteLine("selftest-verify write: " + e.Message);
            ok = false;
        }
        finally
        {
            try { if (File.Exists(tmp)) File.Delete(tmp); } catch { }
        }
        Console.WriteLine(ok ? "SELFTEST-VERIFY:PASS" : "SELFTEST-VERIFY:FAIL");
        return ok;
    }

    // CountBranches counts REACHABLE branch/switch instructions — the same control-
    // flow edges a decompiler would render as `goto`. Counting only reachable edges
    // makes the before/after comparison meaningful: de-flattening turns the dispatcher
    // switch + back-edges into dead (unreachable) code, so the reachable count drops
    // even though we leave the dead instructions in place as nops.
    private static int CountBranches(MethodDefinition m)
    {
        var ins = m.CilMethodBody.Instructions;
        InvalidateOffsets(ins);
        ins.CalculateOffsets();
        int n = ins.Count;
        var reachable = new bool[n];
        var stack = new Stack<int>();
        void Push(int idx) { if (idx >= 0 && idx < n && !reachable[idx]) { reachable[idx] = true; stack.Push(idx); } }
        Push(0);
        foreach (var eh in m.CilMethodBody.ExceptionHandlers)
        {
            if (eh.HandlerStart != null) Push(IndexByOffset(ins, eh.HandlerStart.Offset));
            if (eh.FilterStart != null) Push(IndexByOffset(ins, eh.FilterStart.Offset));
        }
        while (stack.Count > 0)
        {
            int i = stack.Pop();
            var op = ins[i].OpCode; var fc = op.FlowControl;
            if (fc == CilFlowControl.Branch || op == CilOpCodes.Leave || op == CilOpCodes.Leave_S)
            { foreach (int t in BranchTargets(ins, ins[i])) Push(t); continue; }
            if (fc == CilFlowControl.ConditionalBranch)
            { foreach (int t in BranchTargets(ins, ins[i])) Push(t); Push(i + 1); continue; }
            if (fc == CilFlowControl.Return || fc == CilFlowControl.Throw) continue;
            Push(i + 1);
        }
        int count = 0;
        for (int i = 0; i < n; i++)
        {
            if (!reachable[i]) continue;
            var fc = ins[i].OpCode.FlowControl;
            if (fc == CilFlowControl.Branch || fc == CilFlowControl.ConditionalBranch) count++;
            if (ins[i].OpCode == CilOpCodes.Switch) count++;
        }
        return count;
    }

    // ---- in-memory fixture builders ----------------------------------------

    private static ModuleDefinition NewModule(string name)
    {
        var module = new ModuleDefinition(name + ".dll", KnownCorLibs.SystemRuntime_v8_0_0_0);
        return module;
    }

    private static TypeDefinition HostType(ModuleDefinition module, string typeName)
    {
        var t = new TypeDefinition(null, typeName,
            TypeAttributes.Public | TypeAttributes.Class | TypeAttributes.Sealed,
            module.CorLibTypeFactory.Object.ToTypeDefOrRef());
        module.TopLevelTypes.Add(t);
        return t;
    }

    // int P(int x) => x switch { 16=>2, 17=>3, 18=>7, _=>0 };  (pure provider)
    private static MethodDefinition BuildProvider(ModuleDefinition module)
    {
        var t = HostType(module, "Provider");
        var sig = MethodSignature.CreateStatic(
            module.CorLibTypeFactory.Int32, module.CorLibTypeFactory.Int32);
        var m = new MethodDefinition("P", MethodAttributes.Public | MethodAttributes.Static, sig);
        t.Methods.Add(m);
        m.CilMethodBody = new CilMethodBody(m);
        var il = m.CilMethodBody.Instructions;

        // Simple if-chain (equivalent to the switch lookup, evaluator-friendly):
        //   if (x==16) return 2; if (x==17) return 3; if (x==18) return 7; return 0;
        void Case(int key, int val)
        {
            var skip = new CilInstructionLabel();
            il.Add(CilOpCodes.Ldarg_0);
            il.Add(CilOpCodes.Ldc_I4, key);
            il.Add(CilOpCodes.Bne_Un, skip);
            il.Add(CilOpCodes.Ldc_I4, val);
            il.Add(CilOpCodes.Ret);
            skip.Instruction = il.Add(CilOpCodes.Nop);
        }
        Case(16, 2);
        Case(17, 3);
        Case(18, 7);
        il.Add(CilOpCodes.Ldc_I4_0);
        il.Add(CilOpCodes.Ret);
        return m;
    }

    // A flattened method: state machine driven purely by provider(literal) constants.
    //   s = P(16);                 // -> 2
    //   loop: switch (s) { 2: blockA; 3: blockB; 7: blockC; default: exit }
    //   blockA: acc += 10; s = P(17); goto loop;   // -> 3
    //   blockB: acc += 20; s = P(18); goto loop;   // -> 7
    //   blockC: acc += 40; s = 0;     goto loop;   // -> 0 -> default -> exit
    //   exit: return acc;
    // Selector is `s` directly (no xor) for clarity; switch indices are the state
    // values themselves (we size the switch table to cover them).
    private static MethodDefinition BuildFlattened(ModuleDefinition module, MethodDefinition provider)
    {
        var t = HostType(module, "Flat");
        var sig = MethodSignature.CreateStatic(module.CorLibTypeFactory.Int32);
        var m = new MethodDefinition("Run", MethodAttributes.Public | MethodAttributes.Static, sig);
        t.Methods.Add(m);
        var body = new CilMethodBody(m);
        m.CilMethodBody = body;

        var i32 = module.CorLibTypeFactory.Int32;
        var state = new CilLocalVariable(i32);
        var acc = new CilLocalVariable(i32);
        body.LocalVariables.Add(state);
        body.LocalVariables.Add(acc);

        var il = body.Instructions;

        var loop = new CilInstructionLabel();
        var blockA = new CilInstructionLabel();
        var blockB = new CilInstructionLabel();
        var blockC = new CilInstructionLabel();
        var exit = new CilInstructionLabel();

        // acc = 0
        il.Add(CilOpCodes.Ldc_I4_0);
        il.Add(CilOpCodes.Stloc, acc);
        // s = P(16)  -> 2
        il.Add(CilOpCodes.Ldc_I4, 16);
        il.Add(CilOpCodes.Call, provider);
        il.Add(CilOpCodes.Stloc, state);
        il.Add(CilOpCodes.Br, loop); // explicit entry edge into the dispatcher

        // loop: switch(s) over 0..7 ; cases 2->A,3->B,7->C else exit
        loop.Instruction = il.Add(CilOpCodes.Ldloc, state);
        var labels = new List<ICilLabel> { exit, exit, blockA, blockB, exit, exit, exit, blockC };
        il.Add(CilOpCodes.Switch, labels);
        il.Add(CilOpCodes.Br, exit); // default fall-through

        // blockA: acc += 10; s = P(17) -> 3; goto loop
        blockA.Instruction = il.Add(CilOpCodes.Ldloc, acc);
        il.Add(CilOpCodes.Ldc_I4, 10);
        il.Add(CilOpCodes.Add);
        il.Add(CilOpCodes.Stloc, acc);
        il.Add(CilOpCodes.Ldc_I4, 17);
        il.Add(CilOpCodes.Call, provider);
        il.Add(CilOpCodes.Stloc, state);
        il.Add(CilOpCodes.Br, loop);

        // blockB: acc += 20; s = P(18) -> 7; goto loop
        blockB.Instruction = il.Add(CilOpCodes.Ldloc, acc);
        il.Add(CilOpCodes.Ldc_I4, 20);
        il.Add(CilOpCodes.Add);
        il.Add(CilOpCodes.Stloc, acc);
        il.Add(CilOpCodes.Ldc_I4, 18);
        il.Add(CilOpCodes.Call, provider);
        il.Add(CilOpCodes.Stloc, state);
        il.Add(CilOpCodes.Br, loop);

        // blockC: acc += 40; s = 0; goto loop -> default -> exit
        blockC.Instruction = il.Add(CilOpCodes.Ldloc, acc);
        il.Add(CilOpCodes.Ldc_I4, 40);
        il.Add(CilOpCodes.Add);
        il.Add(CilOpCodes.Stloc, acc);
        il.Add(CilOpCodes.Ldc_I4_0);
        il.Add(CilOpCodes.Stloc, state);
        il.Add(CilOpCodes.Br, loop);

        // exit: return acc
        exit.Instruction = il.Add(CilOpCodes.Ldloc, acc);
        il.Add(CilOpCodes.Ret);

        return m;
    }

    // Arg-dependent flattened shape: the FIRST state store reads the method argument
    // (runtime data) rather than a constant -> Prove must withhold.
    private static MethodDefinition BuildArgDependent(ModuleDefinition module, MethodDefinition provider)
    {
        var t = HostType(module, "FlatArg");
        var i32 = module.CorLibTypeFactory.Int32;
        var sig = MethodSignature.CreateStatic(i32, i32);
        var m = new MethodDefinition("RunArg", MethodAttributes.Public | MethodAttributes.Static, sig);
        t.Methods.Add(m);
        var body = new CilMethodBody(m);
        m.CilMethodBody = body;

        var state = new CilLocalVariable(i32);
        var acc = new CilLocalVariable(i32);
        body.LocalVariables.Add(state);
        body.LocalVariables.Add(acc);
        var il = body.Instructions;

        var loop = new CilInstructionLabel();
        var blockA = new CilInstructionLabel();
        var exit = new CilInstructionLabel();

        il.Add(CilOpCodes.Ldc_I4_0);
        il.Add(CilOpCodes.Stloc, acc);
        // s = arg0  (RUNTIME data — not constant)
        il.Add(CilOpCodes.Ldarg_0);
        il.Add(CilOpCodes.Stloc, state);
        il.Add(CilOpCodes.Br, loop);

        loop.Instruction = il.Add(CilOpCodes.Ldloc, state);
        var labels = new List<ICilLabel> { exit, exit, blockA };
        il.Add(CilOpCodes.Switch, labels);
        il.Add(CilOpCodes.Br, exit);

        blockA.Instruction = il.Add(CilOpCodes.Ldloc, acc);
        il.Add(CilOpCodes.Ldc_I4, 10);
        il.Add(CilOpCodes.Add);
        il.Add(CilOpCodes.Stloc, acc);
        il.Add(CilOpCodes.Ldc_I4_0);
        il.Add(CilOpCodes.Stloc, state);
        il.Add(CilOpCodes.Br, loop);

        exit.Instruction = il.Add(CilOpCodes.Ldloc, acc);
        il.Add(CilOpCodes.Ret);
        return m;
    }
}
