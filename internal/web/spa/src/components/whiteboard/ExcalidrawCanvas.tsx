import '@excalidraw/excalidraw/index.css';
import { Excalidraw, serializeAsJSON } from '@excalidraw/excalidraw';
import { useMemo, useRef } from 'react';

// ExcalidrawCanvas — the ONLY module that imports @excalidraw/excalidraw
// + its CSS, so Rollup splits it into its own chunk (loaded via
// React.lazy on the /whiteboard route). MIT-licensed (no watermark /
// license key, unlike tldraw). The team whiteboard persists as an
// Excalidraw scene JSON (serializeAsJSON) stored in the existing
// whiteboard.text field; changes are debounced before bubbling up.
//
// Note: Excalidraw loads its fonts from a CDN at runtime by default —
// fine online; vendor the assets via window.EXCALIDRAW_ASSET_PATH if a
// fully-offline build is required.
export default function ExcalidrawCanvas({
  initialDocument,
  onChange,
}: {
  initialDocument?: string;
  onChange: (json: string) => void;
}) {
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Parse the saved scene once for the initial canvas state. Bad/legacy
  // (plain-text) content falls back to a blank canvas.
  const initialData = useMemo(() => {
    if (!initialDocument) return null;
    try {
      return JSON.parse(initialDocument);
    } catch {
      return null;
    }
  }, [initialDocument]);

  return (
    <div style={{ position: 'absolute', inset: 0 }}>
      <Excalidraw
        theme="dark"
        initialData={initialData}
        onChange={(elements, appState, files) => {
          // Debounce so we POST at most every ~800ms of activity.
          if (timer.current) clearTimeout(timer.current);
          timer.current = setTimeout(() => {
            onChangeRef.current(serializeAsJSON(elements, appState, files, 'local'));
          }, 800);
        }}
      />
    </div>
  );
}
