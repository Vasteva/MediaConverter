import './Header.css';

interface HeaderProps {
    theme: 'light' | 'dark';
    onToggleTheme: () => void;
    currentView: string;
    onViewChange: (view: 'dashboard' | 'jobs' | 'scanner' | 'settings') => void;
    isPremium?: boolean;
}

export default function Header({ theme, onToggleTheme, currentView, onViewChange, isPremium }: HeaderProps) {
    return (
        <header className="header">
            <div className="header-container">
                <div className="header-left">
                    <div className="logo-container">
                        <svg className="logo-icon" viewBox="0 0 100 100" fill="none">
                            <path
                                d="M50 10 L90 30 L90 70 L50 90 L10 70 L10 30 Z"
                                stroke="currentColor"
                                strokeWidth="3"
                                fill="none"
                            />
                            <path
                                d="M35 35 L50 65 L65 35"
                                stroke="var(--brand-teal)"
                                strokeWidth="4"
                                strokeLinecap="round"
                                strokeLinejoin="round"
                            />
                        </svg>
                        <div className="logo-text">
                            <div className="logo-title flex items-center">
                                VASTIVA
                                {isPremium && <span className="pro-tag ml-2">PRO</span>}
                            </div>
                            <div className="logo-subtitle">MEDIA CONVERTER</div>
                        </div>
                    </div>
                </div>

                <nav className="header-nav">
                    <button
                        className={`nav-item ${currentView === 'dashboard' ? 'active' : ''}`}
                        onClick={() => onViewChange('dashboard')}
                    >
                        <svg className="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                            <rect x="3" y="3" width="7" height="7" strokeWidth="2" />
                            <rect x="14" y="3" width="7" height="7" strokeWidth="2" />
                            <rect x="3" y="14" width="7" height="7" strokeWidth="2" />
                            <rect x="14" y="14" width="7" height="7" strokeWidth="2" />
                        </svg>
                        Dashboard
                    </button>

                    <button
                        className={`nav-item ${currentView === 'jobs' ? 'active' : ''}`}
                        onClick={() => onViewChange('jobs')}
                    >
                        <svg className="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                            <path d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" strokeWidth="2" strokeLinecap="round" />
                        </svg>
                        Jobs
                    </button>

                    <button
                        className={`nav-item ${currentView === 'scanner' ? 'active' : ''}`}
                        onClick={() => onViewChange('scanner')}
                    >
                        <svg className="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                            <path d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" strokeWidth="2" strokeLinecap="round" />
                        </svg>
                        Scanner
                    </button>

                    <button
                        className={`nav-item ${currentView === 'settings' ? 'active' : ''}`}
                        onClick={() => onViewChange('settings')}
                    >
                        <svg className="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                            <path d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" strokeWidth="2" />
                            <circle cx="12" cy="12" r="3" strokeWidth="2" />
                        </svg>
                        Settings
                    </button>
                </nav>

                <div className="header-right">
                    <button className="theme-toggle" onClick={onToggleTheme} title="Toggle theme">
                        {theme === 'light' ? (
                            <svg className="theme-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                                <path d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                            </svg>
                        ) : (
                            <svg className="theme-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                                <circle cx="12" cy="12" r="5" strokeWidth="2" />
                                <path d="M12 1v2m0 18v2M4.22 4.22l1.42 1.42m12.72 12.72l1.42 1.42M1 12h2m18 0h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42" strokeWidth="2" strokeLinecap="round" />
                            </svg>
                        )}
                    </button>
                </div>
            </div>
        </header>
    );
}
