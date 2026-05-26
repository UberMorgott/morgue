// Wails RPC stubs — will be replaced by generated bindings after `wails3 generate bindings`

export const ReconService = {
  ScanDirectory: async (_dir: string): Promise<any[]> => [],
  ClassifyFile: async (_path: string): Promise<any> => ({}),
};

export const PipelineService = {
  Run: async (_input: string, _output: string): Promise<void> => {},
  Stop: async (): Promise<void> => {},
  GetStatus: async (): Promise<{ running: boolean; phase: string; target: string }> =>
    ({ running: false, phase: '', target: '' }),
};

export const ToolsService = {
  CheckAll: async (): Promise<any[]> => [],
  Install: async (_name: string): Promise<void> => {},
  InstallAll: async (): Promise<void> => {},
};

export const ConfigService = {
  Get: async (): Promise<any> => ({
    skip_system_libs: true,
    skip_categories: {},
    default_output_dir: './decompiled',
    step_timeout_minutes: 60,
    concurrent_targets: 1,
    log_level: 'info',
  }),
  Save: async (_cfg: any): Promise<void> => {},
  GetSkipCategories: async (): Promise<Record<string, boolean>> => ({}),
};

export const UpdateService = {
  GetVersion: async (): Promise<string> => 'dev',
  Check: async (): Promise<{ available: boolean; version: string; status: string }> =>
    ({ available: false, version: '', status: 'up to date' }),
  Apply: async (): Promise<void> => {},
};
