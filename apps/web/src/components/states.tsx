export function LoadingState({ label = "Loading workspace" }: { label?: string }) {
  return <div className="state-panel" role="status"><span className="loading-dot" aria-hidden="true" />{label}</div>;
}

export function ErrorState({ message = "We could not load this view.", onRetry }: { message?: string; onRetry?: () => void }) {
  return <div className="state-panel error-state" role="alert"><strong>Something needs attention</strong><span>{message}</span>{onRetry && <button className="text-button" onClick={onRetry}>Try again</button>}</div>;
}

export function EmptyState({ title, message }: { title: string; message: string }) {
  return <div className="state-panel empty-state"><strong>{title}</strong><span>{message}</span></div>;
}

export function PermissionDeniedState() {
  return <div className="state-panel error-state" role="alert"><strong>Permission denied</strong><span>Your role does not allow this operational action.</span></div>;
}