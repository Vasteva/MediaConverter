import type { Job } from '../types';

interface JobListProps {
    jobs: Job[];
    onCreateJob: (job: Partial<Job>) => Promise<boolean>;
    onCancelJob: (jobId: string) => Promise<boolean>;
}

export default function JobList({ jobs, onCreateJob, onCancelJob }: JobListProps) {
    return (
        <div className="job-list-view">
            <div className="view-header">
                <h1>Jobs</h1>
                <button className="btn btn-primary">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ width: '20px', height: '20px' }}>
                        <path d="M12 4v16m8-8H4" strokeWidth="2" strokeLinecap="round" />
                    </svg>
                    Create Job
                </button>
            </div>

            <div className="card mt-4">
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
                        {jobs.length === 0 ? (
                            <tr>
                                <td colSpan={6} style={{ textAlign: 'center', padding: '2rem', color: 'var(--text-secondary)' }}>
                                    No jobs yet. Create your first job to get started!
                                </td>
                            </tr>
                        ) : (
                            jobs.map(job => (
                                <tr key={job.id}>
                                    <td>
                                        <span className={`badge badge-${job.type}`}>
                                            {job.type}
                                        </span>
                                    </td>
                                    <td style={{ fontFamily: 'monospace', fontSize: '0.875rem' }}>
                                        {job.sourcePath}
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
                                            <div style={{ fontSize: '0.75rem', marginTop: '0.25rem', color: 'var(--text-secondary)' }}>
                                                {job.progress}% {job.fps > 0 && `• ${job.fps.toFixed(1)} FPS`}
                                            </div>
                                        </div>
                                    </td>
                                    <td style={{ fontSize: '0.875rem', color: 'var(--text-secondary)' }}>
                                        {new Date(job.createdAt).toLocaleString()}
                                    </td>
                                    <td>
                                        {job.status === 'processing' && (
                                            <button
                                                className="btn btn-sm btn-danger"
                                                onClick={() => onCancelJob(job.id)}
                                            >
                                                Cancel
                                            </button>
                                        )}
                                    </td>
                                </tr>
                            ))
                        )}
                    </tbody>
                </table>
            </div>
        </div>
    );
}
