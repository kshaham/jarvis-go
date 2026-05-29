// AuroraBackground — the ambient gradient mesh that sits behind every
// screen. It's a fixed, pointer-transparent layer (z-index -1) defined
// in styles.css (.aurora-root / .aurora-mesh). The drift is pure CSS
// (compositor-only transform) so there's no JS render loop and it's
// safe under React StrictMode; reduced-motion stops the drift via the
// media query in styles.css. Mount once, inside AppShell, so the
// [data-theme] toggle applies to its tokens.
export function AuroraBackground() {
  return (
    <div className="aurora-root" aria-hidden="true">
      <div className="aurora-mesh" />
    </div>
  );
}
