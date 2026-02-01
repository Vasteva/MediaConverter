import { useState, useEffect } from 'react';
import './App.css';
import Header from './components/Header';
import Dashboard from './components/Dashboard';
import JobList from './components/JobList';
import ScannerConfigComponent from './components/ScannerConfig';
import Search from './components/Search';
import Settings from './components/Settings';
import Login from './components/Login';
import SetupWizard from './components/SetupWizard';
import type { Job, SystemConfig, ScannerConfig, SystemStats, DashboardStats } from './types';

function App() {
  const [theme, setTheme] = useState<'light' | 'dark'>('dark');
  const [currentView, setCurrentView] = useState<'dashboard' | 'jobs' | 'scanner' | 'search' | 'settings'>('dashboard');
  const [token, setToken] = useState<string | null>(localStorage.getItem('token'));
  const [jobs, setJobs] = useState<Job[]>([]);
  const [config, setConfig] = useState<SystemConfig | null>(null);
  const [scannerConfig, setScannerConfig] = useState<ScannerConfig | null>(null);
  const [stats, setStats] = useState<SystemStats | null>(null);
  const [dashboardStats, setDashboardStats] = useState<DashboardStats | null>(null);
  const [showSetupWizard, setShowSetupWizard] = useState<boolean>(false);

  // Check if system is initialized
  useEffect(() => {
    fetch('/api/setup/status')
      .then(res => res.json())
      .then(data => {
        if (data.isInitialized === false) {
          setShowSetupWizard(true);
        }
      })
      .catch(err => console.error('Failed to check setup status', err));
  }, []);
  const [isLoading, setIsLoading] = useState(true);

  // Helper for authenticated requests
  const authFetch = async (url: string, options: RequestInit = {}) => {
    const headers = {
      ...options.headers,
      'Authorization': `Bearer ${token}`
    };
    const response = await fetch(url, { ...options, headers });

    // Auto-logout on 401
    if (response.status === 401) {
      handleLogout();
      throw new Error('Session expired');
    }

    return response;
  };

  const handleLogin = (newToken: string) => {
    setToken(newToken);
    localStorage.setItem('token', newToken);
    setIsLoading(true);
  };

  const handleLogout = () => {
    setToken(null);
    localStorage.removeItem('token');
    // Clear data
    setJobs([]);
    setConfig(null);
    setScannerConfig(null);
    setStats(null);
    setDashboardStats(null);
  };

  // Load theme from localStorage
  useEffect(() => {
    const savedTheme = localStorage.getItem('theme') as 'light' | 'dark' | null;
    if (savedTheme) {
      setTheme(savedTheme);
      document.documentElement.setAttribute('data-theme', savedTheme);
    }
  }, []);

  // Toggle theme
  const toggleTheme = () => {
    const newTheme = theme === 'light' ? 'dark' : 'light';
    setTheme(newTheme);
    localStorage.setItem('theme', newTheme);
    document.documentElement.setAttribute('data-theme', newTheme);
  };

  // Fetch stats from API
  const fetchStats = async () => {
    if (!token) return;
    try {
      const response = await authFetch('/api/stats');
      if (response.ok) {
        const data = await response.json();
        setStats(data);
      }

      // Also fetch dashboard stats if premium
      const dResponse = await authFetch('/api/dashboard/stats');
      if (dResponse.ok) {
        const dData = await dResponse.json();
        setDashboardStats(dData);
      }
    } catch (error) {
      console.error('Failed to fetch stats:', error);
    }
  };

  // Fetch jobs from API
  const fetchJobs = async () => {
    if (!token) return;
    try {
      const response = await authFetch('/api/jobs');
      if (response.ok) {
        const data = await response.json();
        setJobs(data || []);
      }
    } catch (error) {
      console.error('Failed to fetch jobs:', error);
    }
  };

  // Fetch configs from API
  const fetchConfigs = async () => {
    if (!token) {
      setIsLoading(false);
      return;
    }
    try {
      const [configRes, scannerRes] = await Promise.all([
        authFetch('/api/config'),
        authFetch('/api/scanner/config')
      ]);

      if (configRes.ok) {
        const data = await configRes.json();
        setConfig(data);
      }
      if (scannerRes.ok) {
        const data = await scannerRes.json();
        setScannerConfig(data);
      }
    } catch (error) {
      console.error('Failed to fetch configs:', error);
    } finally {
      setIsLoading(false);
    }
  };

  // Initial data fetch
  useEffect(() => {
    if (token) {
      fetchJobs();
      fetchConfigs();
      fetchStats();
    } else {
      setIsLoading(false);
    }
  }, [token]);

  // Poll for updates every 2 seconds
  useEffect(() => {
    const interval = setInterval(() => {
      fetchJobs();
      fetchStats();
    }, 2000);
    return () => clearInterval(interval);
  }, []);

  // Create new job
  const createJob = async (jobData: Partial<Job>) => {
    try {
      const response = await authFetch('/api/jobs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(jobData),
      });
      if (response.ok) {
        await fetchJobs();
        return true;
      }
      return false;
    } catch (error) {
      console.error('Failed to create job:', error);
      return false;
    }
  };

  // Cancel job
  const cancelJob = async (jobId: string) => {
    try {
      const response = await authFetch(`/api/jobs/${jobId}`, {
        method: 'DELETE',
      });
      if (response.ok) {
        await fetchJobs();
        return true;
      }
      return false;
    } catch (error) {
      console.error('Failed to cancel job:', error);
      return false;
    }
  };

  // Update System Config
  const updateSystemConfig = async (newConfig: Partial<SystemConfig>) => {
    try {
      const response = await authFetch('/api/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newConfig),
      });
      if (response.ok) {
        await fetchConfigs();
        return true;
      }
      return false;
    } catch (error) {
      console.error('Failed to update system config:', error);
      return false;
    }
  };

  // Update Scanner Config
  const updateScannerConfig = async (newConfig: ScannerConfig) => {
    try {
      const response = await authFetch('/api/scanner/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newConfig),
      });
      if (response.ok) {
        await fetchConfigs();
        return true;
      }
      return false;
    } catch (error) {
      console.error('Failed to update scanner config:', error);
      return false;
    }
  };

  if (showSetupWizard) {
    return <SetupWizard onComplete={() => setShowSetupWizard(false)} />;
  }

  if (isLoading) {
    return (
      <div className="loading-screen">
        <div className="vastiva-logo-container">
          <img
            src={theme === 'dark' ? '/logo-dark.png' : '/logo-light.png'}
            alt="Vastiva"
            className="vastiva-logo"
          />
          <div className="spinner-lg"></div>
        </div>
      </div>
    );
  }

  if (!token) {
    return <Login onLogin={handleLogin} />;
  }

  return (
    <div className="app">
      <Header
        theme={theme}
        onToggleTheme={toggleTheme}
        currentView={currentView}
        onViewChange={setCurrentView}
        isPremium={config?.isPremium}
        onLogout={handleLogout}
      />

      <main className="main-content">
        {currentView === 'dashboard' && (
          <Dashboard jobs={jobs} config={config} stats={stats} dashboardStats={dashboardStats} />
        )}
        {currentView === 'jobs' && (
          <JobList
            jobs={jobs}
            onCreateJob={createJob}
            onCancelJob={cancelJob}
          />
        )}
        {currentView === 'scanner' && scannerConfig && (
          <ScannerConfigComponent
            config={scannerConfig}
            onSave={updateScannerConfig}
          />
        )}
        {currentView === 'search' && (
          <Search authFetch={authFetch} />
        )}
        {currentView === 'settings' && (
          <Settings
            config={config}
            onConfigUpdate={updateSystemConfig}
          />
        )}
      </main>
    </div>
  );
}

export default App;
