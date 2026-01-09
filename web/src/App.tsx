import { useState, useEffect } from 'react';
import './App.css';
import Header from './components/Header';
import Dashboard from './components/Dashboard';
import JobList from './components/JobList';
import ScannerConfig from './components/ScannerConfig';
import Settings from './components/Settings';
import type { Job, SystemConfig } from './types';

function App() {
  const [theme, setTheme] = useState<'light' | 'dark'>('dark');
  const [currentView, setCurrentView] = useState<'dashboard' | 'jobs' | 'scanner' | 'settings'>('dashboard');
  const [jobs, setJobs] = useState<Job[]>([]);
  const [config, setConfig] = useState<SystemConfig | null>(null);
  const [isLoading, setIsLoading] = useState(true);

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

  // Fetch jobs from API
  const fetchJobs = async () => {
    try {
      const response = await fetch('/api/jobs');
      if (response.ok) {
        const data = await response.json();
        setJobs(data || []);
      }
    } catch (error) {
      console.error('Failed to fetch jobs:', error);
    }
  };

  // Fetch config from API
  const fetchConfig = async () => {
    try {
      const response = await fetch('/api/config');
      if (response.ok) {
        const data = await response.json();
        setConfig(data);
      }
    } catch (error) {
      console.error('Failed to fetch config:', error);
    } finally {
      setIsLoading(false);
    }
  };

  // Initial data fetch
  useEffect(() => {
    fetchJobs();
    fetchConfig();
  }, []);

  // Poll for job updates every 2 seconds
  useEffect(() => {
    const interval = setInterval(fetchJobs, 2000);
    return () => clearInterval(interval);
  }, []);

  // Create new job
  const createJob = async (jobData: Partial<Job>) => {
    try {
      const response = await fetch('/api/jobs', {
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
      const response = await fetch(`/api/jobs/${jobId}`, {
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

  if (isLoading) {
    return (
      <div className="loading-screen">
        <div className="vastiva-logo-container">
          <img
            src={theme === 'dark' ? '/logo-dark.svg' : '/logo-light.svg'}
            alt="Vastiva"
            className="vastiva-logo"
          />
          <div className="spinner-lg"></div>
        </div>
      </div>
    );
  }

  return (
    <div className="app">
      <Header
        theme={theme}
        onToggleTheme={toggleTheme}
        currentView={currentView}
        onViewChange={setCurrentView}
      />

      <main className="main-content">
        {currentView === 'dashboard' && (
          <Dashboard jobs={jobs} config={config} />
        )}
        {currentView === 'jobs' && (
          <JobList
            jobs={jobs}
            onCreateJob={createJob}
            onCancelJob={cancelJob}
          />
        )}
        {currentView === 'scanner' && (
          <ScannerConfig />
        )}
        {currentView === 'settings' && (
          <Settings config={config} onConfigUpdate={fetchConfig} />
        )}
      </main>
    </div>
  );
}

export default App;
