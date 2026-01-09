export default function ScannerConfig() {
    return (
        <div className="scanner-config-view">
            <div className="view-header">
                <h1>Scanner Configuration</h1>
                <p className="text-secondary">Configure automatic file discovery and processing</p>
            </div>

            <div className="card mt-4">
                <div className="card-header">
                    <h3 className="card-title">Scanner Status</h3>
                </div>
                <div className="card-body">
                    <p className="text-secondary">
                        Scanner configuration will be available here. You can manage watch directories,
                        file patterns, and scanning modes.
                    </p>
                </div>
            </div>
        </div>
    );
}
