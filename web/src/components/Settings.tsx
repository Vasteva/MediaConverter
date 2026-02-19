import { useState, useEffect, useRef } from 'react';
import type { SystemConfig } from '../types';

interface SettingsProps {
    config: SystemConfig | null;
    onConfigUpdate: (newConfig: Partial<SystemConfig>) => Promise<boolean>;
    token: string | null;
}

export default function Settings({ config: initialConfig, onConfigUpdate, token }: SettingsProps) {
    const [config, setConfig] = useState<SystemConfig | null>(initialConfig);
    const [isSaving, setIsSaving] = useState(false);
    const [testStatus, setTestStatus] = useState<'idle' | 'testing' | 'success' | 'error'>('idle');
    const [testMessage, setTestMessage] = useState('');
    const [saveError, setSaveError] = useState<string | null>(null);

    const testStatusRef = useRef(testStatus);
    useEffect(() => {
        testStatusRef.current = testStatus;
    }, [testStatus]);

    useEffect(() => {
        setConfig(initialConfig);
    }, [initialConfig]);

    if (!config) {
        return <div className="p-8 text-center">Loading settings...</div>;
    }

    const handleSave = async (updates: Partial<SystemConfig>) => {
        setIsSaving(true);
        setSaveError(null);
        const ok = await onConfigUpdate(updates);
        if (!ok) {
            setSaveError('Failed to save settings. Please try again.');
        }
        setIsSaving(false);
    };

    const handleTestConnection = async () => {
        if (!config) return;
        setTestStatus('testing');
        setTestMessage('');

        try {
            const res = await fetch('/api/ai/test', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'Authorization': `Bearer ${token}`
                },
                body: JSON.stringify({
                    provider: config.aiProvider,
                    apiKey: config.aiApiKey,
                    endpoint: config.aiEndpoint,
                    model: config.aiModel
                })
            });

            const data = await res.json();

            if (res.ok) {
                setTestStatus('success');
                setTestMessage(data.message || 'Connection successful');
            } else {
                setTestStatus('error');
                setTestMessage(data.error || 'Connection failed');
            }
        } catch {
            setTestStatus('error');
            setTestMessage('Network error');
        }

        // Reset status after 5 seconds
        setTimeout(() => {
            if (testStatusRef.current !== 'testing') {
                setTestStatus('idle');
                setTestMessage('');
            }
        }, 5000);
    };

    const isPremium = config.isPremium;

    return (
        <div className="settings-view">
            <div className="view-header flex justify-between items-center">
                <div>
                    <h1>Settings</h1>
                    <p className="text-secondary">Configure system settings and preferences</p>
                </div>
                <div className={`badge ${isPremium ? 'badge-premium' : 'badge-standard'}`}>
                    {config.planName || (isPremium ? 'Pro' : 'Standard')}
                </div>
            </div>

            {saveError && (
                <div className="alert alert-error mt-4">
                    <p>{saveError}</p>
                </div>
            )}

            <div className="grid grid-2 mt-4">
                {/* Encoding Settings */}
                <div className="card">
                    <div className="card-header">
                        <h3 className="card-title">Encoding Settings</h3>
                    </div>
                    <div className="card-body">
                        <div className="flex flex-col gap-4">
                            <div className="setting-item">
                                <label className="setting-label">GPU Vendor</label>
                                <select
                                    className="input select"
                                    value={config.gpuVendor}
                                    disabled
                                >
                                    <option value="cpu">CPU</option>
                                    <option value="nvidia">NVIDIA</option>
                                    <option value="intel">Intel</option>
                                    <option value="amd">AMD</option>
                                </select>
                            </div>

                            <div className="setting-item">
                                <label className="setting-label flex justify-between">
                                    Adaptive Encoding
                                    {!isPremium && <span className="pro-tag">PRO</span>}
                                </label>
                                <select
                                    className="input select"
                                    disabled={!isPremium || isSaving}
                                    value={isPremium ? "enabled" : "disabled"}
                                >
                                    <option value="disabled">Disabled (Fixed CRF)</option>
                                    <option value="enabled">Enabled (AI Analysis)</option>
                                </select>
                            </div>

                            <div className="setting-item">
                                <label className="setting-label">Quality Preset</label>
                                <select
                                    className="input select"
                                    value={config.qualityPreset}
                                    onChange={(e) => handleSave({ qualityPreset: e.target.value })}
                                    disabled={isSaving}
                                >
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
                                    onChange={(e) => setConfig({ ...config, crf: parseInt(e.target.value) })}
                                    onBlur={(e) => handleSave({ crf: parseInt(e.target.value) })}
                                    disabled={isSaving}
                                    min="0"
                                    max="51"
                                />
                                <span className="text-xs text-secondary mt-1">Lower = better quality (0-51)</span>
                            </div>
                        </div>
                    </div>
                </div>

                {/* License Section */}
                <div className="card">
                    <div className="card-header">
                        <h3 className="card-title">License & Subscription</h3>
                    </div>
                    <div className="card-body">
                        <div className="flex flex-col gap-4">
                            <div className="setting-item">
                                <label className="setting-label">License Key</label>
                                <input
                                    type="password"
                                    className="input"
                                    value={config.licenseKey || ''}
                                    placeholder="VASTIVA-PRO-XXXX-XXXX"
                                    onChange={(e) => setConfig({ ...config, licenseKey: e.target.value })}
                                    onBlur={(e) => handleSave({ licenseKey: e.target.value })}
                                    disabled={isSaving}
                                />
                                <p className="text-xs text-secondary mt-2">
                                    {isPremium
                                        ? "✅ Your Pro license is active. Thank you for supporting Vastiva!"
                                        : "Enter your Pro license key to unlock AI-Adaptive Encoding, Whisper Subtitles, and Smart Metadata."}
                                </p>
                            </div>

                            {!isPremium && (
                                <button className="btn btn-primary w-full mt-2">
                                    Upgrade to Pro
                                </button>
                            )}
                        </div>
                    </div>
                </div>
            </div>

            {/* AI Integration */}
            <div className="card mt-4">
                <div className="card-header">
                    <h3 className="card-title">AI Integration</h3>
                </div>
                <div className="card-body">
                    <div className="grid grid-2 gap-4">
                        <div className="setting-item">
                            <label className="setting-label">AI Provider</label>
                            <select
                                className="input select"
                                value={config.aiProvider}
                                onChange={(e) => handleSave({ aiProvider: e.target.value })}
                                disabled={isSaving}
                            >
                                <option value="none">None</option>
                                <option value="gemini">Google Gemini (AI Studio)</option>
                                <option value="openai">OpenAI / Compatible</option>
                                <option value="claude">Anthropic Claude</option>
                                <option value="ollama">Ollama (Local LLM)</option>
                            </select>
                        </div>

                        {config.aiProvider !== 'none' && (
                            <>
                                {config.aiProvider !== 'ollama' && (
                                    <div className="setting-item">
                                        <label className="setting-label">API Key</label>
                                        <input
                                            type="password"
                                            className="input"
                                            value={config.aiApiKey || ''}
                                            placeholder="Enter API Key..."
                                            onChange={(e) => setConfig({ ...config, aiApiKey: e.target.value })}
                                            onBlur={(e) => handleSave({ aiApiKey: e.target.value })}
                                            disabled={isSaving}
                                        />
                                    </div>
                                )}

                                <div className="setting-item">
                                    <label className="setting-label">
                                        {config.aiProvider === 'ollama' ? 'Ollama Endpoint' : 'Custom Endpoint (Optional)'}
                                    </label>
                                    <input
                                        type="text"
                                        className="input"
                                        value={config.aiEndpoint || ''}
                                        placeholder={config.aiProvider === 'ollama' ? 'http://localhost:11434' : 'https://api...'}
                                        onChange={(e) => setConfig({ ...config, aiEndpoint: e.target.value })}
                                        onBlur={(e) => handleSave({ aiEndpoint: e.target.value })}
                                        disabled={isSaving}
                                    />
                                </div>

                                <div className="setting-item">
                                    <label className="setting-label">Model Name</label>
                                    <input
                                        type="text"
                                        className="input"
                                        value={config.aiModel || ''}
                                        placeholder="e.g. gemini-1.5-flash, llama3, gpt-4o"
                                        onChange={(e) => setConfig({ ...config, aiModel: e.target.value })}
                                        onBlur={(e) => handleSave({ aiModel: e.target.value })}
                                        disabled={isSaving}
                                    />
                                </div>

                                <div className="mt-4 flex items-center gap-3">
                                    <button
                                        className={`btn ${testStatus === 'success' ? 'btn-success' : testStatus === 'error' ? 'btn-error' : testStatus === 'testing' ? 'btn-secondary' : 'btn-secondary'}`}
                                        onClick={handleTestConnection}
                                        disabled={testStatus === 'testing' || !config.aiProvider}
                                    >
                                        {testStatus === 'testing' ? 'Testing...' : 'Test Connection'}
                                    </button>
                                    {testMessage && (
                                        <span className={`text-sm ${testStatus === 'success' ? 'text-green-400' : testStatus === 'error' ? 'text-red-400' : 'text-secondary'}`}>
                                            {testMessage}
                                        </span>
                                    )}
                                </div>
                            </>
                        )}
                    </div>

                    <div className="divider mt-8 mb-4"></div>

                    <div className="grid grid-2 gap-4">
                        {/* Safety & Verification */}
                        <div className="setting-item">
                            <label className="setting-label flex justify-between">
                                Delete Original File
                                <span className="text-red-400 text-xs uppercase font-bold">Caution</span>
                            </label>
                            <label className="flex items-center gap-2 cursor-pointer mt-2">
                                <input
                                    type="checkbox"
                                    className="checkbox checkbox-error"
                                    checked={config.deleteSource || false}
                                    onChange={(e) => handleSave({ deleteSource: e.target.checked })}
                                    disabled={isSaving}
                                />
                                <span className="text-sm">Permanently delete source file after successful conversion</span>
                            </label>
                        </div>

                        <div className="setting-item">
                            <label className="setting-label flex justify-between">
                                AI Smart Verification
                                {!isPremium && <span className="pro-tag">PRO</span>}
                            </label>
                            <label className="flex items-center gap-2 cursor-pointer mt-2">
                                <input
                                    type="checkbox"
                                    className="checkbox checkbox-success"
                                    checked={config.verifyOutput || false}
                                    onChange={(e) => handleSave({ verifyOutput: e.target.checked })}
                                    disabled={!isPremium || isSaving}
                                />
                                <span className="text-sm">
                                    {isPremium
                                        ? "Verify video integrity with AI before completing (Required for Safe Delete)"
                                        : "Upgrade to Pro to enable AI integrity checks"}
                                </span>
                            </label>
                            {config.deleteSource && !config.verifyOutput && (
                                <p className="text-xs text-orange-400 mt-1">
                                    ⚠️ Warning: Deleting files without AI verification is risky.
                                </p>
                            )}
                        </div>
                    </div>

                    <div className="mt-8 p-4 bg-tertiary rounded-lg border border-border">
                        <p className="text-sm text-secondary">
                            <strong>Pro Tip:</strong> Using a local model with <strong>Ollama</strong> or a fast cloud model like <strong>Gemini Flash</strong> is recommended for real-time media analysis.
                        </p>
                    </div>
                </div>
            </div>
        </div>
    );
}
