import { useState } from 'react';
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
    const [isSubmitting, setIsSubmitting] = useState(false);

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
            priority: 5
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
                                            <span className={`badge badge-${job.type}`}>
                                                {job.type}
                                            </span>
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
                                            <span className={`badge badge-${job.status}`}>
                                                {job.status}
                                            </span>
                                        </td>
                                        <td>
                                            <div style={{ minWidth: '150px' }}>
                                                <div className="progress-bar">
                                                    <div className="progress-fill" style={{ width: `${job.progress}%` }} />
                                                </div>
                                                <div className="flex justify-between mt-1 text-xs text-secondary">
                                                    <span>{job.progress}%</span>
                                                    {job.status === 'processing' && (
                                                        <span>{job.eta} ({job.fps.toFixed(0)} fps)</span>
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
                                    <input
                                        type="text"
                                        className="input"
                                        value={sourcePath}
                                        onChange={e => setSourcePath(e.target.value)}
                                        placeholder="/path/to/source/file.mkv"
                                        required
                                    />
                                    <p className="text-xs text-secondary mt-1">
                                        Absolute path to the source file on the server.
                                    </p>
                                </div>

                                <div className="form-group mb-4">
                                    <label className="label mb-2 block">Destination Path (Optional)</label>
                                    <input
                                        type="text"
                                        className="input"
                                        value={destPath}
                                        onChange={e => setDestPath(e.target.value)}
                                        placeholder="/path/to/output/file.mkv"
                                    />
                                    <p className="text-xs text-secondary mt-1">
                                        Leave empty to auto-generate based on source.
                                    </p>
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
        </div>
    );
}
