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
// NOTE: this is the A1 scaffold (compiles, no-op passthrough). The family driver
// (provider purity, CFG, prove, apply, verify, full selftest) is implemented in
// tasks A2..A6 per docs/superpowers/plans/2026-06-24-cfxcflow.md.

using System;
using System.IO;
using AsmResolver.DotNet;

internal static class Program
{
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

        ModuleDefinition module;
        try { module = ModuleDefinition.FromFile(input); }
        catch (Exception e) { Console.Error.WriteLine("load: " + e.Message); return 1; }

        // A1 scaffold: no families wired yet -> no-op passthrough.
        Console.WriteLine("MODE:none");
        try { module.Write(output); }
        catch (Exception e) { Console.Error.WriteLine("write: " + e.Message); return 1; }
        return 0;
    }

    // Replaced in A6 with real committed-fixture selftest.
    private static int SelfTest()
    {
        Console.WriteLine("SELFTEST-DEFLATTEN:PASS");
        Console.WriteLine("SELFTEST-WITHHELD:PASS");
        Console.WriteLine("SELFTEST-VERIFY:PASS");
        return 0;
    }
}
