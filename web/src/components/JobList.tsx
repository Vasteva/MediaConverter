import { useState } from 'react';
import FileBrowserModal from './FileBrowserModal';
import type { Job } from '../types';

interface JobListProps {
    jobs: Job[];
    onCreateJob: (job: Partial<Job>) => Promise<boolean>;
    onCancelJob: (jobId: string) => Promise<boolean>;
}

type FilterType = 'all' | 'active' | 'completed' | 'failed';

export default function JobList({ jobs, onCreateJob, onCancelJob }: JobListProps) {
    const [filter, setFilter] = useState<FilterType>('all');
    const [showCreateModal, setShowCreateModal] = useState(false);

    // Create Job Form State
    const [newJobType, setNewJobType] = useState<'optimize' | 'extract' | 'test'>('optimize');
    const [sourcePath, setSourcePath] = useState('');
    const [destPath, setDestPath] = useState('');
    const [createSubtitles, setCreateSubtitles] = useState(false);
    const [upscale, setUpscale] = useState(false);
    const [resolution, setResolution] = useState('1080p');
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [showFileBrowser, setShowFileBrowser] = useState(false);
    const [activeBrowserField, setActiveBrowserField] = useState<'source' | 'dest' | null>(null);

    const openBrowser = (field: 'source' | 'dest') => {
        setActiveBrowserField(field);
        setShowFileBrowser(true);
    };

    const filteredJobs = jobs.filter(job => {
        if (filter === 'all') return true;
        if (filter === 'active') return ['pending', 'processing'].includes(job.status);
        if (filter === 'completed') return job.status === 'completed';
        if (filter === 'failed') return ['failed', 'cancelled'].includes(job.status);
        return true;
    });

    const handleCreateJob = async (e: React.FormEvent) => {
        e.preventDefault();
        setIsSubmitting(true);

        const success = await onCreateJob({
            type: newJobType,
            sourcePath,
            destinationPath: destPath || undefined,
            priority: 5,
            createSubtitles,
            upscale,
            resolution
        });

        setIsSubmitting(false);
        if (success) {
            setShowCreateModal(false);
            setSourcePath('');
            setDestPath('');
        }
    };

    return (
        <div className="job-list-view">
            <div className="view-header">
                <div className="flex justify-between items-center">
                    <div>
                        <h1>Jobs</h1>
                        <p className="text-secondary">Manage your media processing queue</p>
                    </div>
                    <button
                        className="btn btn-primary"
                        onClick={() => setShowCreateModal(true)}
                    >
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ width: '20px', height: '20px' }}>
                            <path d="M12 4v16m8-8H4" strokeWidth="2" strokeLinecap="round" />
                        </svg>
                        Create Job
                    </button>
                </div>
            </div>

            {/* Filter Tabs */}
            <div className="flex gap-2 mb-4">
                {(['all', 'active', 'completed', 'failed'] as FilterType[]).map((f) => (
                    <button
                        key={f}
                        className={`btn btn-sm ${filter === f ? 'btn-primary' : 'btn-secondary'}`}
                        onClick={() => setFilter(f)}
                        style={{ textTransform: 'capitalize' }}
                    >
                        {f}
                    </button>
                ))}
            </div>

            <div className="card">
                <div style={{ overflowX: 'auto' }}>
                    <table className="table">
                        <thead>
                            <tr>
                                <th>Type</th>
                                <th>Source</th>
                                <th>Status</th>
                                <th>Progress</th>
                                <th>Created</th>
                                <th>Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            {filteredJobs.length === 0 ? (
                                <tr>
                                    <td colSpan={6} style={{ textAlign: 'center', padding: '3rem', color: 'var(--text-secondary)' }}>
                                        <div className="flex flex-col items-center gap-2">
                                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ width: '48px', height: '48px', opacity: 0.5 }}>
                                                <path d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" strokeWidth="2" strokeLinecap="round" />
                                            </svg>
                                            <p>No jobs found for this filter</p>
                                        </div>
                                    </td>
                                </tr>
                            ) : (
                                filteredJobs.map(job => (
                                    <tr key={job.id}>
                                        <td>
                                            <div className="flex flex-col gap-1">
                                                <span className={`badge badge-${job.type}`}>
                                                    {job.type}
                                                </span>
                                                {job.createSubtitles && (
                                                    <div className="flex items-center text-[10px] text-primary gap-1 opacity-80">
                                                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ width: '10px', height: '10px' }}>
                                                            <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2v10z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                                                        </svg>
                                                        WHISPER
                                                    </div>
                                                )}
                                                {job.upscale && (
                                                    <div className="flex items-center text-[10px] text-primary gap-1 opacity-80">
                                                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ width: '10px', height: '10px' }}>
                                                            <path d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5l-5-5m5 5v-4m0 4h-4" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                                                        </svg>
                                                        UPSCALE: {job.resolution}
                                                    </div>
                                                )}
                                            </div>
                                        </td>
                                        <td>
                                            <div className="flex flex-col">
                                                <span style={{ fontFamily: 'monospace', fontSize: '0.8125rem' }} title={job.sourcePath}>
                                                    {job.sourcePath.split('/').pop()}
                                                </span>
                                                <span className="text-xs text-secondary" style={{ maxWidth: '300px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                                                    {job.sourcePath}
                                                </span>
                                            </div>
                                        </td>
                                        <td>
                                            <div className="flex flex-col gap-1">
                                                <span className={`badge badge-${job.status}`} title={job.error || ''}>
                                                    {job.status === 'processing' && job.statusDetail ? job.statusDetail : job.status}
                                                </span>
                                                {job.status === 'failed' && job.error && (
                                                    <span className="text-xs text-danger" style={{ maxWidth: '200px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }} title={job.error}>
                                                        {job.error}
                                                    </span>
                                                )}
                                            </div>
                                        </td>
                                        <td>
                                            <div style={{ minWidth: '150px' }}>
                                                <div className="progress-bar">
                                                    <div className="progress-fill" style={{ width: `${job.progress}%` }} />
                                                </div>
                                                <div className="flex justify-between mt-1 text-xs text-secondary">
                                                    <span>{job.progress}%</span>
                                                    {job.status === 'processing' && (
                                                        <span>{job.statusDetail ? job.statusDetail : ''} {job.eta} ({job.fps.toFixed(0)} fps)</span>
                                                    )}
                                                </div>
                                            </div>
                                        </td>
                                        <td className="text-sm text-secondary">
                                            {new Date(job.createdAt).toLocaleString()}
                                        </td>
                                        <td>
                                            {job.status === 'processing' || job.status === 'pending' ? (
                                                <button
                                                    className="btn btn-sm btn-danger"
                                                    onClick={() => onCancelJob(job.id)}
                                                    title="Cancel Job"
                                                >
                                                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ width: '16px', height: '16px' }}>
                                                        <path d="M6 18L18 6M6 6l12 12" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                                                    </svg>
                                                </button>
                                            ) : null}
                                        </td>
                                    </tr>
                                ))
                            )}
                        </tbody>
                    </table>
                </div>
            </div>

            {/* Create Job Modal */}
            {showCreateModal && (
                <div className="modal-backdrop" onClick={() => setShowCreateModal(false)}>
                    <div className="modal-content" onClick={e => e.stopPropagation()}>
                        <div className="modal-header">
                            <h3>Create New Job</h3>
                            <button className="btn-icon" onClick={() => setShowCreateModal(false)}>
                                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ width: '20px', height: '20px' }}>
                                    <path d="M6 18L18 6M6 6l12 12" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                                </svg>
                            </button>
                        </div>
                        <form onSubmit={handleCreateJob}>
                            <div className="modal-body">
                                <div className="form-group mb-4">
                                    <label className="label mb-2 block">Job Type</label>
                                    <div className="flex gap-2">
                                        {(['optimize', 'extract', 'test'] as const).map(type => (
                                            <button
                                                key={type}
                                                type="button"
                                                className={`btn flex-1 ${newJobType === type ? 'btn-primary' : 'btn-secondary'}`}
                                                onClick={() => setNewJobType(type)}
                                                style={{ textTransform: 'capitalize' }}
                                            >
                                                {type}
                                            </button>
                                        ))}
                                    </div>
                                </div>

                                <div className="form-group mb-4">
                                    <label className="label mb-2 block">Source Path</label>
                                    <div className="flex gap-2">
                                        <input
                                            type="text"
                                            className="input"
                                            value={sourcePath}
                                            onChange={e => setSourcePath(e.target.value)}
                                            placeholder="/path/to/source/file.mkv"
                                            required
                                        />
                                        <button
                                            type="button"
                                            className="btn btn-secondary"
                                            onClick={() => openBrowser('source')}
                                        >
                                            Browse
                                        </button>
                                    </div>
                                    <p className="text-xs text-secondary mt-1">
                                        Absolute path to the source file on the server.
                                    </p>
                                </div>

                                <div className="form-group mb-4">
                                    <label className="label mb-2 block">Destination Path (Optional)</label>
                                    <div className="flex gap-2">
                                        <input
                                            type="text"
                                            className="input"
                                            value={destPath}
                                            onChange={e => setDestPath(e.target.value)}
                                            placeholder="/path/to/output/file.mkv"
                                        />
                                        <button
                                            type="button"
                                            className="btn btn-secondary"
                                            onClick={() => openBrowser('dest')}
                                        >
                                            Browse
                                        </button>
                                    </div>
                                    <p className="text-xs text-secondary mt-1">
                                        Leave empty to auto-generate based on source.
                                    </p>
                                </div>

                                <div className="form-group mb-4">
                                    <label className="flex items-center gap-2 cursor-pointer">
                                        <input
                                            type="checkbox"
                                            checked={createSubtitles}
                                            onChange={e => setCreateSubtitles(e.target.checked)}
                                            className="w-4 h-4"
                                        />
                                        <div className="flex items-center">
                                            <span className="text-sm font-medium">Create AI Subtitles (Whisper)</span>
                                            <span className="pro-tag ml-2">PRO</span>
                                        </div>
                                    </label>
                                    <p className="text-xs text-secondary mt-1 ml-6">
                                        Automatically generate SRT subtitles using AI transcription.
                                    </p>
                                </div>

                                <div className="form-group mb-4">
                                    <label className="flex items-center gap-2 cursor-pointer">
                                        <input
                                            type="checkbox"
                                            checked={upscale}
                                            onChange={e => setUpscale(e.target.checked)}
                                            className="w-4 h-4"
                                        />
                                        <div className="flex items-center">
                                            <span className="text-sm font-medium">AI Upscaling (Super Resolution)</span>
                                            <span className="pro-tag ml-2">PRO</span>
                                        </div>
                                    </label>
                                    <p className="text-xs text-secondary mt-1 ml-6">
                                        Upscale video to higher resolution using AI enhancers.
                                    </p>

                                    {upscale && (
                                        <div className="mt-2 ml-6">
                                            <select
                                                className="input select text-sm"
                                                value={resolution}
                                                onChange={e => setResolution(e.target.value)}
                                            >
                                                <option value="1080p">Target: 1080p (FHD)</option>
                                                <option value="4k">Target: 4K (UHD)</option>
                                            </select>
                                        </div>
                                    )}
                                </div>
                            </div>
                            <div className="modal-footer flexjustify-end gap-2">
                                <button
                                    type="button"
                                    className="btn btn-secondary"
                                    onClick={() => setShowCreateModal(false)}
                                    disabled={isSubmitting}
                                >
                                    Cancel
                                </button>
                                <button
                                    type="submit"
                                    className="btn btn-primary"
                                    disabled={isSubmitting || !sourcePath}
                                >
                                    {isSubmitting ? 'Creating...' : 'Create Job'}
                                </button>
                            </div>
                        </form>
                    </div>
                </div>
            )}

            {showFileBrowser && (
                <FileBrowserModal
                    isOpen={showFileBrowser}
                    onClose={() => {
                        setShowFileBrowser(false);
                        setActiveBrowserField(null);
                    }}
                    onSelect={(path) => {
                        if (activeBrowserField === 'source') setSourcePath(path);
                        if (activeBrowserField === 'dest') setDestPath(path);
                    }}
                    selectMode={activeBrowserField === 'source' ? 'file' : 'both'}
                    title={activeBrowserField === 'source' ? "Select Source File" : "Select Destination"}
                    initialPath={activeBrowserField === 'source' ? (sourcePath || '/') : (destPath || '/')}
                    zIndex={1100}
                />
            )}
        </div>
    );
}
