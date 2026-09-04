import { useState, useEffect, useRef, Fragment } from 'react';
import type { SystemConfig, ProcessingSchedule } from '../types';

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
    const [subtitlePassword, setSubtitlePassword] = useState('');

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

    const defaultSchedule: ProcessingSchedule = { enabled: false, startHour: 22, endHour: 6, allowedDays: [], timezone: 'UTC' };

    // `config.schedule ?? defaultSchedule` is not enough on its own. The API
    // returns a schedule *object* whose fields may be missing or null — a Go nil
    // slice serialises to null — so the ?? never fires and allowedDays arrives as
    // null. Calling .includes() on it threw and unmounted the page.
    //
    // The server now guarantees an array, but a config persisted before that fix
    // can still be loaded, so each field is defaulted individually here too.
    const raw = config.schedule ?? defaultSchedule;
    const schedule: ProcessingSchedule = {
        ...defaultSchedule,
        ...raw,
        allowedDays: raw.allowedDays ?? [],
    };
    const setSchedule = (s: ProcessingSchedule) => setConfig({ ...config, schedule: s });

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
                                    value={config.overrideAICRF ? "disabled" : "enabled"}
                                    onChange={(e) => handleSave({ overrideAICRF: e.target.value === "disabled" })}
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
                                        : "Enter your Pro license key to unlock AI-Adaptive Encoding and Smart Metadata."}
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

            {/* Processing Schedule */}
            <div className="card mt-4">
                <div className="card-header">
                    <h3 className="card-title">Processing Schedule</h3>
                </div>
                <div className="card-body">
                    <div className="flex flex-col gap-4">
                        <div className="setting-item">
                            <label className="setting-label">Schedule</label>
                            <label className="flex items-center gap-2 cursor-pointer mt-2">
                                <input
                                    type="checkbox"
                                    className="checkbox"
                                    checked={schedule.enabled}
                                    onChange={e => setSchedule({ ...schedule, enabled: e.target.checked })}
                                    disabled={isSaving}
                                />
                                <span className="text-sm">Restrict job processing to specific hours/days</span>
                            </label>
                            <p className="text-xs text-secondary mt-1">
                                When enabled, queued jobs will only be dequeued during the configured window.
                            </p>
                        </div>

                        {schedule.enabled && (
                            <div className="flex flex-col gap-4">
                                <div className="setting-item">
                                    <label className="setting-label">Allowed Days</label>
                                    <div className="flex gap-2 mt-2 flex-wrap">
                                        {(['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'] as const).map((day, idx) => (
                                            <label key={day} className="flex items-center gap-1 cursor-pointer">
                                                <input
                                                    type="checkbox"
                                                    checked={schedule.allowedDays.includes(idx)}
                                                    onChange={e => {
                                                        const days = e.target.checked
                                                            ? [...schedule.allowedDays, idx].sort()
                                                            : schedule.allowedDays.filter(d => d !== idx);
                                                        setSchedule({ ...schedule, allowedDays: days });
                                                    }}
                                                    disabled={isSaving}
                                                />
                                                <span className="text-sm">{day}</span>
                                            </label>
                                        ))}
                                    </div>
                                    <p className="text-xs text-secondary mt-1">Leave all unchecked to allow any day.</p>
                                </div>

                                <div className="grid grid-2 gap-4">
                                    <div className="setting-item">
                                        <label className="setting-label">Start Hour</label>
                                        <select
                                            className="input select"
                                            value={schedule.startHour}
                                            onChange={e => setSchedule({ ...schedule, startHour: Number(e.target.value) })}
                                            disabled={isSaving}
                                        >
                                            {Array.from({ length: 24 }, (_, h) => (
                                                <option key={h} value={h}>
                                                    {new Date(0, 0, 0, h).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit', hour12: true })}
                                                </option>
                                            ))}
                                        </select>
                                    </div>

                                    <div className="setting-item">
                                        <label className="setting-label">End Hour</label>
                                        <select
                                            className="input select"
                                            value={schedule.endHour}
                                            onChange={e => setSchedule({ ...schedule, endHour: Number(e.target.value) })}
                                            disabled={isSaving}
                                        >
                                            {Array.from({ length: 24 }, (_, h) => (
                                                <option key={h} value={h}>
                                                    {new Date(0, 0, 0, h).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit', hour12: true })}
                                                </option>
                                            ))}
                                        </select>
                                    </div>
                                </div>

                                <div className="setting-item">
                                    <label className="setting-label">Timezone</label>
                                    <input
                                        type="text"
                                        className="input"
                                        value={schedule.timezone}
                                        onChange={e => setSchedule({ ...schedule, timezone: e.target.value })}
                                        disabled={isSaving}
                                        placeholder="UTC"
                                    />
                                    <span className="text-xs text-secondary mt-1">IANA timezone name, e.g. America/New_York, Europe/London</span>
                                </div>

                                {/* Summary preview */}
                                <div className="p-3 bg-tertiary rounded-lg border border-border text-sm text-secondary">
                                    {(() => {
                                        const dayNames = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
                                        const dayStr = schedule.allowedDays.length > 0
                                            ? schedule.allowedDays.map(d => dayNames[d]).join(', ')
                                            : 'Every day';
                                        const fmt = (h: number) => new Date(0, 0, 0, h).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit', hour12: true });
                                        return `Jobs will process on ${dayStr}, ${fmt(schedule.startHour)} – ${fmt(schedule.endHour)} (${schedule.timezone || 'UTC'})`;
                                    })()}
                                </div>
                            </div>
                        )}

                        <button
                            className="btn btn-primary"
                            onClick={() => handleSave({ schedule })}
                            disabled={isSaving}
                        >
                            {isSaving ? 'Saving...' : 'Save Schedule'}
                        </button>
                    </div>
                </div>
            </div>

            {/* Subtitles */}
            <div className="card mt-4">
                <div className="card-header">
                    <h3 className="card-title">Subtitles</h3>
                </div>
                <div className="card-body">
                    <div className="grid grid-2 gap-4">
                        <div className="setting-item">
                            <label className="setting-label">Download Mode</label>
                            <select
                                className="input select"
                                value={config.subtitleMode || 'selective'}
                                onChange={e => handleSave({ subtitleMode: e.target.value as SystemConfig['subtitleMode'] })}
                                disabled={isSaving}
                            >
                                <option value="always">Always — download for every job</option>
                                <option value="selective">Per Job — choose per conversion</option>
                                <option value="never">Disabled — no subtitle downloads</option>
                            </select>
                        </div>

                        <div className="setting-item">
                            <label className="setting-label">Preferred Language</label>
                            <input
                                type="text"
                                className="input"
                                value={config.subtitleLang || 'en'}
                                onChange={e => setConfig({ ...config, subtitleLang: e.target.value })}
                                onBlur={e => handleSave({ subtitleLang: e.target.value })}
                                disabled={isSaving}
                                placeholder="en"
                                maxLength={10}
                            />
                            <span className="text-xs text-secondary mt-1">ISO 639-1 — e.g. en, fr, de, es, ja</span>
                        </div>

                        {config.subtitleMode !== 'never' && (
                            <Fragment>
                                <div className="setting-item" style={{ gridColumn: '1 / -1' }}>
                                    <div className="p-3 bg-tertiary rounded-lg border border-border text-sm text-secondary">
                                        <strong className="text-primary block mb-1">How to get an OpenSubtitles API key:</strong>
                                        <ol style={{ paddingLeft: '1.2rem', lineHeight: '1.8' }}>
                                            <li>Create a free account at <strong>opensubtitles.com</strong></li>
                                            <li>Go to your profile &rarr; <strong>API Consumers</strong></li>
                                            <li>Click <strong>Add new consumer</strong>, give it a name, and save</li>
                                            <li>Copy the generated API key below</li>
                                        </ol>
                                        <span className="text-xs">Free accounts: 20 downloads/day. Upgrade to VIP for more.</span>
                                    </div>
                                </div>

                                <div className="setting-item">
                                    <label className="setting-label">OpenSubtitles API Key</label>
                                    <input
                                        type="password"
                                        className="input"
                                        value={config.subtitleApiKey || ''}
                                        placeholder="Paste your API key"
                                        onChange={e => setConfig({ ...config, subtitleApiKey: e.target.value })}
                                        onBlur={e => handleSave({ subtitleApiKey: e.target.value })}
                                        disabled={isSaving}
                                    />
                                </div>

                                <div className="setting-item">
                                    <label className="setting-label">Username <span className="text-xs text-secondary">(optional)</span></label>
                                    <input
                                        type="text"
                                        className="input"
                                        value={config.subtitleUsername || ''}
                                        placeholder="opensubtitles.com username"
                                        onChange={e => setConfig({ ...config, subtitleUsername: e.target.value })}
                                        onBlur={e => handleSave({ subtitleUsername: e.target.value })}
                                        disabled={isSaving}
                                        autoComplete="off"
                                    />
                                </div>

                                <div className="setting-item">
                                    <label className="setting-label">Password <span className="text-xs text-secondary">(optional)</span></label>
                                    <input
                                        type="password"
                                        className="input"
                                        value={subtitlePassword}
                                        placeholder={config.subtitlePasswordSet && !subtitlePassword ? "Password saved — enter new value to change" : "opensubtitles.com password"}
                                        onChange={e => setSubtitlePassword(e.target.value)}
                                        onBlur={e => { if (e.target.value) handleSave({ subtitlePassword: e.target.value }); }}
                                        disabled={isSaving}
                                        autoComplete="new-password"
                                    />
                                    <span className="text-xs text-secondary mt-1">Logging in increases your daily download quota.</span>
                                </div>
                            </Fragment>
                        )}
                    </div>
                </div>
            </div>
        </div>
    );
}
