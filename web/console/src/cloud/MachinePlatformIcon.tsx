/** A machine silhouette for places where the person must choose by name. */
export function MachinePlatformIcon({ platform }: { platform: string }) {
  const server = /linux/i.test(platform)
  return <span className="cloud-machine-platform-icon" aria-hidden="true">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
      strokeLinecap="round" strokeLinejoin="round">
      {server ? <>
        <rect x="4" y="3" width="16" height="8" rx="1.5" />
        <rect x="4" y="13" width="16" height="8" rx="1.5" />
        <circle cx="7.5" cy="7" r=".7" fill="currentColor" stroke="none" />
        <circle cx="7.5" cy="17" r=".7" fill="currentColor" stroke="none" />
        <path d="M11 7h6M11 17h6" />
      </> : <>
        <rect x="3" y="3" width="18" height="14" rx="1.5" />
        <path d="M9 21h6M12 17v4" />
      </>}
    </svg>
  </span>
}
