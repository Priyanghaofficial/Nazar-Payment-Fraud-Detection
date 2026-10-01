import { useEffect, useState } from "react";

/** Light/dark switch.
 *
 * Deliberately NOT wired to prefers-color-scheme. A presentation laptop that happens to be
 * set to dark must not decide how a room sees this — the theme changes only when someone
 * asks for it, and the choice survives a reload so it does not reset mid-demo.
 *
 * All of the work is in tokens.css: every component reads semantic tokens, so flipping this
 * attribute repaints the whole console without a single component knowing a theme exists. */

const KEY = "nazar.theme";
type Theme = "light" | "dark";

function apply(t: Theme) {
  document.documentElement.setAttribute("data-theme", t);
}

/** Read once at module load so the first paint is already correct — setting it inside an
 *  effect would show a light flash before flipping. */
function initial(): Theme {
  const saved = localStorage.getItem(KEY);
  return saved === "dark" ? "dark" : "light";
}

apply(initial());

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(initial);

  useEffect(() => {
    apply(theme);
    localStorage.setItem(KEY, theme);
  }, [theme]);

  const next = theme === "dark" ? "light" : "dark";
  return (
    <button
      className="th-toggle"
      onClick={() => setTheme(next)}
      title={`Switch to ${next} theme`}
      aria-label={`Switch to ${next} theme`}
    >
      {theme === "dark" ? (
        // Sun — offering the light theme.
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
          <circle cx="12" cy="12" r="4" />
          <path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M19.1 4.9l-1.4 1.4M6.3 17.7l-1.4 1.4" />
        </svg>
      ) : (
        // Moon — offering the dark theme.
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
          <path d="M21 12.8A9 9 0 1111.2 3a7 7 0 009.8 9.8z" />
        </svg>
      )}
    </button>
  );
}
