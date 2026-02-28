export interface AILog {
    timestamp: string;
    operation: 'metadata_cleaning' | 'encoding_analysis' | 'subtitle_download' | 'verification' | 'transcoding_start' | 'transcoding_complete' | 'extraction_start' | 'extraction_complete' | 'file_deleted';
    provider: string;
    detail: string;
    durationMs: number;
    success: boolean;
    error?: string;
}

export interface Job {
    id: string;
    type: 'extract' | 'optimize' | 'test';
    sourcePath: string;
    destinationPath: string;
    status: 'pending' | 'processing' | 'completed' | 'failed' | 'cancelled';
    statusDetail?: string;
    progress: number;
    eta: string;
    fps: number;
    priority: number;
    createdAt: string;
    startedAt?: string;
    completedAt?: string;
    error?: string;
    createSubtitles?: boolean;
    upscale?: boolean;
    resolution?: string;
    inputSize?: number;
    outputSize?: number;
    aiCleaned?: boolean;
    aiSubtitles?: boolean;
    verifyOutput?: boolean;
    verified?: boolean;
    deleteSource?: boolean;
    maxRetries?: number;
    retryCount?: number;
    aiLogs?: AILog[];
}

export interface ProcessingSchedule {
    enabled: boolean;
    startHour: number;
    endHour: number;
    allowedDays: number[];  // 0=Sun…6=Sat
    timezone: string;
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
    isInitialized?: boolean;
    planName?: string;
    verifyOutput?: boolean;
    deleteSource?: boolean;
    subtitleMode?: 'always' | 'selective' | 'never';
    subtitleLang?: string;
    subtitleApiKey?: string;
    subtitleUsername?: string;
    subtitlePassword?: string;    // write-only — never returned by GET /api/config
    subtitlePasswordSet?: boolean; // true when a password has been saved (replaces the value for display)
    schedule?: ProcessingSchedule;
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
    autoCreateSubtitles: boolean;
    autoUpscale: boolean;
    autoResolution: string;
    processedFilePath: string;
    defaultPriority: number;
    outputDirectory: string;
    extractExtensions: string[];
    optimizeExtensions: string[];
    skipHighResolution?: boolean;
    resolutionHeightThreshold?: number;
}
export interface SystemStats {
    cpuUsage: number;
    memoryUsage: number;
    diskUsage: number;
    gpuUsage: number;
    gpuTemp: number;
    diskFreeGB: number;
}

export interface DashboardStats {
    totalStorageSaved: number;
    totalAIJobs: number;
    totalSubtitlesCreated: number;
    totalUpscales: number;
    totalCleaned: number;
    efficiencyScore: number;
}

export interface ProcessedFile {
    path: string;
    hash: string;
    processedAt: string;
    jobId: string;
    jobType: string;
    inputSize: number;
    outputSize: number;
    aiSubtitles: boolean;
    aiUpscale: boolean;
    aiCleaned: boolean;
}
