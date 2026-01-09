import type { Job, SystemConfig, SystemStats, DashboardStats } from '../types';
import './Dashboard.css';

interface DashboardProps {
    jobs: Job[];
    config: SystemConfig | null;
    stats: SystemStats | null;
    dashboardStats: DashboardStats | null;
}

export default function Dashboard({ jobs, config, stats, dashboardStats }: DashboardProps) {
    const activeJobs = jobs.filter(j => j.status === 'processing');
    const completedJobs = jobs.filter(j => j.status === 'completed');
    const failedJobs = jobs.filter(j => j.status === 'failed');
    const successRate = jobs.length > 0
        ? Math.round((completedJobs.length / jobs.length) * 100)
        : 0;

    const formatSize = (bytes: number) => {
        if (bytes === 0) return '0 B';
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    };

    return (
        <div className="dashboard">
            <div className="dashboard-header flex justify-between items-center">
                <div>
                    <h1>Dashboard</h1>
                    <p className="text-secondary">System overview and active jobs</p>
                </div>
                {config?.isPremium && dashboardStats && (
                    <div className="premium-efficiency-badge">
                        <div className="flex flex-col items-end">
                            <span className="text-[10px] opacity-70 uppercase tracking-widest font-bold">System Efficiency</span>
                            <span className="text-2xl font-black text-primary">{dashboardStats.efficiencyScore.toFixed(0)}%</span>
                        </div>
                        <div className="efficiency-ring" style={{ '--percent': dashboardStats.efficiencyScore } as any} />
                    </div>
                )}
            </div>

            {config?.isPremium && dashboardStats && (
                <div className="premium-insights-grid mt-4 mb-8">
                    <div className="insight-card glass">
                        <div className="insight-label">Storage Saved</div>
                        <div className="insight-value">{formatSize(dashboardStats.totalStorageSaved)}</div>
                        <div className="insight-sub">Life-time reduction</div>
                        <div className="insight-icon">💾</div>
                    </div>
                    <div className="insight-card glass">
                        <div className="insight-label">Subtitles Generated</div>
                        <div className="insight-value">{dashboardStats.totalSubtitlesCreated}</div>
                        <div className="insight-sub">AI Whisper Transcriptions</div>
                        <div className="insight-icon">💬</div>
                    </div>
                    <div className="insight-card glass">
                        <div className="insight-label">AI Upscales</div>
                        <div className="insight-value">{dashboardStats.totalUpscales}</div>
                        <div className="insight-sub">Enhanced to 1080p/4K</div>
                        <div className="insight-icon">✨</div>
                    </div>
                    <div className="insight-card glass">
                        <div className="insight-label">Smart Metdata</div>
                        <div className="insight-value">{dashboardStats.totalCleaned}</div>
                        <div className="insight-sub">Files organized by AI</div>
                        <div className="insight-icon">🏷️</div>
                    </div>
                </div>
            )}

            <div className="grid grid-4">
                <div className="stat-card">
                    <div className="stat-icon processing">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor">
                            <path d="M13 10V3L4 14h7v7l9-11h-7z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                    </div>
                    <div className="stat-content">
                        <div className="stat-value">{activeJobs.length}</div>
                        <div className="stat-label">Active Jobs</div>
                    </div>
                </div>

                <div className="stat-card">
                    <div className="stat-icon completed">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor">
                            <path d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                    </div>
                    <div className="stat-content">
                        <div className="stat-value">{completedJobs.length}</div>
                        <div className="stat-label">Completed</div>
                    </div>
                </div>

                <div className="stat-card">
                    <div className="stat-icon failed">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor">
                            <path d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                    </div>
                    <div className="stat-content">
                        <div className="stat-value">{failedJobs.length}</div>
                        <div className="stat-label">Failed</div>
                    </div>
                </div>

                <div className="stat-card">
                    <div className="stat-icon">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor">
                            <path d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                    </div>
                    <div className="stat-content">
                        <div className="stat-value">{successRate}%</div>
                        <div className="stat-label">Success Rate</div>
                    </div>
                </div>
            </div>

            <div className="grid grid-2 mt-4">
                {/* Real System Stats */}
                {stats && (
                    <div className="card">
                        <div className="card-header">
                            <h3 className="card-title">System Resources</h3>
                        </div>
                        <div className="card-body">
                            <div className="resource-list">
                                <div className="resource-item">
                                    <div className="resource-info">
                                        <span>CPU Usage</span>
                                        <span>{stats.cpuUsage.toFixed(1)}%</span>
                                    </div>
                                    <div className="progress-bar mini">
                                        <div className="progress-fill" style={{ width: `${stats.cpuUsage}%`, backgroundColor: stats.cpuUsage > 80 ? 'var(--status-failed)' : 'var(--brand-teal)' }} />
                                    </div>
                                </div>
                                <div className="resource-item">
                                    <div className="resource-info">
                                        <span>Memory Usage</span>
                                        <span>{stats.memoryUsage.toFixed(1)}%</span>
                                    </div>
                                    <div className="progress-bar mini">
                                        <div className="progress-fill" style={{ width: `${stats.memoryUsage}%`, backgroundColor: stats.memoryUsage > 80 ? 'var(--status-failed)' : 'var(--brand-teal)' }} />
                                    </div>
                                </div>
                                <div className="resource-item">
                                    <div className="resource-info">
                                        <span>GPU Utilization</span>
                                        <span>{stats.gpuUsage.toFixed(1)}%</span>
                                    </div>
                                    <div className="progress-bar mini">
                                        <div className="progress-fill" style={{ width: `${stats.gpuUsage}%`, backgroundColor: 'var(--brand-teal)' }} />
                                    </div>
                                </div>
                                <div className="resource-item">
                                    <div className="resource-info">
                                        <span>Disk Space (Free)</span>
                                        <span>{stats.diskFreeGB.toFixed(1)} GB</span>
                                    </div>
                                    <div className="progress-bar mini">
                                        <div className="progress-fill" style={{ width: `${100 - stats.diskUsage}%`, backgroundColor: stats.diskUsage > 90 ? 'var(--status-failed)' : 'var(--brand-teal)' }} />
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>
                )}

                {/* System Configuration */}
                {config && (
                    <div className="card">
                        <div className="card-header">
                            <h3 className="card-title">System Configuration</h3>
                        </div>
                        <div className="card-body">
                            <div className="config-grid">
                                <div className="config-item">
                                    <span className="config-label">GPU Vendor</span>
                                    <span className="config-value">{config.gpuVendor.toUpperCase()}</span>
                                </div>
                                <div className="config-item">
                                    <span className="config-label">Quality Preset</span>
                                    <span className="config-value">{config.qualityPreset}</span>
                                </div>
                                <div className="config-item">
                                    <span className="config-label">CRF</span>
                                    <span className="config-value">{config.crf}</span>
                                </div>
                                <div className="config-item">
                                    <span className="config-label">AI Provider</span>
                                    <span className="config-value">{config.aiProvider || 'None'}</span>
                                </div>
                            </div>
                        </div>
                    </div>
                )}
            </div>

            {activeJobs.length > 0 && (
                <div className="card mt-4">
                    <div className="card-header">
                        <h3 className="card-title">Active Jobs</h3>
                    </div>
                    <div className="card-body">
                        <div className="job-list">
                            {activeJobs.map(job => (
                                <div key={job.id} className="job-item">
                                    <div className="job-info">
                                        <div className="job-type">
                                            <span className={`badge badge-${job.type}`}>
                                                {job.type}
                                            </span>
                                        </div>
                                        <div className="job-path" title={job.sourcePath}>
                                            {job.sourcePath.split('/').pop()}
                                        </div>
                                    </div>
                                    <div className="job-progress">
                                        <div className="progress-info">
                                            <span>{job.progress}%</span>
                                            {job.fps > 0 && <span>{job.fps.toFixed(1)} FPS</span>}
                                            {job.eta && <span>ETA: {job.eta}</span>}
                                        </div>
                                        <div className="progress-bar">
                                            <div
                                                className="progress-fill"
                                                style={{ width: `${job.progress}%` }}
                                            />
                                        </div>
                                    </div>
                                </div>
                            ))}
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}
