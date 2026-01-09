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
    gpuVendor: string;
    qualityPreset: string;
    crf: number;
    sourceDir: string;
    destDir: string;
    aiProvider: string;
    aiApiKey?: string;
    aiEndpoint?: string;
    aiModel?: string;
    licenseKey?: string;
    isPremium?: boolean;
    planName?: string;
}
export interface WatchDirectory {
    path: string;
    recursive: boolean;
    includePatterns: string[];
    excludePatterns: string[];
    minFileSizeMB: number;
    minFileAgeMinutes: number;
}

export interface ScannerConfig {
    mode: 'manual' | 'startup' | 'periodic' | 'watch' | 'hybrid';
    enabled: boolean;
    watchDirectories: WatchDirectory[];
    scanIntervalSec: number;
    autoCreateJobs: boolean;
    processedFilePath: string;
    defaultPriority: number;
    outputDirectory: string;
    extractExtensions: string[];
    optimizeExtensions: string[];
}
export interface SystemStats {
    cpuUsage: number;
    memoryUsage: number;
    diskUsage: number;
    gpuUsage: number;
    gpuTemp: number;
    diskFreeGB: number;
}
