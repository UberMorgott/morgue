import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import { pipelineState, resetPipeline, startPipeline, updateFromEvent, eventErrorText } from './pipeline';

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

describe('terminal outcomes and error text', () => {
  beforeEach(() => { resetPipeline(); startPipeline('C:/game'); });

  it('keeps the run going when one target fails', () => {
    updateFromEvent({ data: [{ Phase: 'execute', Target: 'a.dll', Message: 'working' }] });
    updateFromEvent({ data: [{ Phase: 'target', Target: 'a.dll', TargetOutcome: 'error', error: 'ilspy crashed' }] });
    const s = get(pipelineState);
    expect(s.phase).toBe('execute');
    expect(s.error).toBe('');
    expect(s.targets.find(t => t.path === 'a.dll')?.error).toBe('ilspy crashed');
  });

  it('maps RunOutcome on the Done event', () => {
    updateFromEvent({ data: [{ Phase: 'error', Done: true, RunOutcome: 'error', error: 'boom' }] });
    expect(get(pipelineState).phase).toBe('error');
    expect(get(pipelineState).error).toBe('boom');
    startPipeline('C:/game');
    updateFromEvent({ data: [{ Phase: 'cancelled', Done: true, RunOutcome: 'cancelled' }] });
    expect(get(pipelineState).phase).toBe('cancelled');
  });

  it('never shows {} for an opaque error object', () => {
    expect(eventErrorText({ Error: {} })).toBe('');
    expect(eventErrorText({ Error: { message: 'm' } })).toBe('m');
    expect(eventErrorText({ error: 'text' })).toBe('text');
    updateFromEvent({ data: [{ Phase: 'done', Done: true, Error: {} }] });
    expect(get(pipelineState).phase).toBe('done');
  });
});