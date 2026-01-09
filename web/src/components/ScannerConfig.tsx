import { useState } from 'react';

interface WatchDirectory {
    path: string;
    recursive: boolean;
    patterns: string[];
}

interface ScannerState {
    enabled: boolean;
    mode: 'manual' | 'startup' | 'periodic' | 'watch' | 'hybrid';
    intervalSec: number;
    watchDirectories: WatchDirectory[];
    excludePatterns: string[];
}

// Mock initial state
const INITIAL_STATE: ScannerState = {
    enabled: true,
    mode: 'watch',
    intervalSec: 300,
    watchDirectories: [
        { path: '/data/media/downloads', recursive: true, patterns: ['*.iso', '*.mkv'] },
        { path: '/data/media/upload', recursive: false, patterns: ['*.mp4'] }
    ],
    excludePatterns: ['*.tmp', '*.part', '._*']
};

export default function ScannerConfig() {
    const [config, setConfig] = useState<ScannerState>(INITIAL_STATE);
    const [isSaving, setIsSaving] = useState(false);
    const [newDir, setNewDir] = useState('');

    const handleSave = async () => {
        setIsSaving(true);
        // Simulator API call
        await new Promise(resolve => setTimeout(resolve, 1000));
        setIsSaving(false);
        // Show success toast (todo)
    };

    const addWatchDir = () => {
        if (!newDir) return;
        setConfig({
            ...config,
            watchDirectories: [
                ...config.watchDirectories,
                { path: newDir, recursive: true, patterns: ['*'] }
            ]
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
                                onChange={e => setConfig({ ...config, mode: e.target.value as any })}
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
                                    value={config.intervalSec}
                                    onChange={e => setConfig({ ...config, intervalSec: parseInt(e.target.value) })}
                                    min="60"
                                />
                            </div>
                        )}

                        <div className="form-group">
                            <label className="label mb-2 block">Exclude Patterns (comma separated)</label>
                            <input
                                type="text"
                                className="input"
                                value={config.excludePatterns.join(', ')}
                                onChange={e => setConfig({
                                    ...config,
                                    excludePatterns: e.target.value.split(',').map(s => s.trim()).filter(Boolean)
                                })}
                                placeholder="*.tmp, *.part"
                            />
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
                            <button className="btn btn-secondary" onClick={addWatchDir} disabled={!newDir}>Add</button>
                        </div>

                        <div className="flex flex-col gap-2">
                            {config.watchDirectories.length === 0 ? (
                                <p className="text-secondary text-sm text-center py-4">No directories configured</p>
                            ) : (
                                config.watchDirectories.map((dir, index) => (
                                    <div key={index} className="flex justify-between items-start p-3 bg-tertiary rounded border border-border">
                                        <div className="flex-1">
                                            <div className="font-medium font-mono text-sm">{dir.path}</div>
                                            <div className="flex gap-2 mt-1">
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
                                                <span className="text-xs text-secondary">{dir.patterns.join(', ')}</span>
                                            </div>
                                        </div>
                                        <button
                                            className="btn-icon text-danger hover:bg-danger/10"
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
