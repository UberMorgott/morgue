import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import { pipelineState, resetPipeline, updateFromEvent } from './pipeline';

describe('IL2CPP modern pipeline events', () => {
  beforeEach(() => resetPipeline());

  it('routes InspectorRedux dump progress to the execute phase + tool counter', () => {
    updateFromEvent({ data: [{ Phase: 'execute', Target: 'GameAssembly.dll', Tool: 'il2cppinspector',
      Progress: { Step: 1, Total: 7, Name: 'Extract metadata', Tool: 'il2cppinspector', Status: 'Success', Count: 193, Unit: 'assemblies' } }] });
    const s = get(pipelineState);
    expect(s.phase).toBe('execute');
    expect(s.execCounters['il2cppinspector']).toBeTruthy();
    expect(s.execCounters['il2cppinspector'].unit).toBe('assemblies');
  });

  it('routes AssetRipper + Odin stages through tool counters', () => {
    updateFromEvent({ data: [{ Phase: 'execute', Target: 'GameAssembly.dll', Tool: 'assetripper',
      Progress: { Step: 3, Total: 7, Name: 'Extract data layer', Tool: 'assetripper', Status: 'Success', Count: 42, Unit: 'assets' } }] });
    updateFromEvent({ data: [{ Phase: 'execute', Target: 'GameAssembly.dll', Tool: 'odin',
      Progress: { Step: 4, Total: 7, Name: 'Decode Odin config', Tool: 'odin', Status: 'Success', Count: 9, Unit: 'configs' } }] });
    const s = get(pipelineState);
    expect(s.execCounters['assetripper'].count).toBe(42);
    expect(s.execCounters['odin'].count).toBe(9);
    expect(s.toolStatus['odin']).toBe('success');
  });

  it('surfaces the ASP.NET runtime download in the tools phase', () => {
    updateFromEvent({ data: [{ Phase: 'download', Target: 'GameAssembly.dll', Message: 'Downloading dotnet-aspnet10... 47%' }] });
    const s = get(pipelineState);
    expect(s.phase).toBe('tools');
    expect(s.downloadingTool).toBe('dotnet-aspnet10');
    expect(s.downloadProgress).toBe(47);
  });
});
