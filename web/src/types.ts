export interface Job {
    id: string;
    type: 'extract' | 'optimize' | 'test';
    sourcePath: string;
    destinationPath: string;
    status: 'pending' | 'processing' | 'completed' | 'failed' | 'cancelled';
    progress: number;
    eta: string;
    fps: number;
    priority: number;
    createdAt: string;
    startedAt?: string;
    completedAt?: string;
    error?: string;
}

export interface SystemConfig {
    sourceDir: string;
    destDir: string;
    gpuVendor: string;
    qualityPreset: string;
    crf: number;
    aiProvider: string;
}
