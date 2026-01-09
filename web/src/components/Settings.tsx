import type { SystemConfig } from '../types';

interface SettingsProps {
    config: SystemConfig | null;
    onConfigUpdate: () => void;
}

export default function Settings({ config }: SettingsProps) {
    if (!config) {
        return <div>Loading...</div>;
    }

    return (
        <div className="settings-view">
            <div className="view-header">
                <h1>Settings</h1>
                <p className="text-secondary">Configure system settings and preferences</p>
            </div>

            <div className="card mt-4">
                <div className="card-header">
                    <h3 className="card-title">Encoding Settings</h3>
                </div>
                <div className="card-body">
                    <div className="settings-grid">
                        <div className="setting-item">
                            <label className="setting-label">GPU Vendor</label>
                            <select className="input select" value={config.gpuVendor} disabled>
                                <option value="cpu">CPU</option>
                                <option value="nvidia">NVIDIA</option>
                                <option value="intel">Intel</option>
                                <option value="amd">AMD</option>
                            </select>
                        </div>

                        <div className="setting-item">
                            <label className="setting-label">Quality Preset</label>
                            <select className="input select" value={config.qualityPreset} disabled>
                                <option value="fast">Fast</option>
                                <option value="medium">Medium</option>
                                <option value="slow">Slow</option>
                            </select>
                        </div>

                        <div className="setting-item">
                            <label className="setting-label">CRF (Quality)</label>
                            <input
                                type="number"
                                className="input"
                                value={config.crf}
                                disabled
                                min="18"
                                max="28"
                            />
                            <span className="text-xs text-secondary mt-1">Lower = better quality, larger file</span>
                        </div>
                    </div>
                </div>
            </div>

            <div className="card mt-4">
                <div className="card-header">
                    <h3 className="card-title">Paths</h3>
                </div>
                <div className="card-body">
                    <div className="settings-grid">
                        <div className="setting-item">
                            <label className="setting-label">Source Directory</label>
                            <input
                                type="text"
                                className="input"
                                value={config.sourceDir}
                                disabled
                                style={{ fontFamily: 'monospace' }}
                            />
                        </div>

                        <div className="setting-item">
                            <label className="setting-label">Destination Directory</label>
                            <input
                                type="text"
                                className="input"
                                value={config.destDir}
                                disabled
                                style={{ fontFamily: 'monospace' }}
                            />
                        </div>
                    </div>
                </div>
            </div>

            <div className="card mt-4">
                <div className="card-header">
                    <h3 className="card-title">AI Integration</h3>
                </div>
                <div className="card-body">
                    <div className="setting-item">
                        <label className="setting-label">AI Provider</label>
                        <select className="input select" value={config.aiProvider} disabled>
                            <option value="none">None</option>
                            <option value="gemini">Google Gemini</option>
                            <option value="openai">OpenAI</option>
                            <option value="claude">Anthropic Claude</option>
                            <option value="ollama">Ollama (Local)</option>
                        </select>
                    </div>
                </div>
            </div>

            <div className="mt-4" style={{ padding: '1rem', background: 'var(--bg-tertiary)', borderRadius: '8px' }}>
                <p className="text-sm text-secondary">
                    <strong>Note:</strong> Settings are currently read-only and configured via environment variables.
                    See <code>.env</code> file to modify these values.
                </p>
            </div>
        </div>
    );
}
