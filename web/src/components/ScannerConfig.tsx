import { useState, useEffect } from 'react';
import FileBrowserModal from './FileBrowserModal';
import type { ScannerConfig, WatchDirectory } from '../types';

interface ScannerConfigProps {
    config: ScannerConfig;
    onSave: (config: ScannerConfig) => Promise<boolean>;
}

export default function ScannerConfigComponent({ config: initialConfig, onSave }: ScannerConfigProps) {
    // Ensure watchDirectories is always an array to prevent crashes
    const ensureConfig = (cfg: ScannerConfig) => ({
        ...cfg,
        watchDirectories: cfg.watchDirectories || []
    });

    const [config, setConfig] = useState<ScannerConfig>(ensureConfig(initialConfig));
    const [isSaving, setIsSaving] = useState(false);
    const [newDir, setNewDir] = useState('');
    const [showFileBrowser, setShowFileBrowser] = useState(false);

    // Update local state if initialConfig changes
    useEffect(() => {
        setConfig(ensureConfig(initialConfig));
    }, [initialConfig]);

    const handleSave = async () => {
        setIsSaving(true);
        await onSave(config);
        setIsSaving(false);
    };

    const addWatchDir = () => {
        if (!newDir) return;

        const newWatchDir: WatchDirectory = {
            path: newDir,
            recursive: true,
            includePatterns: ['*'],
            excludePatterns: [],
            minFileSizeMB: 0,
            minFileAgeMinutes: 0
        };

        setConfig({
            ...config,
            watchDirectories: [...config.watchDirectories, newWatchDir]
        });
        setNewDir('');
    };

    const removeWatchDir = (index: number) => {
        const newDirs = [...config.watchDirectories];
        newDirs.splice(index, 1);
        setConfig({ ...config, watchDirectories: newDirs });
    };

    return (
        <div className="scanner-config-view">
            <div className="view-header">
                <h1>Scanner Configuration</h1>
                <p className="text-secondary">Configure automatic file discovery and processing</p>
            </div>

            <div className="grid grid-2">
                {/* General Settings */}
                <div className="card">
                    <div className="card-header">
                        <h3 className="card-title">General Settings</h3>
                        <label className="flex items-center gap-2 cursor-pointer">
                            <input
                                type="checkbox"
                                checked={config.enabled}
                                onChange={e => setConfig({ ...config, enabled: e.target.checked })}
                                className="w-4 h-4"
                            />
                            <span className={config.enabled ? 'text-primary font-medium' : 'text-secondary'}>
                                {config.enabled ? 'Enabled' : 'Disabled'}
                            </span>
                        </label>
                    </div>
                    <div className={`card-body ${!config.enabled ? 'opacity-50 pointer-events-none' : ''}`}>
                        <div className="form-group mb-4">
                            <label className="label mb-2 block">Scan Mode</label>
                            <select
                                className="input select"
                                value={config.mode}
                                onChange={e => setConfig({ ...config, mode: e.target.value as ScannerConfig['mode'] })}
                            >
                                <option value="manual">Manual (Trigger API only)</option>
                                <option value="startup">Startup Only</option>
                                <option value="periodic">Periodic (Interval)</option>
                                <option value="watch">Watch (Real-time)</option>
                                <option value="hybrid">Hybrid (Watch + Periodic)</option>
                            </select>
                        </div>

                        {(config.mode === 'periodic' || config.mode === 'hybrid') && (
                            <div className="form-group mb-4">
                                <label className="label mb-2 block">Scan Interval (seconds)</label>
                                <input
                                    type="number"
                                    className="input"
                                    value={config.scanIntervalSec}
                                    onChange={e => setConfig({ ...config, scanIntervalSec: parseInt(e.target.value) })}
                                    min="60"
                                />
                            </div>
                        )}

                        <div className="form-group mb-4">
                            <label className="label mb-2 block">Default Priority</label>
                            <input
                                type="number"
                                className="input"
                                value={config.defaultPriority}
                                onChange={e => setConfig({ ...config, defaultPriority: parseInt(e.target.value) })}
                                min="1"
                                max="10"
                            />
                        </div>

                        <div className="form-group mb-4">
                            <label className="label mb-2 block">Auto-Create Jobs</label>
                            <label className="flex items-center gap-2 cursor-pointer">
                                <input
                                    type="checkbox"
                                    checked={config.autoCreateJobs}
                                    onChange={e => setConfig({ ...config, autoCreateJobs: e.target.checked })}
                                    className="w-4 h-4"
                                />
                                <span className="text-secondary text-sm">Automatically queue jobs when files are found</span>
                            </label>
                        </div>

                        <div className="form-group">
                            <label className="label mb-2 block flex items-center">
                                AI Subtitles (Whisper)
                                <span className="pro-tag ml-2">PRO</span>
                            </label>
                            <label className="flex items-center gap-2 cursor-pointer">
                                <input
                                    type="checkbox"
                                    checked={config.autoCreateSubtitles}
                                    onChange={e => setConfig({ ...config, autoCreateSubtitles: e.target.checked })}
                                    className="w-4 h-4"
                                />
                                <span className="text-secondary text-sm">Generate AI subtitles using Whisper for new jobs</span>
                            </label>
                        </div>

                        <div className="form-group">
                            <label className="label mb-2 block flex items-center">
                                AI Upscaling (Super Resolution)
                                <span className="pro-tag ml-2">PRO</span>
                            </label>
                            <div className="flex flex-col gap-2">
                                <label className="flex items-center gap-2 cursor-pointer">
                                    <input
                                        type="checkbox"
                                        checked={config.autoUpscale}
                                        onChange={e => setConfig({ ...config, autoUpscale: e.target.checked })}
                                        className="w-4 h-4"
                                    />
                                    <span className="text-secondary text-sm">Automatically upscale low-res content</span>
                                </label>

                                {config.autoUpscale && (
                                    <select
                                        className="input select text-xs"
                                        value={config.autoResolution}
                                        onChange={e => setConfig({ ...config, autoResolution: e.target.value })}
                                    >
                                        <option value="1080p">Target: 1080p (FHD)</option>
                                        <option value="4k">Target: 4K (UHD)</option>
                                    </select>
                                )}
                            </div>
                        </div>
                    </div>
                </div>

                {/* Watch Directories */}
                <div className="card">
                    <div className="card-header">
                        <h3 className="card-title">Watch Directories</h3>
                    </div>
                    <div className={`card-body ${!config.enabled ? 'opacity-50 pointer-events-none' : ''}`}>
                        <div className="flex gap-2 mb-4">
                            <input
                                type="text"
                                className="input"
                                value={newDir}
                                onChange={e => setNewDir(e.target.value)}
                                placeholder="/path/to/watch"
                                onKeyDown={e => e.key === 'Enter' && addWatchDir()}
                            />
                            <button
                                className="btn btn-secondary"
                                onClick={() => setShowFileBrowser(true)}
                                type="button"
                            >
                                Browse
                            </button>
                            <button className="btn btn-secondary" onClick={addWatchDir} disabled={!newDir}>Add</button>
                        </div>

                        {showFileBrowser && (
                            <FileBrowserModal
                                isOpen={showFileBrowser}
                                onClose={() => setShowFileBrowser(false)}
                                onSelect={(path) => setNewDir(path)}
                                selectMode="directory"
                                initialPath={newDir || '/'}
                                title="Select Watch Directory"
                            />
                        )}

                        <div className="flex flex-col gap-2">
                            {config.watchDirectories.length === 0 ? (
                                <p className="text-secondary text-sm text-center py-4">No directories configured</p>
                            ) : (
                                config.watchDirectories.map((dir, index) => (
                                    <div key={index} className="flex justify-between items-start p-3 bg-tertiary rounded border border-border">
                                        <div className="flex-1">
                                            <div className="font-medium font-mono text-sm">{dir.path}</div>
                                            <div className="flex flex-col gap-1 mt-1">
                                                <div className="flex gap-2">
                                                    <label className="flex items-center gap-1 text-xs text-secondary cursor-pointer">
                                                        <input
                                                            type="checkbox"
                                                            checked={dir.recursive}
                                                            onChange={() => {
                                                                const newDirs = [...config.watchDirectories];
                                                                newDirs[index].recursive = !newDirs[index].recursive;
                                                                setConfig({ ...config, watchDirectories: newDirs });
                                                            }}
                                                        />
                                                        Recursive
                                                    </label>
                                                    <span className="text-xs text-secondary">•</span>
                                                    <span className="text-xs text-secondary">
                                                        Include: {dir.includePatterns.join(', ')}
                                                    </span>
                                                </div>
                                                {dir.excludePatterns.length > 0 && (
                                                    <span className="text-xs text-secondary">
                                                        Exclude: {dir.excludePatterns.join(', ')}
                                                    </span>
                                                )}
                                            </div>
                                        </div>
                                        <button
                                            className="btn-icon text-danger hover-bg-danger-10"
                                            onClick={() => removeWatchDir(index)}
                                            title="Remove"
                                        >
                                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ width: '16px', height: '16px' }}>
                                                <path d="M6 18L18 6M6 6l12 12" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                                            </svg>
                                        </button>
                                    </div>
                                ))
                            )}
                        </div>
                    </div>
                </div>
            </div>

            <div className="flex justify-end mt-4">
                <button
                    className="btn btn-primary btn-lg"
                    onClick={handleSave}
                    disabled={isSaving}
                >
                    {isSaving ? (
                        <>
                            <div className="spinner" style={{ width: '16px', height: '16px', borderWidth: '2px' }} />
                            Saving...
                        </>
                    ) : (
                        'Save Configuration'
                    )}
                </button>
            </div>
        </div>
    );
}
