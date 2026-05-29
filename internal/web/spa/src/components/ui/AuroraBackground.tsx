// AuroraBackground — the ambient gradient mesh that sits behind every
// screen. It's a fixed, pointer-transparent layer (z-index -1) defined
// in styles.css (.aurora-root / .aurora-mesh). The drift is pure CSS
// (compositor-only transform) so there's no JS render loop and it's
// safe under React StrictMode; reduced-motion stops the drift via the
// media query in styles.css. Mount once, inside AppShell, so the
// [data-theme] toggle applies to its tokens.
//
// It also hosts the single shared #lg-refraction SVG filter that the
// optional `.lg-refract` class references for true light-bending edges on
// a few hero surfaces (command bar, primary CTAs). The filter must live
// in the DOM for url(#…) to resolve; Chromium uses it as a backdrop
// displacement, Safari/Firefox ignore it and keep the layered glass blur.
export function AuroraBackground() {
  return (
    <div className="aurora-root" aria-hidden="true">
      <div className="aurora-mesh" />
      <svg
        width="0"
        height="0"
        aria-hidden="true"
        style={{ position: 'absolute', width: 0, height: 0, pointerEvents: 'none' }}
      >
        <defs>
          {/* Gentle fractal-noise displacement → an organic "liquid glass"
              warp of the blurred backdrop. Size-independent (no per-element
              maps), kept low-frequency so it reads as soft refraction, not
              frosted texture. */}
          <filter
            id="lg-refraction"
            x="-20%"
            y="-20%"
            width="140%"
            height="140%"
            colorInterpolationFilters="sRGB"
          >
            <feTurbulence
              type="fractalNoise"
              baseFrequency="0.011 0.015"
              numOctaves="2"
              seed="7"
              result="noise"
            />
            <feGaussianBlur in="noise" stdDeviation="1.4" result="softNoise" />
            <feDisplacementMap
              in="SourceGraphic"
              in2="softNoise"
              scale="20"
              xChannelSelector="R"
              yChannelSelector="G"
            />
          </filter>
        </defs>
      </svg>
    </div>
  );
}
