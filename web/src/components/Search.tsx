import { useState } from 'react';
import type { ProcessedFile } from '../types';

export default function Search() {
    const [query, setQuery] = useState('');
    const [results, setResults] = useState<ProcessedFile[]>([]);
    const [isSearching, setIsSearching] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const handleSearch = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!query.trim()) return;

        setIsSearching(true);
        setError(null);

        try {
            const response = await fetch(`/api/search?q=${encodeURIComponent(query)}`);
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Search failed');
            }
            const data = await response.json();
            setResults(data || []);
        } catch (err: any) {
            setError(err.message);
            setResults([]);
        } finally {
            setIsSearching(false);
        }
    };

    return (
        <div className="search-view">
            <div className="view-header">
                <h1>AI Media Search</h1>
                <p className="text-secondary">Find any media in your library using natural language</p>
            </div>

            <div className="card mb-6">
                <form onSubmit={handleSearch} className="flex gap-2 p-2">
                    <input
                        type="text"
                        className="input"
                        placeholder="e.g., 'Action movies set in space' or 'That documentary about bees'"
                        value={query}
                        onChange={e => setQuery(e.target.value)}
                        disabled={isSearching}
                        style={{ fontSize: '1.125rem', padding: '0.75rem 1rem' }}
                    />
                    <button
                        type="submit"
                        className="btn btn-primary btn-lg"
                        disabled={isSearching || !query.trim()}
                        style={{ minWidth: '120px' }}
                    >
                        {isSearching ? 'Searching...' : 'Search'}
                    </button>
                </form>
            </div>

            {error && (
                <div className="alert alert-danger mb-6">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ width: '20px', height: '20px' }}>
                        <path d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" strokeWidth="2" strokeLinecap="round" />
                    </svg>
                    {error}
                </div>
            )}

            <div className="search-results">
                {isSearching ? (
                    <div className="flex flex-col items-center py-12">
                        <div className="spinner-lg mb-4"></div>
                        <p className="text-secondary">AI is analyzing your library...</p>
                    </div>
                ) : results.length > 0 ? (
                    <div className="grid grid-1 gap-4">
                        {results.map((file, index) => (
                            <div key={index} className="card result-card hover-bg-tertiary transition-all">
                                <div className="card-body flex justify-between items-center">
                                    <div className="flex flex-col gap-1">
                                        <div className="font-medium text-lg">
                                            {file.path.split('/').pop()}
                                        </div>
                                        <div className="text-sm text-secondary font-mono">
                                            {file.path}
                                        </div>
                                        <div className="flex gap-2 mt-2">
                                            <span className={`badge badge-${file.jobType}`}>
                                                {file.jobType}
                                            </span>
                                            <span className="text-xs text-secondary italic">
                                                Processed on {new Date(file.processedAt).toLocaleDateString()}
                                            </span>
                                        </div>
                                    </div>
                                    <div className="result-actions">
                                        <button className="btn btn-secondary btn-sm">
                                            Open Folder
                                        </button>
                                    </div>
                                </div>
                            </div>
                        ))}
                    </div>
                ) : query && !isSearching ? (
                    <div className="text-center py-12">
                        <p className="text-secondary text-lg">No matching media found for "{query}"</p>
                    </div>
                ) : (
                    <div className="text-center py-12 opacity-50">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ width: '64px', height: '64px', margin: '0 auto 1rem' }}>
                            <path d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" strokeWidth="2" strokeLinecap="round" />
                        </svg>
                        <p className="text-xl">Try searching for themes, genres, or specific topics</p>
                    </div>
                )}
            </div>
        </div>
    );
}
