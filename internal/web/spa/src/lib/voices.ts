// Mirror of internal/tts/voices.go Voices (same order). A Go drift test
// asserts these two lists are identical.
export const VOICES: readonly string[] = [
  'marin', 'cedar', 'alloy', 'ash', 'ballad', 'coral', 'echo',
  'fable', 'nova', 'onyx', 'sage', 'shimmer', 'verse',
] as const;
