import { useState, useEffect } from 'react';
import '../index.css'; // Ensure we have access to variables

interface FileEntry {
    name: string;
    path: string;
    isDir: boolean;
    size: number;
    modTime: string;
    extension: string;
}

interface FileListResponse {
    path: string;
    parent: string;
    entries: FileEntry[];
    isRoot: boolean;
    error?: string;
}

interface FileBrowserModalProps {
    isOpen: boolean;
    onClose: () => void;
    onSelect: (path: string) => void;
    title?: string;
    initialPath?: string;
    selectMode?: 'file' | 'directory' | 'both';
    zIndex?: number;
}

export default function FileBrowserModal({
    isOpen,
    onClose,
    onSelect,
    title = 'Select File or Directory',
    initialPath = '/storage',
    selectMode = 'both',
    zIndex = 1000
}: FileBrowserModalProps) {
    const [currentPath, setCurrentPath] = useState(initialPath);
    const [files, setFiles] = useState<FileEntry[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [selectedItem, setSelectedItem] = useState<FileEntry | null>(null);

    // Fetch files when path changes
    useEffect(() => {
        if (!isOpen) return;

        const fetchFiles = async () => {
            setLoading(true);
            setError(null);
            setSelectedItem(null);

            try {
                const token = localStorage.getItem('token');
                const encodedPath = encodeURIComponent(currentPath);
                const response = await fetch(`/api/fs/list?path=${encodedPath}`, {
                    headers: {
                        'Authorization': `Bearer ${token}`
                    }
                });

                if (!response.ok) {
                    throw new Error(`Failed to load directory: ${response.statusText}`);
                }

                const data: FileListResponse = await response.json();
                if (data.error) {
                    throw new Error(data.error);
                }

                setFiles(data.entries || []);
                // Update currentPath from server response to get clean path
                if (data.path) {
                    // setCurrentPath(data.path); // Doing this might cause loop if not careful, better to trust requested path or just use it for display
                }
            } catch (err: any) {
                setError(err.message);
            } finally {
                setLoading(false);
            }
        };

        fetchFiles();
    }, [currentPath, isOpen]);

    // Reset when opening
    useEffect(() => {
        if (isOpen) {
            setCurrentPath(initialPath || '/storage');
        }
    }, [isOpen]);

    const handleNavigate = (path: string) => {
        setCurrentPath(path);
    };

    const handleUp = () => {
        // Simple parent calculation
        if (currentPath === '/') return;
        const parent = currentPath.split('/').slice(0, -1).join('/') || '/';
        setCurrentPath(parent);
    };

    const handleSelectCurrent = () => {
        onSelect(currentPath);
        onClose();
    };

    const handleConfirmSelection = () => {
        if (selectedItem) {
            onSelect(selectedItem.path);
            onClose();
        } else if (selectMode === 'directory' || selectMode === 'both') {
            // Allow selecting current dir if nothing selected?
            handleSelectCurrent();
        }
    };

    if (!isOpen) return null;

    return (
        <div className="modal-backdrop" onClick={onClose} style={{ zIndex }}>
            <div className="modal-content file-browser-modal" onClick={e => e.stopPropagation()} style={{ maxWidth: '800px', width: '90%', maxHeight: '80vh', display: 'flex', flexDirection: 'column' }}>
                <div className="modal-header">
                    <h3>{title}</h3>
                    <button className="btn-icon" onClick={onClose}>✕</button>
                </div>

                <div className="file-browser-controls" style={{ padding: '1rem', borderBottom: '1px solid var(--border-color)', display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
                    <button className="btn btn-sm btn-secondary" onClick={handleUp} disabled={currentPath === '/'}>
                        <span style={{ fontSize: '1.2em', lineHeight: '0.5' }}>↑</span> Up
                    </button>
                    <input
                        type="text"
                        className="input"
                        value={currentPath}
                        readOnly
                        style={{ fontFamily: 'monospace' }}
                    />
                </div>

                <div className="file-list" style={{ flex: 1, overflowY: 'auto', padding: '0.5rem' }}>
                    {loading ? (
                        <div className="flex justify-center p-8">
                            <div className="spinner"></div>
                        </div>
                    ) : error ? (
                        <div className="p-4 text-danger bg-danger-low rounded">
                            {error}
                        </div>
                    ) : (
                        <table className="table" style={{ fontSize: '0.9rem' }}>
                            <thead>
                                <tr>
                                    <th style={{ width: '40px' }}></th>
                                    <th>Name</th>
                                    <th style={{ width: '100px' }}>Size</th>
                                    <th style={{ width: '150px' }}>Date</th>
                                </tr>
                            </thead>
                            <tbody>
                                {(files || []).map((file, idx) => {
                                    const isSelected = selectedItem?.path === file.path;
                                    return (
                                        <tr
                                            key={idx}
                                            onClick={() => setSelectedItem(file)}
                                            onDoubleClick={() => file.isDir && handleNavigate(file.path)}
                                            style={{
                                                cursor: 'pointer',
                                                background: isSelected ? 'rgba(61, 217, 208, 0.1)' : undefined,
                                                borderLeft: isSelected ? '3px solid var(--brand-teal)' : '3px solid transparent'
                                            }}
                                        >
                                            <td style={{ textAlign: 'center' }}>
                                                {file.isDir ? '📁' : '📄'}
                                            </td>
                                            <td style={{ fontWeight: file.isDir ? 600 : 400 }}>
                                                {file.name}
                                            </td>
                                            <td className="text-secondary text-xs">
                                                {file.isDir ? '-' : (file.size / 1024 / 1024).toFixed(2) + ' MB'}
                                            </td>
                                            <td className="text-secondary text-xs">
                                                {file.modTime ? new Date(file.modTime).toLocaleDateString() : '-'}
                                            </td>
                                        </tr>
                                    );
                                })}
                                {(!files || files.length === 0) && (
                                    <tr>
                                        <td colSpan={4} className="text-center text-secondary p-4">Empty directory</td>
                                    </tr>
                                )}
                            </tbody>
                        </table>
                    )}
                </div>

                <div className="modal-footer" style={{ padding: '1rem', borderTop: '1px solid var(--border-color)', display: 'flex', justifyContent: 'flex-end', gap: '0.5rem' }}>
                    <button className="btn btn-secondary" onClick={onClose}>Cancel</button>
                    {(selectMode === 'directory' || selectMode === 'both') && (
                        <button className="btn btn-secondary" onClick={handleSelectCurrent}>
                            Select Current Folder
                        </button>
                    )}
                    <button
                        className="btn btn-primary"
                        onClick={handleConfirmSelection}
                        disabled={!selectedItem && selectMode === 'file'}
                    >
                        {selectedItem ? `Select "${selectedItem.name}"` : 'Select'}
                    </button>
                </div>
            </div>
        </div>
    );
}
