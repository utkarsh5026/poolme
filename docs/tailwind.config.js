/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    './.vitepress/**/*.{js,ts,vue}',
    './*.md',
    './guide/**/*.md',
    './api/**/*.md',
    './examples/**/*.md',
    './advanced/**/*.md'
  ],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        poolme: {
          primary: '#60a5fa',
          secondary: '#a78bfa',
          accent: '#f472b6',
          dark: '#1e1e1e',
          darker: '#1a1a1a',
          light: '#f8fafc',
          bg: {
            DEFAULT: '#1e1e1e',
            soft: '#252526',
            mute: '#2d2d30',
            alt: '#1a1a1a'
          }
        }
      },
      fontFamily: {
        sans: ['-apple-system', 'BlinkMacSystemFont', '"Segoe UI"', 'Roboto', 'Oxygen', 'Ubuntu', 'Cantarell', 'sans-serif'],
        mono: ['"Cascadia Code"', '"Cascadia Mono"', '"JetBrains Mono"', 'Menlo', 'Monaco', 'Consolas', '"Courier New"', 'monospace']
      },
      animation: {
        'fade-in': 'fadeIn 0.5s ease-in-out',
        'slide-up': 'slideUp 0.5s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite'
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { transform: 'translateY(20px)', opacity: '0' },
          '100%': { transform: 'translateY(0)', opacity: '1' }
        }
      }
    }
  },
  plugins: []
}
