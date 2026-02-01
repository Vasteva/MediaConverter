import React, { useState, useEffect } from 'react';
import './SetupWizard.css';

interface SetupWizardProps {
    onComplete: () => void;
}

interface ProbesState {
    ffmpeg: boolean;
    makemkv: boolean;
    gpu: string | null;
}

const SetupWizard: React.FC<SetupWizardProps> = ({ onComplete }) => {
    const [step, setStep] = useState(0);
    const [probes, setProbes] = useState<ProbesState | null>(null);
    const [formData, setFormData] = useState({
        adminPassword: '',
        aiProvider: 'none',
        aiApiKey: '',
        licenseKey: '',
    });
    const [isSubmitting, setIsSubmitting] = useState(false);

    useEffect(() => {
        if (step === 1) {
            fetchProbes();
        }
    }, [step]);

    const fetchProbes = async () => {
        try {
            const res = await fetch('/api/setup/probes');
            const data = await res.json();
            setProbes(data);
        } catch (e) {
            console.error('Failed to fetch probes', e);
        }
    };

    const handleNext = () => setStep(s => s + 1);
    const handleBack = () => setStep(s => s - 1);

    const handleSubmit = async () => {
        setIsSubmitting(true);
        try {
            const res = await fetch('/api/setup/complete', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(formData),
            });
            if (res.ok) {
                onComplete();
            }
        } catch (e) {
            console.error('Setup failed', e);
        } finally {
            setIsSubmitting(false);
        }
    };

    const steps = [
        { title: 'Welcome', subtitle: 'Let\'s get your Vastiva Media Converter ready.' },
        { title: 'System Check', subtitle: 'Checking for required tools and hardware.' },
        { title: 'Security', subtitle: 'Secure your admin account.' },
        { title: 'AI Integration', subtitle: 'Enhance your media with AI power.' },
        { title: 'Ready!', subtitle: 'Everything is configured.' }
    ];

    return (
        <div className="setup-wizard-overlay shadow-xl">
            <div className="setup-wizard-card glass animate-in fade-in zoom-in duration-300">
                <div className="setup-wizard-content">
                    <div className="setup-step-indicator">
                        {steps.map((_, i) => (
                            <div key={i} className={`step-dot ${i === step ? 'active' : ''} ${i < step ? 'completed' : ''}`} />
                        ))}
                    </div>

                    <div className="setup-header">
                        <h2 className="title-gradient font-bold">{steps[step].title}</h2>
                        <p className="text-secondary">{steps[step].subtitle}</p>
                    </div>

                    <div className="setup-form">
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

                        {step === 1 && (
                            <div className="fade-in space-y-4">
                                {probes ? (
                                    <>
                                        <div className="probe-item bg-black/20 p-4 rounded-xl border border-white/5">
                                            <span>FFmpeg (Transcoding)</span>
                                            <span className={`probe-status ${probes.ffmpeg ? 'status-ok' : 'status-warn'}`}>
                                                {probes.ffmpeg ? 'Detected' : 'Not Found'}
                                            </span>
                                        </div>
                                        <div className="probe-item bg-black/20 p-4 rounded-xl border border-white/5">
                                            <span>MakeMKV (Extraction)</span>
                                            <span className={`probe-status ${probes.makemkv ? 'status-ok' : 'status-warn'}`}>
                                                {probes.makemkv ? 'Detected' : 'Optional'}
                                            </span>
                                        </div>
                                        <div className="probe-item bg-black/20 p-4 rounded-xl border border-white/5">
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

                        {step === 2 && (
                            <div className="fade-in space-y-6">
                                <div className="form-group">
                                    <label className="block mb-2 font-medium">Admin Password</label>
                                    <input
                                        type="password"
                                        className="input w-full"
                                        value={formData.adminPassword}
                                        onChange={e => setFormData({ ...formData, adminPassword: e.target.value })}
                                        placeholder="Enter a strong password"
                                        autoFocus
                                    />
                                    <small className="text-secondary block mt-1">You'll use this to sign in to the dashboard.</small>
                                </div>
                                <div className="form-group">
                                    <label className="block mb-2 font-medium">License Key (Optional)</label>
                                    <input
                                        type="text"
                                        className="input w-full font-mono"
                                        value={formData.licenseKey}
                                        onChange={e => setFormData({ ...formData, licenseKey: e.target.value })}
                                        placeholder="VASTIVA-PRO-..."
                                    />
                                    <small className="text-secondary block mt-1">Enter your Pro key to unlock AI Search and Advanced Automation.</small>
                                </div>
                            </div>
                        )}

                        {step === 3 && (
                            <div className="fade-in">
                                <div className="ai-provider-grid">
                                    {['gemini', 'openai', 'ollama', 'none'].map(p => (
                                        <div
                                            key={p}
                                            className={`ai-card ${formData.aiProvider === p ? 'active' : ''}`}
                                            onClick={() => setFormData({ ...formData, aiProvider: p })}
                                        >
                                            <span className="icon text-2xl block mb-2">{p === 'none' ? '🚫' : '✨'}</span>
                                            <span className="name font-bold">{p.toUpperCase()}</span>
                                        </div>
                                    ))}
                                </div>
                                {formData.aiProvider !== 'none' && formData.aiProvider !== 'ollama' && (
                                    <div className="form-group mt-6">
                                        <label className="block mb-2 font-medium">API Key</label>
                                        <input
                                            type="password"
                                            className="input w-full"
                                            value={formData.aiApiKey}
                                            onChange={e => setFormData({ ...formData, aiApiKey: e.target.value })}
                                            placeholder={`Enter your ${formData.aiProvider} API key`}
                                        />
                                    </div>
                                )}
                            </div>
                        )}

                        {step === 4 && (
                            <div className="welcome-screen fade-in text-center">
                                <div className="text-6xl mb-6">🚀</div>
                                <p className="text-xl">Setup complete! You can now start automating your media library.</p>
                            </div>
                        )}
                    </div>
                </div>

                <div className="setup-footer bg-black/10">
                    {step > 0 && step < 4 && (
                        <button className="btn btn-secondary" onClick={handleBack}>Back</button>
                    )}
                    <div style={{ flex: 1 }}></div>
                    {step < 4 ? (
                        <button
                            className="btn btn-primary px-8"
                            onClick={handleNext}
                            disabled={step === 2 && !formData.adminPassword}
                        >
                            Next
                        </button>
                    ) : (
                        <button className="btn btn-primary px-10 btn-lg" onClick={handleSubmit} disabled={isSubmitting}>
                            {isSubmitting ? 'Finalizing...' : 'Finish & Launch'}
                        </button>
                    )}
                </div>
            </div>
        </div>
    );
};

export default SetupWizard;
