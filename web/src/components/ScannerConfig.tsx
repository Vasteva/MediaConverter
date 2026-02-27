import { useState, useEffect, useMemo } from 'react';
import FileBrowserModal from './FileBrowserModal';
import type { ScannerConfig, WatchDirectory } from '../types';

interface DiscoveredFile {
    path: string;
    name: string;
    sizeBytes: number;
    extension: string;
    jobType: 'optimize' | 'extract';
    estimatedOutputBytes: number;
    estimatedSavingsBytes: number;
    estimatedSavingsPct: number;
}

function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return `${(bytes / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`;
}

interface ScannerConfigProps {
    config: ScannerConfig;
    onSave: (config: ScannerConfig) => Promise<boolean>;
}

interface ScanStatus {
    isScanning: boolean;
    currentPath: string;
    filesScanned: number;
    lastScan?: string;
    lastResult?: string;
    lastError?: string;
    duration?: string;
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
    const [scanStatus, setScanStatus] = useState<ScanStatus | null>(null);
    const [discoveredFiles, setDiscoveredFiles] = useState<DiscoveredFile[] | null>(null);
    const [isDiscovering, setIsDiscovering] = useState(false);
    const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());
    const [isQueuing, setIsQueuing] = useState(false);
    const [queueResult, setQueueResult] = useState<string | null>(null);
    const [filterType, setFilterType] = useState<'all' | 'optimize' | 'extract'>('all');
    type SortField = 'name' | 'sizeBytes' | 'estimatedSavingsPct' | 'jobType';
    const [sortField, setSortField] = useState<SortField>('name');
    const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc');

    const displayedFiles = useMemo(() => {
        if (!discoveredFiles) return [];
        const filtered = filterType === 'all' ? discoveredFiles : discoveredFiles.filter(f => f.jobType === filterType);
        return [...filtered].sort((a, b) => {
            let cmp = 0;
            if (sortField === 'name') cmp = a.name.localeCompare(b.name);
            else if (sortField === 'sizeBytes') cmp = a.sizeBytes - b.sizeBytes;
            else if (sortField === 'estimatedSavingsPct') cmp = a.estimatedSavingsPct - b.estimatedSavingsPct;
            else cmp = a.jobType.localeCompare(b.jobType);
            return sortDir === 'asc' ? cmp : -cmp;
        });
    }, [discoveredFiles, filterType, sortField, sortDir]);

    const handleSort = (field: SortField) => {
        if (sortField === field) setSortDir(d => d === 'asc' ? 'desc' : 'asc');
        else { setSortField(field); setSortDir('asc'); }
    };

    const sortIcon = (field: SortField) => {
        if (sortField !== field) return ' ⇅';
        return sortDir === 'asc' ? ' ↑' : ' ↓';
    };

    useEffect(() => {
        const fetchStatus = async () => {
            try {
                const token = sessionStorage.getItem('token');
                const res = await fetch('/api/scanner/status', {
                    headers: { 'Authorization': `Bearer ${token}` }
                });
                if (res.ok) {
                    const data = await res.json();
                    setScanStatus(data);
                }
            } catch (e) {
                console.error("Failed to fetch scan status", e);
            }
        };

        fetchStatus();
        const interval = setInterval(fetchStatus, 2000);
        return () => clearInterval(interval);
    }, []);

    // Update local state if initialConfig changes
    const [prevInitialConfig, setPrevInitialConfig] = useState(initialConfig);
    if (initialConfig !== prevInitialConfig) {
        setPrevInitialConfig(initialConfig);
        setConfig(ensureConfig(initialConfig));
    }

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

    const handleDiscover = async () => {
        setIsDiscovering(true);
        setDiscoveredFiles(null);
        setSelectedPaths(new Set());
        setQueueResult(null);
        try {
            const token = sessionStorage.getItem('token');
            const res = await fetch('/api/scanner/discover', {
                headers: { 'Authorization': `Bearer ${token}` }
            });
            if (!res.ok) throw new Error(`Error: ${res.statusText}`);
            const data = await res.json();
            setDiscoveredFiles(data.files || []);
        } catch (e) {
            setDiscoveredFiles([]);
            alert('Discovery failed: ' + (e instanceof Error ? e.message : String(e)));
        } finally {
            setIsDiscovering(false);
        }
    };

    const handleQueueSelected = async () => {
        if (selectedPaths.size === 0) return;
        setIsQueuing(true);
        setQueueResult(null);
        try {
            const token = sessionStorage.getItem('token');
            const res = await fetch('/api/scanner/queue', {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${token}`,
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({ paths: Array.from(selectedPaths) })
            });
            const data = await res.json();
            setQueueResult(`${data.queued} job${data.queued !== 1 ? 's' : ''} added to queue`);
            // Remove queued files from discovery results
            setDiscoveredFiles(prev => prev?.filter(f => !selectedPaths.has(f.path)) ?? null);
            setSelectedPaths(new Set());
        } catch (e) {
            alert('Queue failed: ' + (e instanceof Error ? e.message : String(e)));
        } finally {
            setIsQueuing(false);
        }
    };

    const toggleSelectAll = () => {
        const allDisplayedSelected = displayedFiles.length > 0 && displayedFiles.every(f => selectedPaths.has(f.path));
        if (allDisplayedSelected) {
            setSelectedPaths(prev => {
                const next = new Set(prev);
                displayedFiles.forEach(f => next.delete(f.path));
                return next;
            });
        } else {
            setSelectedPaths(prev => {
                const next = new Set(prev);
                displayedFiles.forEach(f => next.add(f.path));
                return next;
            });
        }
    };

    const toggleSelect = (path: string) => {
        setSelectedPaths(prev => {
            const next = new Set(prev);
            if (next.has(path)) next.delete(path);
            else next.add(path);
            return next;
        });
    };

    return (
        <div className="scanner-config-view">
            <div className="view-header">
                <h1>Scanner Configuration</h1>
                <p className="text-secondary">Configure automatic file discovery and processing</p>
            </div>

            {/* Status Card */}
            {scanStatus && (
                <div className="card mb-6" style={{ borderLeft: scanStatus.isScanning ? '4px solid var(--brand-teal)' : '4px solid transparent' }}>
                    <div className="card-body flex items-center justify-between">
                        <div>
                            <h3 className="text-lg font-medium mb-1">
                                {scanStatus.isScanning ? 'Scanner Running...' : 'Scanner Idle'}
                            </h3>
                            <div className="text-secondary text-sm">
                                {scanStatus.isScanning ? (
                                    <span>Scanning: <span className="font-mono text-primary">{scanStatus.currentPath?.split('/').pop() || 'Initializing...'}</span></span>
                                ) : (
                                    <div className="flex flex-col gap-1">
                                        <span>Last scan: {scanStatus.lastScan ? new Date(scanStatus.lastScan).toLocaleString() : 'Never'}</span>
                                        {scanStatus.duration && <span className="text-xs opacity-70">Duration: {scanStatus.duration}</span>}
                                        {scanStatus.lastResult && <span className="text-success font-medium">{scanStatus.lastResult}</span>}
                                        {scanStatus.lastError && <span className="text-danger font-medium">{scanStatus.lastError}</span>}
                                    </div>
                                )}
                            </div>
                        </div>
                        <div className="text-right">
                            <div className="text-2xl font-bold">{scanStatus.filesScanned}</div>
                            <div className="text-xs text-secondary uppercase tracking-wider">Files Scanned</div>
                        </div>
                    </div>
                    {scanStatus.isScanning && (
                        <div className="h-1 bg-tertiary w-full overflow-hidden relative">
                            <div className="absolute inset-0 bg-primary opacity-50 w-1/2 animate-progress-indeterminate"></div>
                        </div>
                    )}
                </div>
            )}

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
                                Download Subtitles
                            </label>
                            <label className="flex items-center gap-2 cursor-pointer">
                                <input
                                    type="checkbox"
                                    checked={config.autoCreateSubtitles}
                                    onChange={e => setConfig({ ...config, autoCreateSubtitles: e.target.checked })}
                                    className="w-4 h-4"
                                />
                                <span className="text-secondary text-sm">Attempt subtitle download for auto-created jobs</span>
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

            <div className="flex justify-end mt-4" style={{ gap: '0.75rem' }}>
                <button
                    type="button"
                    className="btn btn-secondary btn-lg"
                    onClick={() => {
                        if (scanStatus) {
                            setScanStatus({ ...scanStatus, isScanning: true, currentPath: 'Starting...' });
                        }
                        fetch('/api/scanner/scan', {
                            method: 'POST',
                            headers: { 'Authorization': `Bearer ${sessionStorage.getItem('token')}` }
                        })
                            .then(res => { if (!res.ok) alert('Failed to start scan'); })
                            .catch(err => alert('Error starting scan: ' + err.message));
                    }}
                    disabled={scanStatus?.isScanning}
                >
                    {scanStatus?.isScanning ? 'Scanning...' : 'Scan Now'}
                </button>
                <button
                    type="button"
                    className="btn btn-secondary btn-lg"
                    onClick={handleDiscover}
                    disabled={isDiscovering || scanStatus?.isScanning}
                >
                    {isDiscovering ? (
                        <><div className="spinner" style={{ width: '16px', height: '16px', borderWidth: '2px' }} /> Discovering...</>
                    ) : 'Discover Files'}
                </button>
                <button
                    className="btn btn-primary btn-lg"
                    onClick={handleSave}
                    disabled={isSaving}
                >
                    {isSaving ? (
                        <><div className="spinner" style={{ width: '16px', height: '16px', borderWidth: '2px' }} /> Saving...</>
                    ) : 'Save Configuration'}
                </button>
            </div>

            {/* Discovery Results */}
            {discoveredFiles !== null && (
                <div className="card mt-6">
                    <div className="card-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '0.75rem' }}>
                        <div>
                            <h3 className="card-title">Discovered Files</h3>
                            <p className="text-secondary text-sm mt-1">
                                {discoveredFiles.length === 0
                                    ? 'No new files found'
                                    : `${discoveredFiles.length} file${discoveredFiles.length !== 1 ? 's' : ''} ready to queue${filterType !== 'all' ? ` · ${displayedFiles.length} shown` : ''} · ${selectedPaths.size} selected`}
                            </p>
                        </div>
                        {discoveredFiles.length > 0 && (
                            <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', flexWrap: 'wrap' }}>
                                <div style={{ display: 'flex', gap: '0.25rem', background: 'var(--bg-tertiary)', borderRadius: '6px', padding: '3px' }}>
                                    {(['all', 'optimize', 'extract'] as const).map(t => (
                                        <button
                                            key={t}
                                            type="button"
                                            onClick={() => setFilterType(t)}
                                            style={{
                                                padding: '3px 10px', borderRadius: '4px', fontSize: '0.8rem', fontWeight: 500,
                                                background: filterType === t ? 'var(--bg-card)' : 'transparent',
                                                color: filterType === t ? 'var(--text-primary)' : 'var(--text-secondary)',
                                                border: 'none', cursor: 'pointer', transition: 'all 0.15s'
                                            }}
                                        >
                                            {t === 'all' ? 'All' : t.charAt(0).toUpperCase() + t.slice(1)}
                                        </button>
                                    ))}
                                </div>
                                <button className="btn btn-secondary" onClick={toggleSelectAll}>
                                    {displayedFiles.length > 0 && displayedFiles.every(f => selectedPaths.has(f.path)) ? 'Deselect All' : 'Select All'}
                                </button>
                                <button
                                    className="btn btn-primary"
                                    onClick={handleQueueSelected}
                                    disabled={selectedPaths.size === 0 || isQueuing}
                                >
                                    {isQueuing ? (
                                        <><div className="spinner" style={{ width: '14px', height: '14px', borderWidth: '2px' }} /> Queuing...</>
                                    ) : `Queue Selected (${selectedPaths.size})`}
                                </button>
                            </div>
                        )}
                    </div>

                    {queueResult && (
                        <div style={{ margin: '0 1.5rem', padding: '0.75rem 1rem', background: 'rgba(61, 217, 208, 0.1)', color: 'var(--brand-teal)', borderRadius: '8px', border: '1px solid var(--brand-teal)', fontSize: '0.9rem' }}>
                            {queueResult}
                        </div>
                    )}

                    {discoveredFiles.length > 0 && (
                        <div className="card-body" style={{ padding: 0 }}>
                            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                                <thead>
                                    <tr style={{ background: 'var(--bg-tertiary)' }}>
                                        <th style={{ width: '48px', padding: '0.75rem 1rem', textAlign: 'center' }}>
                                            <input
                                                type="checkbox"
                                                checked={displayedFiles.length > 0 && displayedFiles.every(f => selectedPaths.has(f.path))}
                                                onChange={toggleSelectAll}
                                            />
                                        </th>
                                        <th
                                            onClick={() => handleSort('name')}
                                            style={{ textAlign: 'left', padding: '0.75rem 1rem', fontWeight: 600, fontSize: '0.85rem', color: sortField === 'name' ? 'var(--text-primary)' : 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', cursor: 'pointer', userSelect: 'none' }}
                                        >File{sortIcon('name')}</th>
                                        <th
                                            onClick={() => handleSort('jobType')}
                                            style={{ width: '80px', textAlign: 'center', padding: '0.75rem 1rem', fontWeight: 600, fontSize: '0.85rem', color: sortField === 'jobType' ? 'var(--text-primary)' : 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', cursor: 'pointer', userSelect: 'none' }}
                                        >Type{sortIcon('jobType')}</th>
                                        <th
                                            onClick={() => handleSort('sizeBytes')}
                                            style={{ width: '100px', textAlign: 'right', padding: '0.75rem 1rem', fontWeight: 600, fontSize: '0.85rem', color: sortField === 'sizeBytes' ? 'var(--text-primary)' : 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', cursor: 'pointer', userSelect: 'none' }}
                                        >Size{sortIcon('sizeBytes')}</th>
                                        <th
                                            onClick={() => handleSort('estimatedSavingsPct')}
                                            style={{ width: '120px', textAlign: 'right', padding: '0.75rem 1rem', fontWeight: 600, fontSize: '0.85rem', color: sortField === 'estimatedSavingsPct' ? 'var(--text-primary)' : 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', cursor: 'pointer', userSelect: 'none' }}
                                        >Est. Savings{sortIcon('estimatedSavingsPct')}</th>
                                        <th style={{ width: '160px', padding: '0.75rem 1rem' }}></th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {displayedFiles.map((file) => {
                                        const isSelected = selectedPaths.has(file.path);
                                        return (
                                            <tr
                                                key={file.path}
                                                onClick={() => toggleSelect(file.path)}
                                                style={{
                                                    cursor: 'pointer',
                                                    background: isSelected ? 'rgba(61, 217, 208, 0.08)' : 'transparent',
                                                    borderLeft: isSelected ? '3px solid var(--brand-teal)' : '3px solid transparent',
                                                    transition: 'background 0.1s'
                                                }}
                                            >
                                                <td style={{ padding: '0.875rem 1rem', textAlign: 'center', borderBottom: '1px solid var(--border-color)' }}>
                                                    <input type="checkbox" checked={isSelected} onChange={() => toggleSelect(file.path)} onClick={e => e.stopPropagation()} />
                                                </td>
                                                <td style={{ padding: '0.875rem 1rem', borderBottom: '1px solid var(--border-color)' }}>
                                                    <div className="font-medium" style={{ fontSize: '0.9rem' }}>{file.name}</div>
                                                    <div className="text-secondary" style={{ fontSize: '0.75rem', fontFamily: 'monospace', marginTop: '2px', opacity: 0.7 }}>{file.path}</div>
                                                </td>
                                                <td style={{ padding: '0.875rem 1rem', textAlign: 'center', borderBottom: '1px solid var(--border-color)' }}>
                                                    <span style={{
                                                        padding: '2px 8px', borderRadius: '4px', fontSize: '0.75rem', fontWeight: 600,
                                                        background: file.jobType === 'optimize' ? 'rgba(61, 217, 208, 0.15)' : 'rgba(168, 85, 247, 0.15)',
                                                        color: file.jobType === 'optimize' ? 'var(--brand-teal)' : '#a855f7'
                                                    }}>
                                                        {file.jobType}
                                                    </span>
                                                </td>
                                                <td style={{ padding: '0.875rem 1rem', textAlign: 'right', borderBottom: '1px solid var(--border-color)', fontFamily: 'monospace', fontSize: '0.85rem' }}>
                                                    {formatBytes(file.sizeBytes)}
                                                </td>
                                                <td style={{ padding: '0.875rem 1rem', textAlign: 'right', borderBottom: '1px solid var(--border-color)' }}>
                                                    {file.estimatedSavingsPct > 0 ? (
                                                        <div>
                                                            <div style={{ color: 'var(--status-completed)', fontWeight: 600, fontSize: '0.9rem' }}>
                                                                -{file.estimatedSavingsPct.toFixed(0)}%
                                                            </div>
                                                            <div className="text-secondary" style={{ fontSize: '0.75rem', fontFamily: 'monospace' }}>
                                                                ~{formatBytes(file.estimatedSavingsBytes)} saved
                                                            </div>
                                                        </div>
                                                    ) : (
                                                        <span className="text-secondary" style={{ fontSize: '0.8rem' }}>N/A</span>
                                                    )}
                                                </td>
                                                <td style={{ padding: '0.875rem 1rem', borderBottom: '1px solid var(--border-color)' }}>
                                                    {file.estimatedSavingsPct > 0 && (
                                                        <div style={{ background: 'var(--bg-tertiary)', borderRadius: '4px', height: '6px', overflow: 'hidden' }}>
                                                            <div style={{
                                                                height: '100%',
                                                                width: `${file.estimatedSavingsPct}%`,
                                                                background: 'var(--brand-teal)',
                                                                borderRadius: '4px'
                                                            }} />
                                                        </div>
                                                    )}
                                                </td>
                                            </tr>
                                        );
                                    })}
                                </tbody>
                            </table>
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}
