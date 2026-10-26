/// <reference types="vite/client" />

// Feature flags, read from the environment at build time.
export const flags = {
  // rerunQuestions shows a button that drops the rounds and asks round 1 again.
  rerunQuestions: import.meta.env.VITE_RERUN_QUESTIONS === '1'
}
