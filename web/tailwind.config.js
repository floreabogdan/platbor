/** @type {import('tailwindcss').Config} */
// Canonical design tokens — see docs/DESIGN-SYSTEM.md. Do not add colors,
// fonts, shadows, or radii here without updating that document first.
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Manrope', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'monospace'],
      },
      colors: {
        ink: {
          900: '#0b1220', // sidebar / darkest surface
          800: '#101a2e',
          700: '#17233c',
        },
        canvas: '#f5f4f0', // app background (warm paper)
      },
      boxShadow: {
        card: '0 1px 2px rgba(15, 23, 42, 0.04), 0 8px 24px -12px rgba(15, 23, 42, 0.12)',
      },
    },
  },
  plugins: [],
};
