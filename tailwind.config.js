/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./internal/views/**/*.templ"],
  theme: {
    extend: {
      colors: {
        bg: "var(--color-bg)",
        surface: "var(--color-surface)",
        border: "var(--color-border)",
        text: "var(--color-text)",
        muted: "var(--color-muted)",
        accent: "var(--color-accent)",
        success: "var(--color-success)",
        info: "var(--color-info)",
      },
    },
  },
  plugins: [],
};
