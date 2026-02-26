import React, { useState, useEffect } from 'react';
import './SetupWizard.css';

interface SetupWizardProps {
    onComplete: (token: string) => void;
}

interface ProbesState {
    ffmpeg: boolean;
    makemkv: boolean;
    gpu: string | null;
}

type QualityPreset = 'fast' | 'medium' | 'slow';
type SubtitleMode = 'always' | 'selective' | 'never';

const QUALITY_PRESETS: QualityPreset[] = ['fast', 'medium', 'slow'];
const QUALITY_PRESET_LABELS: Record<QualityPreset, string> = {
    fast: 'Faster, larger files',
    medium: 'Balanced',
    slow: 'Slower, smaller files',
};

const SetupWizard: React.FC<SetupWizardProps> = ({ onComplete }) => {
    const [step, setStep] = useState(0);
    const [probes, setProbes] = useState<ProbesState | null>(null);
    const [probeError, setProbeError] = useState<string | null>(null);
    const [formData, setFormData] = useState({
        adminPassword: '',
        confirmPassword: '',
        aiProvider: 'none',
        aiApiKey: '',
        aiEndpoint: '',
        aiModel: '',
        licenseKey: '',
        subtitleMode: 'selective' as SubtitleMode,
        subtitleLang: 'en',
        subtitleApiKey: '',
        subtitleUsername: '',
        subtitlePassword: '',
        gpuVendor: '',
        qualityPreset: 'medium' as QualityPreset,
        crf: 23,
    });
    const [aiTestStatus, setAiTestStatus] = useState<{ loading: boolean; success?: boolean; message?: string } | null>(null);
    const [submitError, setSubmitError] = useState<string | null>(null);
    const [isSubmitting, setIsSubmitting] = useState(false);

    // 8 steps total: 0=Welcome, 1=Check, 2=Security, 3=AI, 4=Subtitles, 5=Encoding, 6=Review, 7=Launching
    const VISIBLE_STEP_COUNT = 7; // dots shown for steps 0–6; step 7 is the submitting spinner

    useEffect(() => {
        if (step === 1) {
            setProbes(null);
            setProbeError(null);
            fetchProbes();
        }
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [step]);

    const fetchProbes = async () => {
        try {
            const res = await fetch('/api/setup/probes');
            if (!res.ok) throw new Error(`Server error: ${res.status}`);
            const data = await res.json();
            setProbes(data);
            if (data.gpu) {
                setFormData(prev => ({ ...prev, gpuVendor: data.gpu }));
            }
        } catch (e) {
            setProbeError(e instanceof Error ? e.message : 'Failed to run system check');
        }
    };

    const handleNext = () => setStep(s => s + 1);
    const handleBack = () => setStep(s => s - 1);

    const handleTestAI = async () => {
        setAiTestStatus({ loading: true });
        try {
            const res = await fetch('/api/setup/test-ai', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    provider: formData.aiProvider,
                    apiKey: formData.aiApiKey,
                    endpoint: formData.aiEndpoint,
                    model: formData.aiModel,
                }),
            });
            const data = await res.json();
            if (res.ok && data.success) {
                setAiTestStatus({ loading: false, success: true, message: data.message });
            } else {
                setAiTestStatus({ loading: false, success: false, message: data.error || 'Connection failed' });
            }
        } catch (e) {
            setAiTestStatus({ loading: false, success: false, message: e instanceof Error ? e.message : 'Connection failed' });
        }
    };

    const handleSubmit = async () => {
        setIsSubmitting(true);
        setSubmitError(null);
        setStep(7);
        try {
            const res = await fetch('/api/setup/complete', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    adminPassword: formData.adminPassword,
                    aiProvider: formData.aiProvider,
                    aiApiKey: formData.aiApiKey,
                    aiEndpoint: formData.aiEndpoint,
                    aiModel: formData.aiModel,
                    licenseKey: formData.licenseKey,
                    subtitleMode: formData.subtitleMode,
                    subtitleLang: formData.subtitleLang,
                    subtitleApiKey: formData.subtitleApiKey,
                    subtitleUsername: formData.subtitleUsername,
                    subtitlePassword: formData.subtitlePassword,
                    gpuVendor: formData.gpuVendor,
                    qualityPreset: formData.qualityPreset,
                    crf: formData.crf,
                }),
            });
            if (!res.ok) {
                const data = await res.json();
                throw new Error(data.error || 'Setup failed');
            }

            // Auto-login with the configured password
            const loginRes = await fetch('/api/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ password: formData.adminPassword }),
            });
            if (!loginRes.ok) {
                throw new Error('Setup complete but login failed. Please refresh and log in manually.');
            }
            const loginData = await loginRes.json();
            onComplete(loginData.token);
        } catch (e) {
            setSubmitError(e instanceof Error ? e.message : 'An unexpected error occurred');
            setIsSubmitting(false);
            setStep(6);
        }
    };

    const steps = [
        { title: 'Welcome',        subtitle: "Let's get your Vastiva Media Converter ready." },
        { title: 'System Check',   subtitle: 'Checking for required tools and hardware.' },
        { title: 'Security',       subtitle: 'Secure your admin account.' },
        { title: 'AI Integration', subtitle: 'Enhance your media with AI power.' },
        { title: 'Subtitles',      subtitle: 'Configure automatic subtitle downloading.' },
        { title: 'Encoding',       subtitle: 'Configure hardware acceleration and quality.' },
        { title: 'Review',         subtitle: 'Confirm your settings before launching.' },
        { title: 'Launching\u2026', subtitle: 'Setting up your media converter.' },
    ];

    const passwordValid = formData.adminPassword.length >= 8;
    const passwordsMatch = formData.adminPassword === formData.confirmPassword;
    const securityNextEnabled = passwordValid && passwordsMatch;

    const isNextDisabled =
        (step === 1 && probes === null && probeError === null) ||
        (step === 2 && !securityNextEnabled);

    const subtitleNeedsKey = formData.subtitleMode !== 'never';

    return (
        <div className="setup-wizard-overlay shadow-xl">
            <div className="setup-wizard-card glass animate-in fade-in zoom-in duration-300">
                <div className="setup-wizard-content">
                    <div className="setup-step-indicator">
                        {Array.from({ length: VISIBLE_STEP_COUNT }).map((_, i) => (
                            <div
                                key={i}
                                className={`step-dot ${i === step && step < VISIBLE_STEP_COUNT ? 'active' : ''} ${i < step ? 'completed' : ''}`}
                            />
                        ))}
                    </div>

                    <div className="setup-header">
                        <h2 className="title-gradient font-bold">{steps[step].title}</h2>
                        <p className="text-secondary">{steps[step].subtitle}</p>
                    </div>

                    <div className="setup-form">
                        {/* Step 0: Welcome */}
                        {step === 0 && (
                            <div className="welcome-screen fade-in">
                                <div className="welcome-icon text-teal-400">
                                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="w-20 h-20">
                                        <path d="M5 3l14 9-14 9V3z" />
                                    </svg>
                                </div>
                                <p className="text-lg">Experience high-performance media conversion with AI-powered enhancements.</p>
                            </div>
                        )}

                        {/* Step 1: System Check */}
                        {step === 1 && (
                            <div className="fade-in space-y-4">
                                {probeError ? (
                                    <div className="setup-error">
                                        <span>{probeError}</span>
                                        <button className="btn btn-secondary btn-sm" onClick={fetchProbes}>Retry</button>
                                    </div>
                                ) : probes ? (
                                    <>
                                        <div className="probe-item">
                                            <span>FFmpeg (Transcoding)</span>
                                            <span className={`probe-status ${probes.ffmpeg ? 'status-ok' : 'status-warn'}`}>
                                                {probes.ffmpeg ? 'Detected' : 'Not Found'}
                                            </span>
                                        </div>
                                        <div className="probe-item">
                                            <span>MakeMKV (Extraction)</span>
                                            <span className={`probe-status ${probes.makemkv ? 'status-ok' : 'status-warn'}`}>
                                                {probes.makemkv ? 'Detected' : 'Optional'}
                                            </span>
                                        </div>
                                        <div className="probe-item">
                                            <span>Graphics Accelerator</span>
                                            <span className="probe-status status-ok">{probes.gpu?.toUpperCase() || 'CPU'}</span>
                                        </div>
                                    </>
                                ) : (
                                    <div className="flex justify-center p-12">
                                        <div className="spinner-lg"></div>
                                    </div>
                                )}
                            </div>
                        )}

                        {/* Step 2: Security */}
                        {step === 2 && (
                            <div className="fade-in space-y-6">
                                <div className="form-group">
                                    <label className="block mb-2 font-medium">Admin Password</label>
                                    <input
                                        type="password"
                                        className="input w-full"
                                        value={formData.adminPassword}
                                        onChange={e => setFormData({ ...formData, adminPassword: e.target.value })}
                                        placeholder="Enter a strong password (min. 8 characters)"
                                        autoFocus
                                    />
                                    {formData.adminPassword.length > 0 && !passwordValid && (
                                        <small className="setup-field-error">Password must be at least 8 characters.</small>
                                    )}
                                </div>
                                <div className="form-group">
                                    <label className="block mb-2 font-medium">Confirm Password</label>
                                    <input
                                        type="password"
                                        className="input w-full"
                                        value={formData.confirmPassword}
                                        onChange={e => setFormData({ ...formData, confirmPassword: e.target.value })}
                                        placeholder="Re-enter your password"
                                    />
                                    {formData.confirmPassword.length > 0 && !passwordsMatch && (
                                        <small className="setup-field-error">Passwords do not match.</small>
                                    )}
                                </div>
                                <div className="form-group">
                                    <label className="block mb-2 font-medium">
                                        License Key <span className="text-secondary font-normal">(Optional)</span>
                                    </label>
                                    <input
                                        type="text"
                                        className="input w-full font-mono"
                                        value={formData.licenseKey}
                                        onChange={e => setFormData({ ...formData, licenseKey: e.target.value })}
                                        placeholder="VASTIVA-PRO-..."
                                    />
                                    <small className="text-secondary block mt-1">Unlock AI Search and Advanced Automation.</small>
                                </div>
                            </div>
                        )}

                        {/* Step 3: AI Integration */}
                        {step === 3 && (
                            <div className="fade-in">
                                <div className="ai-provider-grid">
                                    {['gemini', 'openai', 'ollama', 'none'].map(p => (
                                        <div
                                            key={p}
                                            className={`ai-card ${formData.aiProvider === p ? 'active' : ''}`}
                                            onClick={() => {
                                                setFormData({ ...formData, aiProvider: p });
                                                setAiTestStatus(null);
                                            }}
                                        >
                                            <span className="icon text-2xl block mb-2">{p === 'none' ? '🚫' : '✨'}</span>
                                            <span className="name font-bold">{p.toUpperCase()}</span>
                                        </div>
                                    ))}
                                </div>

                                {formData.aiProvider !== 'none' && (
                                    <div className="space-y-4 mt-6">
                                        {formData.aiProvider !== 'ollama' && (
                                            <div className="form-group">
                                                <label className="block mb-2 font-medium">API Key</label>
                                                <input
                                                    type="password"
                                                    className="input w-full"
                                                    value={formData.aiApiKey}
                                                    onChange={e => {
                                                        setFormData({ ...formData, aiApiKey: e.target.value });
                                                        setAiTestStatus(null);
                                                    }}
                                                    placeholder={`Enter your ${formData.aiProvider} API key`}
                                                />
                                            </div>
                                        )}
                                        {formData.aiProvider === 'ollama' && (
                                            <div className="form-group">
                                                <label className="block mb-2 font-medium">Ollama Endpoint</label>
                                                <input
                                                    type="text"
                                                    className="input w-full"
                                                    value={formData.aiEndpoint}
                                                    onChange={e => {
                                                        setFormData({ ...formData, aiEndpoint: e.target.value });
                                                        setAiTestStatus(null);
                                                    }}
                                                    placeholder="http://localhost:11434"
                                                />
                                            </div>
                                        )}
                                        <div className="form-group">
                                            <label className="block mb-2 font-medium">
                                                Model <span className="text-secondary font-normal">(Optional)</span>
                                            </label>
                                            <input
                                                type="text"
                                                className="input w-full"
                                                value={formData.aiModel}
                                                onChange={e => {
                                                    setFormData({ ...formData, aiModel: e.target.value });
                                                    setAiTestStatus(null);
                                                }}
                                                placeholder="Leave blank for default"
                                            />
                                        </div>
                                        <div className="ai-test-row">
                                            <button
                                                className="btn btn-secondary test-btn"
                                                onClick={handleTestAI}
                                                disabled={aiTestStatus?.loading}
                                            >
                                                {aiTestStatus?.loading ? 'Testing\u2026' : 'Test Connection'}
                                            </button>
                                            {aiTestStatus && !aiTestStatus.loading && (
                                                <span className={aiTestStatus.success ? 'setup-status-ok' : 'setup-status-err'}>
                                                    {aiTestStatus.success ? '✓ ' : '✗ '}{aiTestStatus.message}
                                                </span>
                                            )}
                                        </div>
                                    </div>
                                )}
                            </div>
                        )}

                        {/* Step 4: Subtitles */}
                        {step === 4 && (
                            <div className="fade-in space-y-5">
                                <div className="form-group">
                                    <label className="block mb-2 font-medium">Download Mode</label>
                                    <div className="encoding-grid">
                                        {([
                                            { value: 'always',    label: 'Always',   desc: 'Every job automatically' },
                                            { value: 'selective', label: 'Per Job',  desc: 'Choose per conversion' },
                                            { value: 'never',     label: 'Disabled', desc: 'No subtitle downloads' },
                                        ] as { value: SubtitleMode; label: string; desc: string }[]).map(opt => (
                                            <div
                                                key={opt.value}
                                                className={`ai-card ${formData.subtitleMode === opt.value ? 'active' : ''}`}
                                                onClick={() => setFormData({ ...formData, subtitleMode: opt.value })}
                                            >
                                                <span className="name font-bold">{opt.label}</span>
                                                <small className="text-secondary">{opt.desc}</small>
                                            </div>
                                        ))}
                                    </div>
                                </div>

                                {subtitleNeedsKey && (
                                    <>
                                        <div className="subtitle-setup-info">
                                            <strong>How to get an OpenSubtitles API key:</strong>
                                            <ol className="subtitle-setup-steps">
                                                <li>Create a free account at <strong>opensubtitles.com</strong></li>
                                                <li>Go to your profile and open <strong>API Consumers</strong></li>
                                                <li>Click <strong>Add new consumer</strong>, give it a name, and save</li>
                                                <li>Copy the generated <strong>API Key</strong> below</li>
                                            </ol>
                                            <small className="text-secondary">Free accounts: 20 downloads/day. Upgrade to VIP for more.</small>
                                        </div>

                                        <div className="form-group">
                                            <label className="block mb-2 font-medium">OpenSubtitles API Key</label>
                                            <input
                                                type="password"
                                                className="input w-full"
                                                value={formData.subtitleApiKey}
                                                onChange={e => setFormData({ ...formData, subtitleApiKey: e.target.value })}
                                                placeholder="Paste your API key here"
                                            />
                                        </div>

                                        <div className="form-group">
                                            <label className="block mb-2 font-medium">Preferred Language</label>
                                            <input
                                                type="text"
                                                className="input w-full"
                                                value={formData.subtitleLang}
                                                onChange={e => setFormData({ ...formData, subtitleLang: e.target.value })}
                                                placeholder="en"
                                                maxLength={10}
                                            />
                                            <small className="text-secondary block mt-1">ISO 639-1 code — e.g. en, fr, de, es, ja</small>
                                        </div>

                                        <details className="subtitle-credentials-details">
                                            <summary className="text-secondary text-sm cursor-pointer">
                                                Account credentials <span className="text-xs">(optional — increases daily download quota)</span>
                                            </summary>
                                            <div className="space-y-4 mt-3">
                                                <div className="form-group">
                                                    <label className="block mb-2 font-medium">Username</label>
                                                    <input
                                                        type="text"
                                                        className="input w-full"
                                                        value={formData.subtitleUsername}
                                                        onChange={e => setFormData({ ...formData, subtitleUsername: e.target.value })}
                                                        placeholder="Your opensubtitles.com username"
                                                        autoComplete="off"
                                                    />
                                                </div>
                                                <div className="form-group">
                                                    <label className="block mb-2 font-medium">Password</label>
                                                    <input
                                                        type="password"
                                                        className="input w-full"
                                                        value={formData.subtitlePassword}
                                                        onChange={e => setFormData({ ...formData, subtitlePassword: e.target.value })}
                                                        placeholder="Your opensubtitles.com password"
                                                        autoComplete="new-password"
                                                    />
                                                </div>
                                            </div>
                                        </details>
                                    </>
                                )}
                            </div>
                        )}

                        {/* Step 5: Encoding */}
                        {step === 5 && (
                            <div className="fade-in space-y-6">
                                <div className="form-group">
                                    <label className="block mb-2 font-medium">GPU Vendor</label>
                                    <div className="ai-provider-grid">
                                        {['nvidia', 'amd', 'intel', 'none'].map(gpu => (
                                            <div
                                                key={gpu}
                                                className={`ai-card ${formData.gpuVendor === gpu ? 'active' : ''}`}
                                                onClick={() => setFormData({ ...formData, gpuVendor: gpu })}
                                            >
                                                <span className="name font-bold">{gpu === 'none' ? 'CPU' : gpu.toUpperCase()}</span>
                                            </div>
                                        ))}
                                    </div>
                                    <small className="text-secondary block mt-2">Pre-filled from system detection. Change if incorrect.</small>
                                </div>
                                <div className="form-group">
                                    <label className="block mb-2 font-medium">Quality Preset</label>
                                    <div className="encoding-grid">
                                        {QUALITY_PRESETS.map(preset => (
                                            <div
                                                key={preset}
                                                className={`ai-card ${formData.qualityPreset === preset ? 'active' : ''}`}
                                                onClick={() => setFormData({ ...formData, qualityPreset: preset })}
                                            >
                                                <span className="name font-bold">{preset.charAt(0).toUpperCase() + preset.slice(1)}</span>
                                                <small className="text-secondary">{QUALITY_PRESET_LABELS[preset]}</small>
                                            </div>
                                        ))}
                                    </div>
                                </div>
                                <div className="form-group">
                                    <label className="block mb-2 font-medium">
                                        CRF (Quality Factor) — <strong>{formData.crf}</strong>
                                        <span className="text-secondary font-normal ml-2">
                                            {formData.crf <= 18 ? 'Near-lossless' : formData.crf <= 22 ? 'High quality' : formData.crf <= 26 ? 'Standard' : 'Space-saving'}
                                        </span>
                                    </label>
                                    <input
                                        type="range"
                                        className="crf-slider"
                                        min={18}
                                        max={28}
                                        value={formData.crf}
                                        onChange={e => setFormData({ ...formData, crf: parseInt(e.target.value) })}
                                    />
                                    <div className="crf-labels">
                                        <span>Better Quality</span>
                                        <span>Smaller Files</span>
                                    </div>
                                </div>
                            </div>
                        )}

                        {/* Step 6: Review */}
                        {step === 6 && (
                            <div className="fade-in">
                                <table className="review-table">
                                    <tbody>
                                        <tr>
                                            <td>Admin Password</td>
                                            <td>{'•'.repeat(Math.min(formData.adminPassword.length, 12))}</td>
                                        </tr>
                                        <tr>
                                            <td>License Key</td>
                                            <td>{formData.licenseKey || <span className="text-secondary">None</span>}</td>
                                        </tr>
                                        <tr>
                                            <td>AI Provider</td>
                                            <td>{formData.aiProvider.toUpperCase()}</td>
                                        </tr>
                                        {formData.aiProvider !== 'none' && formData.aiEndpoint && (
                                            <tr>
                                                <td>AI Endpoint</td>
                                                <td>{formData.aiEndpoint}</td>
                                            </tr>
                                        )}
                                        {formData.aiModel && (
                                            <tr>
                                                <td>AI Model</td>
                                                <td>{formData.aiModel}</td>
                                            </tr>
                                        )}
                                        <tr>
                                            <td>Subtitle Mode</td>
                                            <td>
                                                {formData.subtitleMode === 'always' ? 'Always Download'
                                                    : formData.subtitleMode === 'selective' ? 'Per Job'
                                                    : 'Disabled'}
                                            </td>
                                        </tr>
                                        {formData.subtitleMode !== 'never' && (
                                            <>
                                                <tr>
                                                    <td>Subtitle Language</td>
                                                    <td>{formData.subtitleLang || 'en'}</td>
                                                </tr>
                                                <tr>
                                                    <td>Subtitle API Key</td>
                                                    <td>{formData.subtitleApiKey ? '••••••••' : <span className="text-secondary">Not set</span>}</td>
                                                </tr>
                                            </>
                                        )}
                                        <tr>
                                            <td>GPU Vendor</td>
                                            <td>{formData.gpuVendor ? formData.gpuVendor.toUpperCase() : 'CPU'}</td>
                                        </tr>
                                        <tr>
                                            <td>Quality Preset</td>
                                            <td>{formData.qualityPreset.charAt(0).toUpperCase() + formData.qualityPreset.slice(1)}</td>
                                        </tr>
                                        <tr>
                                            <td>CRF</td>
                                            <td>{formData.crf}</td>
                                        </tr>
                                    </tbody>
                                </table>
                                {submitError && (
                                    <div className="setup-error mt-4">{submitError}</div>
                                )}
                            </div>
                        )}

                        {/* Step 7: Launching */}
                        {step === 7 && (
                            <div className="welcome-screen fade-in">
                                <div className="flex justify-center p-8">
                                    <div className="spinner-lg"></div>
                                </div>
                                <p className="text-xl">Setting up your media converter&hellip;</p>
                            </div>
                        )}
                    </div>
                </div>

                <div className="setup-footer bg-black/10">
                    {step > 0 && step < 7 && (
                        <button className="btn btn-secondary" onClick={handleBack}>Back</button>
                    )}
                    <div style={{ flex: 1 }}></div>
                    {step < 6 && (
                        <button
                            className="btn btn-primary px-8"
                            onClick={handleNext}
                            disabled={isNextDisabled}
                        >
                            Next
                        </button>
                    )}
                    {step === 6 && (
                        <button
                            className="btn btn-primary px-10 btn-lg"
                            onClick={handleSubmit}
                            disabled={isSubmitting}
                        >
                            {isSubmitting ? 'Launching\u2026' : 'Launch'}
                        </button>
                    )}
                </div>
            </div>
        </div>
    );
};

export default SetupWizard;
