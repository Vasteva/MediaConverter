import { Component, type ErrorInfo, type ReactNode } from 'react';

interface Props {
    children: ReactNode;
    /** Shown in the fallback so the user knows which part failed. */
    label?: string;
}

interface State {
    error: Error | null;
}

/**
 * Catches render errors in a subtree and shows a recoverable message.
 *
 * Without this, a single bad field takes down the entire app: a null
 * `allowedDays` in the schedule threw inside the settings view, React unmounted
 * the whole tree, and the user saw a blank white page with no indication of what
 * had happened or how to get back.
 *
 * A blank page is the worst possible failure mode here — it gives the user
 * nothing to act on and looks identical to a crashed server. Showing the error
 * and keeping the rest of the app reachable is strictly better, even when the
 * underlying bug is unfixed.
 */
export default class ErrorBoundary extends Component<Props, State> {
    state: State = { error: null };

    static getDerivedStateFromError(error: Error): State {
        return { error };
    }

    componentDidCatch(error: Error, info: ErrorInfo) {
        // Kept in the console so the stack survives for diagnosis; the UI shows
        // only the message.
        console.error('Render error caught by boundary:', error, info.componentStack);
    }

    private reset = () => this.setState({ error: null });

    render() {
        const { error } = this.state;
        if (!error) return this.props.children;

        return (
            <div className="error-boundary" role="alert">
                <h2 className="error-boundary-title">
                    {this.props.label ? `${this.props.label} failed to load` : 'Something went wrong'}
                </h2>
                <p className="error-boundary-message">{error.message}</p>
                <p className="error-boundary-hint">
                    The rest of the app is still working. Try again, or switch to another view.
                </p>
                <div className="error-boundary-actions">
                    <button className="btn btn-primary" onClick={this.reset}>Try again</button>
                    <button className="btn" onClick={() => window.location.reload()}>Reload</button>
                </div>
            </div>
        );
    }
}
