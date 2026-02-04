import { useState, useEffect } from 'react';
import { createPortal } from 'react-dom';
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
    selectMode = 'both'
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

    // Body scroll lock
    useEffect(() => {
        if (isOpen) {
            document.body.style.overflow = 'hidden';
            document.body.style.paddingRight = '0px'; // Prevent shift if possible
        } else {
            document.body.style.overflow = '';
            document.body.style.paddingRight = '';
        }
        return () => {
            document.body.style.overflow = '';
            document.body.style.paddingRight = '';
        };
    }, [isOpen]);

    if (!isOpen) return null;

    // Use a dedicated styling object to avoid CSS file collisions
    const styles = {
        backdrop: {
            position: 'fixed' as const,
            top: 0,
            left: 0,
            width: '100vw',
            height: '100vh',
            backgroundColor: 'rgba(0, 0, 0, 0.75)',
            backdropFilter: 'blur(8px)',
            zIndex: 100000,
            display: 'block'
        },
        modal: {
            position: 'fixed' as const,
            top: '50%',
            left: '50%',
            transform: 'translate(-50%, -50%)',
            maxWidth: '1000px',
            width: '90vw',
            maxHeight: '90vh',
            backgroundColor: 'var(--bg-secondary)',
            borderRadius: '16px',
            boxShadow: '0 25px 50px -12px rgba(0, 0, 0, 0.5)',
            border: '1px solid var(--border-color)',
            display: 'flex',
            flexDirection: 'column' as const,
            overflow: 'hidden',
            zIndex: 100001
        }
    };

    return createPortal(
        <div className="file-browser-portal-wrapper">
            <div
                className="modal-backdrop-custom"
                onClick={onClose}
                style={styles.backdrop}
            />
            <div
                className="modal-content-custom"
                onClick={e => e.stopPropagation()}
                style={styles.modal}
            >
                <div style={{ padding: '1.5rem', borderBottom: '1px solid var(--border-color)', display: 'flex', justifyContent: 'space-between', alignItems: 'center', background: 'var(--bg-tertiary)' }}>
                    <h3 style={{ margin: 0, fontSize: '1.5rem', fontWeight: 700 }}>{title}</h3>
                    <button
                        onClick={onClose}
                        style={{ background: 'rgba(255,255,255,0.1)', border: 'none', cursor: 'pointer', width: '32px', height: '32px', borderRadius: '50%', color: 'var(--text-primary)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                    >
                        ✕
                    </button>
                </div>

                <div style={{ padding: '1rem', borderBottom: '1px solid var(--border-color)', display: 'flex', gap: '0.75rem', alignItems: 'center', background: 'var(--bg-secondary)' }}>
                    <button className="btn btn-secondary" onClick={handleUp} disabled={currentPath === '/'} style={{ padding: '0.5rem 1rem' }}>
                        <span>↑</span> Up
                    </button>
                    <div style={{ flex: 1, position: 'relative' }}>
                        <input
                            type="text"
                            className="input"
                            value={currentPath}
                            readOnly
                            style={{ fontFamily: 'monospace', width: '100%', padding: '0.6rem 1rem', background: 'var(--bg-tertiary)', border: '1px solid var(--border-color)', borderRadius: '6px' }}
                        />
                    </div>
                </div>

                <div style={{ flex: 1, overflowY: 'auto', padding: '0px', background: 'var(--bg-secondary)' }}>
                    {loading ? (
                        <div style={{ display: 'flex', justifyContent: 'center', padding: '4rem' }}>
                            <div className="spinner-lg" />
                        </div>
                    ) : error ? (
                        <div style={{ margin: '1rem', padding: '1rem', background: 'rgba(239, 68, 68, 0.1)', color: 'var(--status-failed)', borderRadius: '8px', border: '1px solid var(--status-failed)' }}>
                            {error}
                        </div>
                    ) : (
                        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                            <thead style={{ position: 'sticky', top: 0, background: 'var(--bg-tertiary)', zIndex: 10 }}>
                                <tr>
                                    <th style={{ width: '60px', padding: '1rem', borderBottom: '2px solid var(--border-color)' }}></th>
                                    <th style={{ textAlign: 'left', padding: '1rem', borderBottom: '2px solid var(--border-color)', fontWeight: 600 }}>Name</th>
                                    <th style={{ width: '120px', textAlign: 'left', padding: '1rem', borderBottom: '2px solid var(--border-color)', fontWeight: 600 }}>Size</th>
                                    <th style={{ width: '150px', textAlign: 'left', padding: '1rem', borderBottom: '2px solid var(--border-color)', fontWeight: 600 }}>Modified</th>
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
                                                background: isSelected ? 'rgba(61, 217, 208, 0.15)' : 'transparent',
                                                transition: 'background 0.1s ease',
                                                borderLeft: isSelected ? '4px solid var(--brand-teal)' : '4px solid transparent'
                                            }}
                                            className="file-row-hover"
                                        >
                                            <td style={{ textAlign: 'center', padding: '1rem', borderBottom: '1px solid var(--border-color)', fontSize: '1.2rem' }}>
                                                {file.isDir ? '📁' : '📄'}
                                            </td>
                                            <td style={{ padding: '1rem', borderBottom: '1px solid var(--border-color)', fontWeight: file.isDir ? 600 : 400 }}>
                                                {file.name}
                                            </td>
                                            <td style={{ padding: '1rem', borderBottom: '1px solid var(--border-color)', color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
                                                {file.isDir ? '-' : (file.size / 1024 / 1024).toFixed(2) + ' MB'}
                                            </td>
                                            <td style={{ padding: '1rem', borderBottom: '1px solid var(--border-color)', color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
                                                {file.modTime ? new Date(file.modTime).toLocaleDateString() : '-'}
                                            </td>
                                        </tr>
                                    );
                                })}
                                {(!files || files.length === 0) && (
                                    <tr>
                                        <td colSpan={4} style={{ textAlign: 'center', padding: '4rem', color: 'var(--text-secondary)' }}>
                                            No files or folders found.
                                        </td>
                                    </tr>
                                )}
                            </tbody>
                        </table>
                    )}
                </div>

                <div style={{ padding: '1.25rem', borderTop: '1px solid var(--border-color)', display: 'flex', justifyContent: 'flex-end', gap: '1rem', background: 'var(--bg-tertiary)' }}>
                    <button className="btn btn-secondary" onClick={onClose} style={{ minWidth: '100px' }}>Cancel</button>
                    {(selectMode === 'directory' || selectMode === 'both') && (
                        <button className="btn btn-secondary" onClick={handleSelectCurrent} style={{ minWidth: '160px' }}>
                            Select Current Folder
                        </button>
                    )}
                    <button
                        className="btn btn-primary"
                        onClick={handleConfirmSelection}
                        disabled={!selectedItem && selectMode === 'file'}
                        style={{ minWidth: '120px' }}
                    >
                        {selectedItem ? `Select Selected` : 'Select'}
                    </button>
                </div>
            </div>
        </div>,
        document.body
    );
}
