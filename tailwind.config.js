module.exports = {
  darkMode: 'class',
  content: [
    './web/templates/**/*.html',
    './web/static/js/**/*.js'
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Sora', 'system-ui', 'sans-serif'],
      },
      colors: {
        jg: {
          bg: 'var(--jg-bg-primary)',
          card: 'var(--jg-bg-card)',
          accent: '#14b8a6',
          'accent-light': '#2dd4bf',
          'accent-cyan': '#22d3ee',
          'accent-emerald': '#10b981',
          gold: '#f59e0b',
          ink: '#e5eefb',
          text: 'var(--jg-text)',
          'text-muted': 'var(--jg-text-muted)',
        },
      },
    },
  },
  safelist: [
    'hidden',
    'block',
    'flex',
    'inline-flex',
    'text-red-400',
    'text-emerald-400',
    'text-emerald-300',
    'text-cyan-300',
    'text-cyan-400',
    'text-purple-300',
    'text-purple-400',
    'text-amber-300',
    'text-slate-300',
    'text-slate-400',
    'text-slate-500',
    'bg-red-500/10',
    'bg-emerald-500/10',
    'bg-emerald-500/15',
    'bg-cyan-500/15',
    'bg-purple-500/15',
    'bg-amber-500/15',
    'shadow-purple-500/20',
    'text-jg-accent-cyan',
    'text-jg-accent-emerald',
    'bg-jg-accent-cyan',
    'bg-jg-accent-emerald'
  ],
};
